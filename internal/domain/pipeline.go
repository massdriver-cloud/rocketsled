package domain

import (
	"context"
	"fmt"
	"maps"
	"reflect"
	"regexp"
	"slices"
)

// Pipeline orders an app's environments into stages and names the components
// a promotion copies from one stage to a later one.
type Pipeline struct {
	Stages  []Stage
	Promote []string
}

// Stage is one step of a pipeline with the presets its apps start from.
type Stage struct {
	ID      string
	Name    string
	Presets map[string]string
}

// StageSummary names a stage, in pipeline order.
type StageSummary struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// OfferedStage is a stage as one runtime offers it: the presets its form
// starts with and the add-ons switched on, limited to what the runtime offers.
type OfferedStage struct {
	ID      string            `json:"id"`
	Name    string            `json:"name"`
	Presets map[string]string `json:"presets"`
	Addons  []string          `json:"addons"`
}

func (p Pipeline) index(stage string) int {
	return slices.IndexFunc(p.Stages, func(st Stage) bool { return st.ID == stage })
}

// stageOffer checks the pipeline against one runtime's template and reduces it
// to what that runtime offers. A component the template doesn't have is
// skipped, since stages are shared across runtimes; a preset its bundle
// doesn't have is an operator mistake.
func (s *Service) stageOffer(rt Runtime, ro *RuntimeOffer) ([]OfferedStage, []string, error) {
	stages := make([]OfferedStage, 0, len(s.paas.Pipeline.Stages))
	for _, st := range s.paas.Pipeline.Stages {
		stage := OfferedStage{ID: st.ID, Name: st.Name, Presets: map[string]string{}, Addons: []string{}}
		for slug, preset := range st.Presets {
			all, has := ro.presets[slug]
			if !has {
				continue
			}
			if _, ok := all[preset]; !ok {
				return nil, nil, fmt.Errorf("paas.pipeline: stage %q picks %q for %s, which the %s template's bundle has no preset for", st.ID, preset, slug, rt.ID)
			}
			if c, ok := offered(ro.Components, slug); ok && hasPreset(c, preset) {
				stage.Presets[slug] = preset
			}
			if c, ok := offered(ro.Addons, slug); ok {
				stage.Addons = append(stage.Addons, slug)
				if hasPreset(c, preset) {
					stage.Presets[slug] = preset
				}
			}
		}
		slices.Sort(stage.Addons)
		stages = append(stages, stage)
	}
	promote := []string{}
	for _, slug := range s.paas.Pipeline.Promote {
		if _, has := ro.presets[slug]; has {
			promote = append(promote, slug)
		}
	}
	return stages, promote, nil
}

func offered(cs []OfferedComponent, slug string) (OfferedComponent, bool) {
	i := slices.IndexFunc(cs, func(c OfferedComponent) bool { return c.Slug == slug })
	if i < 0 {
		return OfferedComponent{}, false
	}
	return cs[i], true
}

func hasPreset(c OfferedComponent, name string) bool {
	return slices.ContainsFunc(c.Presets, func(p OfferedPreset) bool { return p.Name == name })
}

var stagePattern = regexp.MustCompile(`\[stage ([a-z0-9]+)\]`)

func stageOf(description string) string {
	if m := stagePattern.FindStringSubmatch(description); m != nil {
		return m[1]
	}
	return ""
}

// PromoteInput names the app a promotion copies into.
type PromoteInput struct {
	To string `json:"to"`
}

// Promote copies the config and bundle version of the pipeline's promoted
// components from one app to an app in a later stage, keeping the fields the
// target's presets and pinned params own, then deploys what changed.
func (s *Service) Promote(ctx context.Context, user *User, account *Account, id string, in PromoteInput) (*Run, error) {
	source, ro, err := s.tenantApp(ctx, account, id)
	if err != nil {
		return nil, err
	}
	if in.To == "" || in.To == source.ID {
		return nil, Invalid("to", "pick another app to promote to")
	}
	target, err := s.ownedEnvironment(ctx, account, in.To)
	if err != nil {
		return nil, Invalid("to", "you have no app %q", in.To)
	}
	if target.State == EnvironmentStateDecommissioning {
		return nil, fmt.Errorf("%w: %s is being deleted", ErrConflict, target.Name)
	}
	if target.Runtime != source.Runtime {
		return nil, Invalid("to", "%s runs %s and %s runs %s; promote only between apps of one runtime", source.Name, source.Runtime, target.Name, target.Runtime)
	}
	from, to := s.paas.Pipeline.index(source.Stage), s.paas.Pipeline.index(target.Stage)
	if from < 0 || to <= from {
		return nil, Invalid("to", "promote to an app in a later stage than %s", source.Name)
	}
	var components []string
	for _, slug := range ro.Promote {
		_, inSource := source.Presets[slug]
		_, inTarget := target.Presets[slug]
		if inSource && inTarget {
			components = append(components, slug)
		}
	}
	if len(components) == 0 {
		return nil, Invalid("to", "%s and %s share nothing to promote", source.Name, target.Name)
	}

	rt := s.runtimeConfig(target.Runtime)
	vars := appVars(target, rt)
	overrides := make(map[string]map[string]any, len(components))
	for _, slug := range components {
		preset := maps.Clone(ro.presets[slug][target.Presets[slug]])
		overrides[slug] = MergeParams(MergeParams(map[string]any{}, preset), s.pinnedFor(rt, slug, vars))
	}
	platform := s.platforms(s.shared)
	sourceID, targetID, links := source.EnvironmentID, target.EnvironmentID, ro.links
	message := "Promoted from " + source.Name
	return s.startRun(ctx, user, target, TriggerPromote, message, func(bg context.Context) error {
		before, err := platform.GetEnvironment(bg, targetID)
		if err != nil {
			return fmt.Errorf("load %s: %w", targetID, err)
		}
		for _, slug := range components {
			if err := platform.CopyInstance(bg, sourceID+"-"+slug, targetID+"-"+slug, overrides[slug]); err != nil {
				return fmt.Errorf("copy %s: %w", slug, err)
			}
		}
		after, err := platform.GetEnvironment(bg, targetID)
		if err != nil {
			return fmt.Errorf("load %s: %w", targetID, err)
		}
		params := changed(before, after, components)
		return s.deployInLayers(bg, platform, targetID, params, links, message+" by "+user.Login)
	})
}

// changed returns the params of each component whose params or version differ
// between two reads of an environment.
func changed(before, after *PlatformEnvironment, slugs []string) map[string]map[string]any {
	was := map[string]PlatformInstance{}
	for _, pi := range before.Instances {
		was[pi.ComponentSlug] = pi
	}
	out := map[string]map[string]any{}
	for _, pi := range after.Instances {
		if !slices.Contains(slugs, pi.ComponentSlug) {
			continue
		}
		old := was[pi.ComponentSlug]
		if pi.Version != old.Version || !reflect.DeepEqual(pi.Params, old.Params) {
			out[pi.ComponentSlug] = pi.Params
		}
	}
	return out
}

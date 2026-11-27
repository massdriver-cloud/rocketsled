package domain

import (
	"context"
	"fmt"
	"slices"
	"strings"
)

// AddonInput adds an add-on to an app.
type AddonInput struct {
	Component string `json:"component"`
	Preset    string `json:"preset"`
}

// AddAddon deploys an add-on the app left out, then redeploys the components
// linked to it so they pick up the new connection.
func (s *Service) AddAddon(ctx context.Context, user *User, account *Account, id string, in AddonInput) (*Run, error) {
	env, ro, err := s.tenantApp(ctx, account, id)
	if err != nil {
		return nil, err
	}
	i := slices.IndexFunc(ro.Addons, func(c OfferedComponent) bool { return c.Slug == in.Component })
	if i < 0 {
		return nil, Invalid("component", "%s has no add-on %q", ro.Name, in.Component)
	}
	addon := ro.Addons[i]
	if _, ok := env.Presets[addon.Slug]; ok {
		return nil, fmt.Errorf("%w: %s already has %s", ErrConflict, env.Name, addon.Name)
	}
	preset := OfferedPreset{Params: map[string]any{}}
	if len(addon.Presets) > 0 {
		preset = addon.Presets[0]
	}
	if in.Preset != "" {
		j := slices.IndexFunc(addon.Presets, func(p OfferedPreset) bool { return p.Name == in.Preset })
		if j < 0 {
			return nil, Invalid("preset", "%s has no plan %q", addon.Name, in.Preset)
		}
		preset = addon.Presets[j]
	}

	platform := s.platforms(s.shared)
	live, err := platform.GetEnvironment(ctx, env.EnvironmentID)
	if err != nil {
		return nil, fmt.Errorf("load %s: %w", env.EnvironmentID, err)
	}
	if env.Presets == nil {
		env.Presets = map[string]string{}
	}
	env.Presets[addon.Slug] = preset.Name
	env.Addons = append(env.Addons, addon.Slug)
	description := withPicks(live.Description, env.Presets)

	rt := s.runtimeConfig(env.Runtime)
	params := s.consumerParams(live, env, ro.links, addon.Slug, rt)
	params[addon.Slug] = s.componentParams(env, rt, addon.Slug, MergeParams(map[string]any{}, preset.Params))
	links := ro.links
	return s.startRun(ctx, user, env, TriggerAddon, "Added "+addon.Name, func(bg context.Context) error {
		if err := platform.SetEnvironmentDescription(bg, env.EnvironmentID, description); err != nil {
			return fmt.Errorf("record %s on %s: %w", addon.Slug, env.EnvironmentID, err)
		}
		return s.deployInLayers(bg, platform, env.EnvironmentID, params, links, "Added "+addon.Name+" for "+user.Login)
	})
}

// RemoveAddon decommissions an add-on, which deletes its data, then redeploys
// the components linked to it without the connection.
func (s *Service) RemoveAddon(ctx context.Context, user *User, account *Account, id, component string) (*Run, error) {
	env, ro, err := s.tenantApp(ctx, account, id)
	if err != nil {
		return nil, err
	}
	if !slices.Contains(env.Addons, component) {
		return nil, fmt.Errorf("%w: %s has no add-on %q", ErrNotFound, env.Name, component)
	}
	name := component
	for _, c := range ro.Addons {
		if c.Slug == component {
			name = c.Name
		}
	}
	platform := s.platforms(s.shared)
	live, err := platform.GetEnvironment(ctx, env.EnvironmentID)
	if err != nil {
		return nil, fmt.Errorf("load %s: %w", env.EnvironmentID, err)
	}
	var current map[string]any
	deployed := false
	for _, pi := range live.Instances {
		if pi.ComponentSlug == component {
			current = pi.Params
			deployed = !strings.EqualFold(pi.Status, "INITIALIZED") && !strings.EqualFold(pi.Status, "DECOMMISSIONED")
		}
	}
	rt := s.runtimeConfig(env.Runtime)
	params := s.consumerParams(live, env, ro.links, component, rt)
	envRecordID, envID, links := env.ID, env.EnvironmentID, ro.links
	return s.startRun(ctx, user, env, TriggerAddon, "Removed "+name, func(bg context.Context) error {
		if deployed {
			if err := platform.DecommissionInstance(bg, envID+"-"+component, current, "Removed by "+user.Login); err != nil {
				return fmt.Errorf("remove %s: %w", component, err)
			}
			if err := s.waitFor(bg, platform, envID, []string{component}, "DECOMMISSIONED", "removing"); err != nil {
				return err
			}
		}
		stored, err := s.store.GetEnvironment(bg, envRecordID)
		if err != nil {
			return err
		}
		delete(stored.Presets, component)
		stored.Addons = slices.DeleteFunc(stored.Addons, func(a string) bool { return a == component })
		if err := platform.SetEnvironmentDescription(bg, envID, withPicks(live.Description, stored.Presets)); err != nil {
			return fmt.Errorf("record removing %s on %s: %w", component, envID, err)
		}
		if err := s.store.UpdateEnvironment(bg, stored); err != nil {
			return err
		}
		return s.deployInLayers(bg, platform, envID, params, links, "Removed "+name+" for "+user.Login)
	})
}

// tenantApp loads an app the account owns with its runtime's offer.
func (s *Service) tenantApp(ctx context.Context, account *Account, id string) (*Environment, *RuntimeOffer, error) {
	if !s.tenant() {
		return nil, nil, fmt.Errorf("%w: this only exists in tenant mode", ErrNotFound)
	}
	env, err := s.ownedEnvironment(ctx, account, id)
	if err != nil {
		return nil, nil, err
	}
	if env.State == EnvironmentStateDecommissioning {
		return nil, nil, fmt.Errorf("%w: %s is being deleted", ErrConflict, env.Name)
	}
	offer, err := s.Offer(ctx)
	if err != nil {
		return nil, nil, err
	}
	ro, ok := offer.runtime(env.Runtime)
	if !ok {
		return nil, nil, fmt.Errorf("%w: runtime %q is no longer offered", ErrConflict, env.Runtime)
	}
	return env, ro, nil
}

// consumerParams are the current params, with pinned params over them, of every
// picked component linked from producer.
func (s *Service) consumerParams(live *PlatformEnvironment, env *Environment, links []Link, producer string, rt Runtime) map[string]map[string]any {
	params := map[string]map[string]any{}
	for _, l := range links {
		if l.From != producer || l.To == producer {
			continue
		}
		if _, picked := env.Presets[l.To]; !picked {
			continue
		}
		for _, pi := range live.Instances {
			if pi.ComponentSlug == l.To {
				params[l.To] = s.componentParams(env, rt, l.To, pi.Params)
			}
		}
	}
	return params
}

// componentParams are base with pinned params and the customer's domains over
// it, and the image last released to the app component over those. Every
// deploy of a tenant app builds its params here.
func (s *Service) componentParams(env *Environment, rt Runtime, slug string, base map[string]any) map[string]any {
	out := MergeParams(base, s.fixedParams(rt, env, slug, appVars(env, rt)))
	if env.Image != "" && slug == rt.AppComponent && rt.ImageParam != "" {
		out[rt.ImageParam] = env.Image
	}
	return out
}

func (s *Service) pinnedFor(rt Runtime, slug string, vars map[string]string) map[string]any {
	return MergeParams(renderPinned(s.paas.Pinned[slug], vars), renderPinned(rt.Pinned[slug], vars))
}

// fixedParams are the params every deploy of slug applies over presets and
// current values: the operator's pinned params, then the customer's domains,
// which must outlive a pinned default for the same param.
func (s *Service) fixedParams(rt Runtime, env *Environment, slug string, vars map[string]string) map[string]any {
	out := s.pinnedFor(rt, slug, vars)
	if d := s.paas.Domains; d != nil && env != nil && env.Domains != nil && slug == d.Component {
		// No domains means the operator's pinned value, when there is one;
		// otherwise the param is emptied so a domain once set can't linger.
		if _, pinned := out[d.Param]; len(env.Domains.Hosts) > 0 || !pinned {
			out[d.Param] = env.Domains.value()
		}
	}
	return out
}

func appVars(env *Environment, rt Runtime) map[string]string {
	return map[string]string{"environment_id": env.EnvironmentID, "app": env.Slug, "runtime": rt.ID}
}

// withPicks rewrites the picks recorded in an app's description, so a server
// that loses its database still knows what the app deploys.
func withPicks(description string, picks map[string]string) string {
	marker := picksMarker(picks)
	if picksPattern.MatchString(description) {
		return picksPattern.ReplaceAllLiteralString(description, marker)
	}
	if i := strings.Index(description, "[owner "); i >= 0 {
		return description[:i] + marker + " " + description[i:]
	}
	return strings.TrimSpace(description + " " + marker)
}

func picksMarker(picks map[string]string) string {
	parts := make([]string, 0, len(picks))
	for slug, preset := range picks {
		parts = append(parts, slug+"="+preset)
	}
	slices.Sort(parts)
	return "[deploy " + strings.Join(parts, ",") + "]"
}

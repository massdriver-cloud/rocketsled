package domain

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
)

// PlatformLayer is a project deployed once whose resources every runtime
// template uses as environment defaults.
type PlatformLayer struct {
	Project     string
	Environment string
	Domain      string
	Components  []PlatformComponent
	Shares      []Share
}

// PlatformComponent deploys once with a preset, and params over it.
type PlatformComponent struct {
	Slug   string
	Preset string
	Params map[string]any
}

// Share is a platform resource, component.field, every runtime template gets as a default.
type Share struct {
	Component string
	Field     string
}

// PlatformState is how far the platform layer has come.
type PlatformState string

const (
	PlatformStateDeploying PlatformState = "deploying"
	PlatformStateRunning   PlatformState = "running"
	PlatformStateFailed    PlatformState = "failed"
)

// PlatformStatus is the platform layer as customers see it.
type PlatformStatus struct {
	State PlatformState `json:"state"`
	Error string        `json:"error,omitempty"`
}

type platformRun struct {
	sync.Mutex
	busy   bool
	status *PlatformStatus
}

func (l *PlatformLayer) environmentID() string { return l.Project + "-" + l.Environment }

// EnsurePlatform deploys each platform component that isn't provisioned, in
// order, then grants each shared resource to every runtime project and pins it
// on every runtime template. Everything already done is skipped, so calling it
// again converges. Call it at startup and periodically.
func (s *Service) EnsurePlatform(ctx context.Context) error {
	if !s.tenant() || s.paas.Platform == nil {
		return nil
	}
	s.platformRun.Lock()
	if s.platformRun.busy {
		s.platformRun.Unlock()
		return nil
	}
	s.platformRun.busy = true
	s.platformRun.Unlock()
	defer func() {
		s.platformRun.Lock()
		s.platformRun.busy = false
		s.platformRun.Unlock()
	}()

	err := s.ensurePlatform(ctx)
	status := &PlatformStatus{State: PlatformStateRunning}
	if err != nil {
		status = &PlatformStatus{State: PlatformStateFailed, Error: err.Error()}
		if errors.Is(err, errPlatformDeploying) {
			status = &PlatformStatus{State: PlatformStateDeploying}
		}
	}
	s.platformRun.Lock()
	s.platformRun.status = status
	s.platformRun.Unlock()
	return err
}

var errPlatformDeploying = errors.New("the platform is still deploying")

func (s *Service) ensurePlatform(ctx context.Context) error {
	layer := s.paas.Platform
	platform := s.platforms(s.shared)
	envID := layer.environmentID()
	live, err := platform.GetEnvironment(ctx, envID)
	if err != nil {
		return fmt.Errorf("load platform %s: %w", envID, err)
	}
	instances := map[string]PlatformInstance{}
	for _, pi := range live.Instances {
		instances[pi.ComponentSlug] = pi
	}
	vars := map[string]string{"platform_domain": layer.Domain}
	for _, c := range layer.Components {
		pi, ok := instances[c.Slug]
		if !ok {
			return fmt.Errorf("platform %s has no component %q", envID, c.Slug)
		}
		if strings.EqualFold(pi.Status, "PROVISIONED") {
			continue
		}
		s.platformRun.Lock()
		s.platformRun.status = &PlatformStatus{State: PlatformStateDeploying}
		s.platformRun.Unlock()
		if inProgress(pi.Status) {
			// A deploy a previous server started is still going; Massdriver
			// would refuse another, so this one only waits for it.
			if err := s.waitProvisioned(ctx, platform, envID, []string{c.Slug}); err != nil {
				return err
			}
			continue
		}
		i := slices.IndexFunc(pi.Presets(), func(p Preset) bool { return p.Name == c.Preset })
		if i < 0 {
			return fmt.Errorf("platform component %s has no preset %q", c.Slug, c.Preset)
		}
		params := MergeParams(MergeParams(map[string]any{}, pi.Presets()[i].Params), renderPinned(c.Params, vars))
		s.log.Info("platform: deploying", "component", c.Slug, "preset", c.Preset)
		if err := platform.DeployInstance(ctx, pi.ID, params, "Platform layer deployed by Rocketsled"); err != nil {
			return fmt.Errorf("deploy platform %s: %w", c.Slug, err)
		}
		if err := s.waitProvisioned(ctx, platform, envID, []string{c.Slug}); err != nil {
			return err
		}
	}
	for _, share := range layer.Shares {
		pi, ok := instances[share.Component]
		if !ok {
			return fmt.Errorf("platform %s has no component %q to share", envID, share.Component)
		}
		managed := slices.ContainsFunc(layer.Components, func(c PlatformComponent) bool { return c.Slug == share.Component })
		if !managed && !strings.EqualFold(pi.Status, "PROVISIONED") {
			if inProgress(pi.Status) {
				return fmt.Errorf("%w: %s is %s", errPlatformDeploying, pi.ID, strings.ToLower(pi.Status))
			}
			return fmt.Errorf("%s is %s; deploy it, or list %s under paas.platform.components", pi.ID, strings.ToLower(pi.Status), share.Component)
		}
		resourceID := pi.ID + "." + share.Field
		projects, templates := s.runtimeProjects(), make([]string, 0, len(s.paas.Runtimes))
		for _, rt := range s.paas.Runtimes {
			templates = append(templates, templateID(rt))
		}
		if s.appProjects() {
			// Apps made before a share was added get it too.
			apps, err := s.appsHere(ctx, platform)
			if err != nil {
				return err
			}
			for i := range apps {
				projects = append(projects, apps[i].ID)
				templates = append(templates, apps[i].templateID())
			}
		}
		for _, project := range projects {
			if err := platform.GrantResourceToProject(ctx, resourceID, project); err != nil {
				return fmt.Errorf("share %s with %s: %w", resourceID, project, err)
			}
		}
		for _, tmpl := range templates {
			pinned, err := platform.EnvironmentDefaults(ctx, tmpl)
			if err != nil {
				return fmt.Errorf("read defaults of %s: %w", tmpl, err)
			}
			if slices.Contains(pinned, resourceID) {
				continue
			}
			if err := platform.SetEnvironmentDefault(ctx, tmpl, resourceID); err != nil {
				return fmt.Errorf("pin %s on %s: %w", resourceID, tmpl, err)
			}
		}
	}
	return nil
}

// inProgress is a status on the way to PROVISIONED or DECOMMISSIONED.
func inProgress(status string) bool {
	switch strings.ToUpper(status) {
	case "PROVISIONED", "INITIALIZED", "FAILED", "DECOMMISSIONED":
		return false
	}
	return true
}

func (s *Service) runtimeProjects() []string {
	var out []string
	for _, rt := range s.paas.Runtimes {
		if !slices.Contains(out, rt.Project) {
			out = append(out, rt.Project)
		}
	}
	return out
}

// PlatformStatus reports the platform layer, or nil when there is none.
func (s *Service) PlatformStatus() *PlatformStatus {
	if !s.tenant() || s.paas.Platform == nil {
		return nil
	}
	s.platformRun.Lock()
	defer s.platformRun.Unlock()
	if s.platformRun.status == nil {
		return &PlatformStatus{State: PlatformStateDeploying}
	}
	st := *s.platformRun.status
	return &st
}

package domain

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

// PreviewInput releases an image to a preview, making the preview first if needed.
type PreviewInput struct {
	Image       string `json:"image"`
	Description string `json:"description,omitempty"`
	// Runtime is required when the PaaS offers more than one.
	Runtime string `json:"runtime,omitempty"`
}

// Preview makes the app named name with the preview presets if it doesn't
// exist, then releases the image to it. A build calls it on every push to a
// branch or pull request.
func (s *Service) Preview(ctx context.Context, user *User, account *Account, name string, in PreviewInput) (*Release, error) {
	if !s.tenant() || s.paas.Previews == nil {
		return nil, fmt.Errorf("%w: previews are not offered here", ErrNotFound)
	}
	name = strings.ToLower(strings.TrimSpace(name))
	if !ValidSlug(name) || name == "template" {
		return nil, Invalid("name", "use 1-20 lowercase letters or digits, like pr123")
	}
	existing, err := s.environmentByRef(ctx, account, name)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return nil, err
	}
	if existing != nil {
		if !existing.Preview {
			return nil, fmt.Errorf("%w: %s is not a preview", ErrConflict, name)
		}
		return s.Release(ctx, user, account, existing.ID, ReleaseInput{Image: in.Image, Description: in.Description})
	}

	image := strings.TrimSpace(in.Image)
	if image == "" {
		return nil, Invalid("image", "name the image to release")
	}
	rt, err := s.previewRuntime(in.Runtime)
	if err != nil {
		return nil, err
	}
	if rt.AppComponent == "" || rt.ImageParam == "" {
		return nil, fmt.Errorf("%w: %s has no app component to release images to", ErrConflict, rt.Name)
	}
	offer, err := s.Offer(ctx)
	if err != nil {
		return nil, err
	}
	ro, ok := offer.runtime(rt.ID)
	if !ok {
		return nil, Invalid("runtime", "%q is not offered", rt.ID)
	}
	tmpl, err := s.platforms(s.shared).GetEnvironment(ctx, templateID(rt))
	if err != nil {
		return nil, fmt.Errorf("load template %q: %w", templateID(rt), err)
	}
	for _, pi := range tmpl.Instances {
		if pi.ComponentSlug == rt.AppComponent {
			if err := checkParam(pi.ParamsSchema, rt.ImageParam, image); err != nil {
				return nil, err
			}
		}
	}

	presets := map[string]string{}
	var addons []string
	for slug, preset := range s.paas.Previews.Presets {
		switch {
		case slices.ContainsFunc(ro.Components, func(c OfferedComponent) bool { return c.Slug == slug }):
			presets[slug] = preset
		case slices.ContainsFunc(ro.Addons, func(c OfferedComponent) bool { return c.Slug == slug }):
			presets[slug] = preset
			addons = append(addons, slug)
		}
	}
	slices.Sort(addons)
	description := strings.TrimSpace(in.Description)
	env, err := s.CreateEnvironment(ctx, user, account, CreateEnvironmentInput{
		Slug: name, Name: name, Runtime: rt.ID, Presets: presets, Addons: addons, Preview: true, Image: image,
	})
	if err != nil {
		return nil, err
	}
	runs, err := s.store.ListRuns(ctx, env.ID, 1)
	if err != nil {
		return nil, err
	}
	release := &Release{ID: s.newID(), EnvironmentID: env.ID, Number: 1, Image: image, Description: description, RequestedBy: user.Login, CreatedAt: s.now()}
	if len(runs) > 0 {
		release.RunID = runs[0].ID
	}
	if err := s.store.CreateRelease(ctx, release); err != nil {
		return nil, err
	}
	return release, nil
}

// DeletePreview removes a preview, when its pull request closes, say.
func (s *Service) DeletePreview(ctx context.Context, account *Account, name string) (*Environment, error) {
	if !s.tenant() || s.paas.Previews == nil {
		return nil, fmt.Errorf("%w: previews are not offered here", ErrNotFound)
	}
	env, err := s.environmentByRef(ctx, account, strings.ToLower(strings.TrimSpace(name)))
	if err != nil {
		return nil, err
	}
	if !env.Preview {
		return nil, fmt.Errorf("%w: %s is not a preview", ErrConflict, env.Slug)
	}
	return s.DestroyEnvironment(ctx, account, env.ID)
}

func (s *Service) previewRuntime(id string) (Runtime, error) {
	if id == "" && len(s.paas.Runtimes) == 1 {
		return s.paas.Runtimes[0], nil
	}
	for _, rt := range s.paas.Runtimes {
		if rt.ID == id {
			return rt, nil
		}
	}
	ids := make([]string, 0, len(s.paas.Runtimes))
	for _, rt := range s.paas.Runtimes {
		ids = append(ids, rt.ID)
	}
	return Runtime{}, Invalid("runtime", "name the runtime: %s", strings.Join(ids, ", "))
}

// ReapPreviews removes previews whose last release is older than the
// previews TTL. Nothing else is touched. Call it periodically; ReapExpired
// deletes them once their resources are gone.
func (s *Service) ReapPreviews(ctx context.Context) {
	if !s.tenant() || s.paas.Previews == nil || s.paas.Previews.TTL == 0 {
		return
	}
	platform := s.platforms(s.shared)
	all, _, err := s.madeHere(ctx, platform)
	if err != nil {
		s.log.Warn("preview reaper: listing apps", "err", err)
		return
	}
	for _, e := range all {
		if !strings.Contains(e.Description, previewMarker) {
			continue
		}
		last := e.CreatedAt
		releases, err := s.store.ListReleases(ctx, e.ID, 1)
		if err != nil {
			// Without the release history a busy preview looks quiet; wait for the next tick.
			continue
		}
		if len(releases) > 0 && releases[0].CreatedAt.After(last) {
			last = releases[0].CreatedAt
		}
		if s.now().Sub(last) < s.paas.Previews.TTL {
			continue
		}
		pe, err := platform.GetEnvironment(ctx, e.ID)
		if err != nil || allDecommissioned(pe) {
			continue
		}
		if !s.reaped.try(e.ID, s.now()) {
			continue
		}
		if err := platform.DecommissionEnvironment(ctx, e.ID); err != nil {
			s.log.Warn("preview reaper: decommission", "environment", e.ID, "err", err)
			continue
		}
		s.log.Info("preview reaper: removing a quiet preview", "environment", e.ID, "last release", last)
		if env, err := s.store.GetEnvironment(ctx, e.ID); err == nil {
			env.State = EnvironmentStateDecommissioning
			env.UpdatedAt = s.now()
			_ = s.store.UpdateEnvironment(ctx, env)
		}
	}
}

// try reports whether id may be reaped now, at most once an hour.
func (r *reapLog) try(id string, now time.Time) bool {
	r.Lock()
	defer r.Unlock()
	if last, ok := r.at[id]; ok && now.Sub(last) < time.Hour {
		return false
	}
	r.at[id] = now
	return true
}

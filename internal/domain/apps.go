package domain

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
)

// App is a customer's app with its own Massdriver project, made from a
// runtime's template. Its environments fork the project's template.
type App struct {
	ID           string        `json:"id"`
	AccountID    string        `json:"-"`
	Name         string        `json:"name"`
	Runtime      string        `json:"runtime"`
	CreatedBy    string        `json:"createdBy"`
	CreatedAt    time.Time     `json:"createdAt"`
	Environments []Environment `json:"environments"`
	environments []EnvironmentSummary
	owner        string
}

// CreateAppInput makes an app.
type CreateAppInput struct {
	Name    string `json:"name"`
	Runtime string `json:"runtime"`
}

// appTemplate is the environment in an app's project that its environments fork.
const appTemplate = "template"

func (a *App) templateID() string { return a.ID + "-" + appTemplate }

func (s *Service) appProjects() bool {
	return s.tenant() && slices.ContainsFunc(s.paas.Runtimes, func(rt Runtime) bool { return rt.AppProjects })
}

// The runtime is written into an app project's description next to its owner,
// so the server can adopt its apps after losing its database.
var runtimePattern = regexp.MustCompile(`\[runtime ([^\]\s]+)\]`)

func describeAppProject(login, userID, runtime string) string {
	return fmt.Sprintf("Made in Rocketsled for %s [runtime %s] [owner %s]", login, runtime, userID)
}

// CreateApp makes the app's project from the runtime's template, then gives its
// template every default the runtime's template has: the shared codebase, the
// platform's network and cluster, the cloud credential. Retrying converges.
func (s *Service) CreateApp(ctx context.Context, user *User, account *Account, in CreateAppInput) (*App, error) {
	if !s.appProjects() {
		return nil, fmt.Errorf("%w: apps are not their own projects here", ErrNotFound)
	}
	rt := s.runtimeConfig(in.Runtime)
	if rt.ID == "" || !rt.AppProjects {
		return nil, Invalid("runtime", "pick a runtime")
	}
	id := strings.TrimSpace(in.Name)
	if !ValidSlug(id) {
		return nil, Invalid("name", "use 1-20 lowercase letters or digits")
	}
	if s.reservedProject(id) {
		return nil, Invalid("name", "that name is reserved")
	}
	if st := s.PlatformStatus(); st != nil && st.State != PlatformStateRunning {
		return nil, fmt.Errorf("%w: the platform every app runs on is still being set up; try again in a few minutes", ErrConflict)
	}
	platform := s.platforms(s.shared)
	existing, err := platform.GetProject(ctx, id)
	if errors.Is(err, ErrNotFound) {
		if err := s.checkAppLimits(ctx, platform, account); err != nil {
			return nil, err
		}
	}
	switch {
	case err == nil:
		if ownerOf(existing.Description) != account.OwnerUserID {
			return nil, Invalid("name", "%s is taken; try another name", id)
		}
		if m := runtimePattern.FindStringSubmatch(existing.Description); m == nil || m[1] != rt.ID {
			return nil, Invalid("name", "you already have %s on another runtime", id)
		}
	case errors.Is(err, ErrNotFound):
		err = platform.CreateProjectFromEnvironment(ctx, templateID(rt), NewProject{
			ID: id, Name: id, Description: describeAppProject(user.Login, account.OwnerUserID, rt.ID),
			EnvironmentID: appTemplate, EnvironmentName: "Template",
		})
		if err != nil {
			return nil, fmt.Errorf("create %s: %w", id, err)
		}
	default:
		return nil, fmt.Errorf("look up %s: %w", id, err)
	}
	app := &App{ID: id, AccountID: account.ID, Name: id, Runtime: rt.ID, CreatedBy: user.ID, CreatedAt: s.now(), Environments: []Environment{}}
	if err := s.copyDefaults(ctx, platform, templateID(rt), app); err != nil {
		return nil, err
	}
	if err := s.store.CreateApp(ctx, app); err != nil {
		if !errors.Is(err, ErrConflict) {
			return nil, err
		}
		return s.store.GetApp(ctx, app.ID)
	}
	return app, nil
}

// checkAppLimits holds app projects to the same caps as apps: a customer
// can't make more projects than they may have environments.
func (s *Service) checkAppLimits(ctx context.Context, platform Platform, account *Account) error {
	l := s.paas.Limits
	if l.AppsPerPerson == 0 && l.TotalApps == 0 {
		return nil
	}
	apps, err := s.appsHere(ctx, platform)
	if err != nil {
		return err
	}
	mine := 0
	for _, a := range apps {
		if a.owner == account.OwnerUserID {
			mine++
		}
	}
	if l.AppsPerPerson > 0 && mine >= l.AppsPerPerson {
		return fmt.Errorf("%w: you can have %d at a time; delete one first", ErrConflict, l.AppsPerPerson)
	}
	if l.TotalApps > 0 && len(apps) >= l.TotalApps {
		return fmt.Errorf("%w: this demo is full right now; try again later", ErrConflict)
	}
	return nil
}

// copyDefaults shares each default of the runtime's template with the app's
// project and pins it on the app's template, unless it already is.
func (s *Service) copyDefaults(ctx context.Context, platform Platform, from string, app *App) error {
	want, err := platform.EnvironmentDefaults(ctx, from)
	if err != nil {
		return fmt.Errorf("read defaults of %s: %w", from, err)
	}
	have, err := platform.EnvironmentDefaults(ctx, app.templateID())
	if err != nil {
		return fmt.Errorf("read defaults of %s: %w", app.templateID(), err)
	}
	for _, resourceID := range want {
		if slices.Contains(have, resourceID) {
			continue
		}
		if err := platform.GrantResourceToProject(ctx, resourceID, app.ID); err != nil {
			return fmt.Errorf("share %s with %s: %w", resourceID, app.ID, err)
		}
		if err := platform.SetEnvironmentDefault(ctx, app.templateID(), resourceID); err != nil {
			return fmt.Errorf("pin %s on %s: %w", resourceID, app.templateID(), err)
		}
	}
	return nil
}

func (s *Service) reservedProject(id string) bool {
	if slices.Contains(s.runtimeProjects(), id) {
		return true
	}
	return s.paas.Platform != nil && s.paas.Platform.Project == id
}

// ListApps lists the account's apps with their environments, adopting apps
// Massdriver knows and the store has forgotten.
func (s *Service) ListApps(ctx context.Context, account *Account) ([]App, error) {
	if !s.appProjects() {
		return []App{}, nil
	}
	platform := s.platforms(s.shared)
	live, err := s.appsHere(ctx, platform)
	if err != nil {
		return nil, err
	}
	known, err := s.store.ListApps(ctx, account.ID)
	if err != nil {
		return nil, err
	}
	for _, a := range live {
		if a.owner != account.OwnerUserID || slices.ContainsFunc(known, func(k App) bool { return k.ID == a.ID }) {
			continue
		}
		adopted := App{ID: a.ID, AccountID: account.ID, Name: a.Name, Runtime: a.Runtime, CreatedBy: account.OwnerUserID, CreatedAt: s.now()}
		if err := s.store.CreateApp(ctx, &adopted); err != nil && !errors.Is(err, ErrConflict) {
			return nil, err
		}
		known = append(known, adopted)
	}
	envs, err := s.ListEnvironments(ctx, account)
	if err != nil {
		return nil, err
	}
	out := make([]App, 0, len(known))
	for _, a := range known {
		if !slices.ContainsFunc(live, func(l App) bool { return l.ID == a.ID }) {
			_ = s.store.DeleteApp(ctx, a.ID)
			continue
		}
		a.Environments = []Environment{}
		for _, e := range envs {
			if e.App == a.ID {
				a.Environments = append(a.Environments, e)
			}
		}
		out = append(out, a)
	}
	return out, nil
}

// DeleteApp removes an app's project once it has no environments but its template.
func (s *Service) DeleteApp(ctx context.Context, account *Account, id string) error {
	if !s.appProjects() {
		return fmt.Errorf("%w: apps are not their own projects here", ErrNotFound)
	}
	app, err := s.ownedApp(ctx, account, id)
	if err != nil {
		return err
	}
	platform := s.platforms(s.shared)
	project, err := platform.GetProject(ctx, app.ID)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return fmt.Errorf("load %s: %w", app.ID, err)
	}
	if project != nil {
		for _, e := range project.Environments {
			if e.ID != app.templateID() {
				return fmt.Errorf("%w: delete %s's environments first", ErrConflict, app.Name)
			}
		}
		if slices.ContainsFunc(project.Environments, func(e EnvironmentSummary) bool { return e.ID == app.templateID() }) {
			if err := platform.DeleteEnvironment(ctx, app.templateID()); err != nil {
				return fmt.Errorf("delete %s: %w", app.templateID(), err)
			}
		}
		if err := platform.DeleteProject(ctx, app.ID); err != nil {
			return fmt.Errorf("delete %s: %w", app.ID, err)
		}
	}
	return s.store.DeleteApp(ctx, app.ID)
}

func (s *Service) ownedApp(ctx context.Context, account *Account, id string) (*App, error) {
	app, err := s.store.GetApp(ctx, id)
	if errors.Is(err, ErrNotFound) && s.appProjects() {
		// The store may have lost an app Massdriver still has.
		if _, listErr := s.ListApps(ctx, account); listErr == nil {
			app, err = s.store.GetApp(ctx, id)
		}
	}
	if err != nil {
		return nil, err
	}
	if app.AccountID != account.ID {
		return nil, fmt.Errorf("%w: app %s", ErrNotFound, id)
	}
	return app, nil
}

// appsHere lists the app projects this server made, with their owners and environments.
func (s *Service) appsHere(ctx context.Context, platform Platform) ([]App, error) {
	projects, err := platform.ListProjects(ctx)
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}
	var out []App
	for _, p := range projects {
		owner := ownerOf(p.Description)
		m := runtimePattern.FindStringSubmatch(p.Description)
		if owner == "" || m == nil || !s.runtimeConfig(m[1]).AppProjects {
			continue
		}
		out = append(out, App{ID: p.ID, Name: p.Name, Runtime: m[1], owner: owner, environments: p.Environments})
	}
	return out, nil
}

func appOf(m madeIn) string {
	if m.Project == m.Runtime.Project {
		return ""
	}
	return m.Project
}

// appEnvironmentSlug is an app project environment's slug: its Massdriver ID,
// since two apps of one account may each have a production.
func appEnvironmentSlug(app, slug string) string { return app + "-" + slug }

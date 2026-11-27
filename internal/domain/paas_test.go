package domain_test

import (
	"context"
	"fmt"
	"maps"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/massdriver-cloud/rocketsled/internal/domain"
	"github.com/massdriver-cloud/rocketsled/internal/domain/mocks"
	"github.com/massdriver-cloud/rocketsled/internal/store/memory"
)

// tenantWorld is a Massdriver organization with one runtime, ruby, whose
// template has a shared registry, web dynos, and two add-ons linked to web.
type tenantWorld struct {
	mu           sync.Mutex
	descriptions map[string]string
	version      map[string]string
	deployed     map[string]string
	created      map[string]time.Time
	status       map[string]string
	params       map[string]map[string]any
}

func rubyProject() *domain.Project {
	return &domain.Project{
		ID: "ruby", Name: "Fauxroku Ruby",
		Components: []domain.Component{{Slug: "registry", Name: "Image Repository"}, {Slug: "web", Name: "Web Dynos"}, {Slug: "db", Name: "Postgres"}, {Slug: "cache", Name: "Key-Value Store"}},
		Links:      []domain.Link{{From: "db", To: "web"}, {From: "cache", To: "web"}, {From: "registry", To: "web"}},
	}
}

func presetSchema(names ...string) map[string]any {
	examples := make([]any, 0, len(names))
	for _, n := range names {
		examples = append(examples, map[string]any{"__name": n, "size": strings.ToLower(n)})
	}
	return map[string]any{
		"examples": examples,
		"properties": map[string]any{
			"image_uri": map[string]any{"type": "string", "pattern": `^[a-z0-9./:@_-]+$`, "message": map[string]any{"pattern": "use a lowercase image reference"}},
		},
	}
}

func (w *tenantWorld) environment(id string) *domain.PlatformEnvironment {
	w.mu.Lock()
	defer w.mu.Unlock()
	env := &domain.PlatformEnvironment{ID: id, Name: id, ProjectID: "ruby", Description: w.descriptions[id]}
	for _, slug := range []string{"registry", "web", "db", "cache"} {
		iid := id + "-" + slug
		status := w.status[iid]
		if status == "" {
			status = "INITIALIZED"
			if slug == "registry" && id == "ruby-template" {
				status = "PROVISIONED"
			}
		}
		env.Instances = append(env.Instances, domain.PlatformInstance{
			ID: iid, ComponentSlug: slug, ComponentName: slug, BundleName: slug, Status: status, Version: w.version[iid], DeployedVersion: w.deployed[iid],
			Params: maps.Clone(w.params[iid]), ParamsSchema: presetSchema("Development", "Production"),
		})
	}
	return env
}

func (w *tenantWorld) set(instanceID, status string, params map[string]any) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.status[instanceID] = status
	if params != nil {
		w.params[instanceID] = maps.Clone(params)
	}
}

type tenantHarness struct {
	svc      *domain.Service
	platform *mocks.PlatformMock
	store    *memory.Store
	world    *tenantWorld
	user     *domain.User
	account  *domain.Account
	now      time.Time
}

func newTenantHarness(t *testing.T, paas *domain.PaaS) *tenantHarness {
	t.Helper()
	if paas == nil {
		paas = &domain.PaaS{}
	}
	if len(paas.Runtimes) == 0 {
		paas.Runtimes = []domain.Runtime{{ID: "ruby", Name: "Ruby", Project: "ruby", Template: "template", AppComponent: "web", ImageParam: "image_uri"}}
	}
	if paas.Addons == nil {
		paas.Addons = []domain.Addon{{ID: "db", Title: "Postgres"}, {ID: "cache", Title: "Key-Value Store"}}
	}
	w := &tenantWorld{descriptions: map[string]string{}, version: map[string]string{}, deployed: map[string]string{}, created: map[string]time.Time{}, status: map[string]string{}, params: map[string]map[string]any{}}
	h := &tenantHarness{platform: &mocks.PlatformMock{}, store: memory.New(), world: w, now: fixedTime}
	h.user = &domain.User{ID: "password:ada", Provider: domain.ProviderPassword, Login: "ada", Name: "ada"}
	p := h.platform
	p.PingFunc = func(context.Context) (string, error) { return "Fauxroku", nil }
	p.GetProjectFunc = func(context.Context, string) (*domain.Project, error) {
		proj := rubyProject()
		w.mu.Lock()
		defer w.mu.Unlock()
		for id, d := range w.descriptions {
			proj.Environments = append(proj.Environments, domain.EnvironmentSummary{ID: id, Name: id, Description: d, CreatedAt: w.created[id]})
		}
		return proj, nil
	}
	p.GetEnvironmentFunc = func(_ context.Context, id string) (*domain.PlatformEnvironment, error) {
		w.mu.Lock()
		_, known := w.descriptions[id]
		w.mu.Unlock()
		if !known && id != "ruby-template" {
			return nil, domain.ErrNotFound
		}
		return w.environment(id), nil
	}
	p.ForkEnvironmentFunc = func(_ context.Context, req domain.ForkRequest) (*domain.PlatformEnvironment, error) {
		id := "ruby-" + req.ID
		w.mu.Lock()
		w.descriptions[id] = req.Description
		w.created[id] = h.now
		w.mu.Unlock()
		return w.environment(id), nil
	}
	p.DeployInstanceFunc = func(_ context.Context, id string, params map[string]any, _ string) error {
		w.set(id, "PROVISIONED", params)
		w.mu.Lock()
		w.deployed[id] = w.version[id]
		w.mu.Unlock()
		return nil
	}
	p.SetInstanceVersionFunc = func(_ context.Context, id, version string) error {
		w.mu.Lock()
		defer w.mu.Unlock()
		w.version[id] = version
		return nil
	}
	started := map[string]string{}
	p.StartDeploymentFunc = func(ctx context.Context, id string, params map[string]any, msg string) (string, error) {
		if err := p.DeployInstance(ctx, id, params, msg); err != nil {
			return "", err
		}
		w.mu.Lock()
		defer w.mu.Unlock()
		dep := fmt.Sprintf("dep-%d", len(started)+1)
		started[dep] = id
		return dep, nil
	}
	p.DeploymentStatusFunc = func(_ context.Context, dep string) (string, error) {
		w.mu.Lock()
		defer w.mu.Unlock()
		if w.status[started[dep]] == "FAILED" {
			return "FAILED", nil
		}
		return "COMPLETED", nil
	}
	p.DecommissionInstanceFunc = func(_ context.Context, id string, _ map[string]any, _ string) error {
		w.set(id, "DECOMMISSIONED", nil)
		return nil
	}
	p.SetEnvironmentDescriptionFunc = func(_ context.Context, id, d string) error {
		w.mu.Lock()
		defer w.mu.Unlock()
		w.descriptions[id] = d
		return nil
	}
	p.DecommissionEnvironmentFunc = func(context.Context, string) error { return nil }

	svc, err := domain.NewService(domain.Options{
		Platforms:        func(domain.Credential) domain.Platform { return p },
		Store:            h.store,
		Installer:        &mocks.InstallerMock{},
		Mode:             domain.AccountModeTenant,
		SharedCredential: domain.Credential{URL: "https://api.massdriver.cloud", OrganizationID: "fauxroku", APIKey: "md_operator"},
		PaaS:             paas,
		Password:         "correct horse",
		RunAsync:         func(f func()) { f() },
		Now:              func() time.Time { return h.now },
	})
	require.NoError(t, err)
	h.svc = svc
	require.NoError(t, h.store.UpsertUser(context.Background(), h.user))
	h.account, err = svc.CreateAccount(context.Background(), h.user, "")
	require.NoError(t, err)
	return h
}

func (h *tenantHarness) app(t *testing.T, slug string, addons ...string) *domain.Environment {
	t.Helper()
	env, err := h.svc.CreateEnvironment(context.Background(), h.user, h.account, domain.CreateEnvironmentInput{Slug: slug, Runtime: "ruby", Addons: addons})
	require.NoError(t, err)
	return env
}

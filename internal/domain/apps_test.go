package domain_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/massdriver-cloud/rocketsled/internal/domain"
)

func appProjectsPaaS() *domain.PaaS {
	return &domain.PaaS{
		Runtimes: []domain.Runtime{{ID: "ruby", Name: "Ruby", Project: "ruby", Template: "template", AppComponent: "web", ImageParam: "image_uri", AppProjects: true}},
		Previews: &domain.Previews{Presets: map[string]string{"web": "Development"}},
	}
}

func TestCreateAppMakesAProjectAndCopiesTheTemplateDefaults(t *testing.T) {
	h := newTenantHarness(t, appProjectsPaaS())
	ctx := context.Background()
	defaults := map[string][]string{"ruby-template": {"ruby-template-codebase.codebase", "platform-main-cluster.cluster", "res-role"}}
	h.platform.EnvironmentDefaultsFunc = func(_ context.Context, id string) ([]string, error) { return defaults[id], nil }
	h.platform.SetEnvironmentDefaultFunc = func(_ context.Context, env, resource string) error {
		defaults[env] = append(defaults[env], resource)
		return nil
	}

	app, err := h.svc.CreateApp(ctx, h.user, h.account, domain.CreateAppInput{Name: "shop", Runtime: "ruby"})
	require.NoError(t, err)
	assert.Equal(t, "shop", app.ID)

	made := h.platform.CreateProjectFromEnvironmentCalls()
	require.Len(t, made, 1)
	assert.Equal(t, "ruby-template", made[0].SourceEnvironmentID)
	assert.Equal(t, domain.NewProject{ID: "shop", Name: "shop", Description: "Made in Rocketsled for ada [runtime ruby] [owner password:ada]", EnvironmentID: "template", EnvironmentName: "Template"}, made[0].Req)
	assert.Equal(t, defaults["ruby-template"], defaults["shop-template"], "the app's template gets every default the runtime's has")
	calls := h.platform.GrantResourceToProjectCalls()
	grants := make([]string, 0, len(calls))
	for _, g := range calls {
		assert.Equal(t, "shop", g.ProjectID)
		grants = append(grants, g.ResourceID)
	}
	assert.Equal(t, defaults["ruby-template"], grants)

	again, err := h.svc.CreateApp(ctx, h.user, h.account, domain.CreateAppInput{Name: "shop", Runtime: "ruby"})
	require.NoError(t, err, "retrying converges")
	assert.Equal(t, "shop", again.ID)
	assert.Len(t, h.platform.CreateProjectFromEnvironmentCalls(), 1)
	assert.Len(t, h.platform.SetEnvironmentDefaultCalls(), 3, "defaults already pinned are left alone")

	env, err := h.svc.CreateEnvironment(ctx, h.user, h.account, domain.CreateEnvironmentInput{Slug: "production", App: "shop"})
	require.NoError(t, err)
	assert.Equal(t, "shop-production", env.EnvironmentID)
	assert.Equal(t, "shop-production", env.Slug, "two apps may each have a production")
	assert.Equal(t, "shop", env.App)
	assert.Equal(t, "ruby", env.Runtime)
	forks := h.platform.ForkEnvironmentCalls()
	assert.Equal(t, "shop-template", forks[len(forks)-1].Req.ParentEnvironmentID)

	apps, err := h.svc.ListApps(ctx, h.account)
	require.NoError(t, err)
	require.Len(t, apps, 1)
	require.Len(t, apps[0].Environments, 1)
	assert.Equal(t, "shop-production", apps[0].Environments[0].ID)
}

func TestAppRejects(t *testing.T) {
	ctx := context.Background()
	var verr *domain.ValidationError

	off := newTenantHarness(t, nil)
	_, err := off.svc.CreateApp(ctx, off.user, off.account, domain.CreateAppInput{Name: "shop", Runtime: "ruby"})
	require.ErrorIs(t, err, domain.ErrNotFound, "a runtime without app projects makes none")

	h := newTenantHarness(t, appProjectsPaaS())
	_, err = h.svc.CreateApp(ctx, h.user, h.account, domain.CreateAppInput{Name: "ruby", Runtime: "ruby"})
	require.ErrorAs(t, err, &verr, "the runtime's own project is reserved")
	_, err = h.svc.CreateApp(ctx, h.user, h.account, domain.CreateAppInput{Name: "My Shop", Runtime: "ruby"})
	require.ErrorAs(t, err, &verr)
	assert.Equal(t, "name", verr.Field)

	h.world.projects["taken"] = "Made in Rocketsled for bob [runtime ruby] [owner password:bob]"
	_, err = h.svc.CreateApp(ctx, h.user, h.account, domain.CreateAppInput{Name: "taken", Runtime: "ruby"})
	require.ErrorAs(t, err, &verr, "someone else's project is never adopted")

	_, err = h.svc.CreateEnvironment(ctx, h.user, h.account, domain.CreateEnvironmentInput{Slug: "production", Runtime: "ruby"})
	require.ErrorAs(t, err, &verr)
	assert.Equal(t, "app", verr.Field, "an environment of such a runtime belongs to an app")
	_, err = h.svc.CreateEnvironment(ctx, h.user, h.account, domain.CreateEnvironmentInput{Slug: "production", App: "taken"})
	require.ErrorAs(t, err, &verr)
}

func TestDeleteAppOnlyWhenEmpty(t *testing.T) {
	h := newTenantHarness(t, appProjectsPaaS())
	ctx := context.Background()
	_, err := h.svc.CreateApp(ctx, h.user, h.account, domain.CreateAppInput{Name: "shop", Runtime: "ruby"})
	require.NoError(t, err)
	_, err = h.svc.CreateEnvironment(ctx, h.user, h.account, domain.CreateEnvironmentInput{Slug: "production", App: "shop"})
	require.NoError(t, err)

	require.ErrorIs(t, h.svc.DeleteApp(ctx, h.account, "shop"), domain.ErrConflict)
	delete(h.world.descriptions, "shop-production")
	require.NoError(t, h.svc.DeleteApp(ctx, h.account, "shop"))
	assert.Equal(t, "shop-template", h.platform.DeleteEnvironmentCalls()[0].EnvironmentID)
	assert.Equal(t, "shop", h.platform.DeleteProjectCalls()[0].ProjectID)
	_, err = h.store.GetApp(ctx, "shop")
	require.ErrorIs(t, err, domain.ErrNotFound)
}

func TestAppsAndTheirEnvironmentsAreAdopted(t *testing.T) {
	h := newTenantHarness(t, appProjectsPaaS())
	ctx := context.Background()
	_, err := h.svc.CreateApp(ctx, h.user, h.account, domain.CreateAppInput{Name: "shop", Runtime: "ruby"})
	require.NoError(t, err)
	env, err := h.svc.CreateEnvironment(ctx, h.user, h.account, domain.CreateEnvironmentInput{Slug: "production", App: "shop"})
	require.NoError(t, err)

	require.NoError(t, h.store.DeleteApp(ctx, "shop"))
	require.NoError(t, h.store.DeleteEnvironment(ctx, env.ID))
	apps, err := h.svc.ListApps(ctx, h.account)
	require.NoError(t, err)
	require.Len(t, apps, 1)
	assert.Equal(t, "ruby", apps[0].Runtime)
	require.Len(t, apps[0].Environments, 1)
	adopted := apps[0].Environments[0]
	assert.Equal(t, "shop-production", adopted.Slug)
	assert.Equal(t, "shop", adopted.App)
}

func TestPreviewsLiveInTheirApp(t *testing.T) {
	h := newTenantHarness(t, appProjectsPaaS())
	ctx := context.Background()
	_, err := h.svc.CreateApp(ctx, h.user, h.account, domain.CreateAppInput{Name: "shop", Runtime: "ruby"})
	require.NoError(t, err)

	r, err := h.svc.Preview(ctx, h.user, h.account, "pr9", domain.PreviewInput{Image: "img:1", App: "shop"})
	require.NoError(t, err)
	assert.Equal(t, "shop-pr9", r.EnvironmentID)
	r, err = h.svc.Preview(ctx, h.user, h.account, "pr9", domain.PreviewInput{Image: "img:2", App: "shop"})
	require.NoError(t, err)
	assert.Equal(t, 2, r.Number)

	env, err := h.svc.DeletePreview(ctx, h.account, "pr9", "shop")
	require.NoError(t, err)
	assert.Equal(t, "shop-pr9", env.ID)
}

func TestRetryingAnAppOnAnotherRuntimeIsRefused(t *testing.T) {
	paas := appProjectsPaaS()
	paas.Runtimes = append(paas.Runtimes, domain.Runtime{ID: "node", Name: "Node", Project: "ruby", Template: "template", AppProjects: true})
	h := newTenantHarness(t, paas)
	ctx := context.Background()
	_, err := h.svc.CreateApp(ctx, h.user, h.account, domain.CreateAppInput{Name: "shop", Runtime: "node"})
	require.NoError(t, err)
	var verr *domain.ValidationError
	_, err = h.svc.CreateApp(ctx, h.user, h.account, domain.CreateAppInput{Name: "shop", Runtime: "ruby"})
	require.ErrorAs(t, err, &verr)
	stored, err := h.store.GetApp(ctx, "shop")
	require.NoError(t, err)
	assert.Equal(t, "node", stored.Runtime)
}

package domain_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/massdriver-cloud/rocketsled/internal/domain"
)

func TestDeployTokensAreShownOnceAndRevocable(t *testing.T) {
	h := newTenantHarness(t, nil)
	ctx := context.Background()

	created, err := h.svc.CreateToken(ctx, h.user, h.account, " github actions ")
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(created.Secret, domain.TokenPrefix))
	assert.Equal(t, "github actions", created.Name)

	account, user, err := h.svc.Authenticate(ctx, created.Secret)
	require.NoError(t, err)
	assert.Equal(t, h.account.ID, account.ID)
	assert.Equal(t, "token:"+created.ID, user.ID)
	assert.Equal(t, "github actions", user.Login)

	tokens, err := h.svc.ListTokens(ctx, h.account)
	require.NoError(t, err)
	require.Len(t, tokens, 1)
	require.NotNil(t, tokens[0].LastUsedAt, "a use is recorded")

	_, _, err = h.svc.Authenticate(ctx, created.Secret+"x")
	require.ErrorIs(t, err, domain.ErrForbidden)
	_, _, err = h.svc.Authenticate(ctx, "md_not_ours")
	require.ErrorIs(t, err, domain.ErrForbidden)

	require.NoError(t, h.svc.RevokeToken(ctx, h.account, created.ID))
	_, _, err = h.svc.Authenticate(ctx, created.Secret)
	require.ErrorIs(t, err, domain.ErrForbidden, "a revoked token stops working")
	require.ErrorIs(t, h.svc.RevokeToken(ctx, h.account, created.ID), domain.ErrNotFound)

	var verr *domain.ValidationError
	_, err = h.svc.CreateToken(ctx, h.user, h.account, "  ")
	require.ErrorAs(t, err, &verr)
}

func TestReleaseSetsTheImageOverPinnedParamsAndDeploysOnlyTheApp(t *testing.T) {
	h := newTenantHarness(t, &domain.PaaS{Runtimes: []domain.Runtime{{
		ID: "ruby", Name: "Ruby", Project: "ruby", Template: "template", AppComponent: "web", ImageParam: "image_uri",
		Pinned: map[string]map[string]any{"web": {"image_uri": "welcome:latest"}},
	}}})
	ctx := context.Background()
	app := h.app(t, "hello", "db")
	before := len(h.platform.DeployInstanceCalls())

	r1, err := h.svc.Release(ctx, h.user, h.account, "hello", domain.ReleaseInput{Image: "registry/hello:abc123", Description: "abc123"})
	require.NoError(t, err, "a build may name the environment by its slug")
	assert.Equal(t, 1, r1.Number)
	assert.Equal(t, "ada", r1.RequestedBy)

	calls := h.platform.DeployInstanceCalls()[before:]
	require.Len(t, calls, 1, "only the app component deploys")
	assert.Equal(t, "ruby-hello-web", calls[0].InstanceID)
	assert.Equal(t, "registry/hello:abc123", calls[0].Params["image_uri"], "the release wins over the pinned welcome image")

	r2, err := h.svc.Release(ctx, h.user, h.account, app.ID, domain.ReleaseInput{Image: "registry/hello:def456"})
	require.NoError(t, err)
	assert.Equal(t, 2, r2.Number)

	runs, err := h.store.ListRuns(ctx, app.ID, 1)
	require.NoError(t, err)
	assert.Equal(t, domain.TriggerRelease, runs[0].Trigger)
	assert.Equal(t, "Release #2", runs[0].Message)

	releases, err := h.svc.ListReleases(ctx, h.account, app.ID)
	require.NoError(t, err)
	require.Len(t, releases, 2)
	assert.Equal(t, "registry/hello:def456", releases[0].Image, "newest first")

	_, err = h.svc.DeployEnvironment(ctx, h.user, h.account, app.ID)
	require.NoError(t, err)
	last := h.platform.DeployInstanceCalls()
	for _, c := range last[len(last)-2:] {
		if c.InstanceID == "ruby-hello-web" {
			assert.Equal(t, "registry/hello:def456", c.Params["image_uri"], "a redeploy keeps the released image")
		}
	}

	_, err = h.svc.AddAddon(ctx, h.user, h.account, app.ID, domain.AddonInput{Component: "cache"})
	require.NoError(t, err)
	last = h.platform.DeployInstanceCalls()
	assert.Equal(t, "ruby-hello-web", last[len(last)-1].InstanceID)
	assert.Equal(t, "registry/hello:def456", last[len(last)-1].Params["image_uri"], "so does a consumer redeploy")
}

func TestReleaseRejects(t *testing.T) {
	h := newTenantHarness(t, &domain.PaaS{Runtimes: []domain.Runtime{
		{ID: "ruby", Name: "Ruby", Project: "ruby", Template: "template", AppComponent: "web", ImageParam: "image_uri"},
	}})
	ctx := context.Background()
	h.app(t, "hello")
	var verr *domain.ValidationError

	_, err := h.svc.Release(ctx, h.user, h.account, "hello", domain.ReleaseInput{Image: "Registry/Hello"})
	require.ErrorAs(t, err, &verr)
	assert.Equal(t, "image", verr.Field)
	assert.Equal(t, "use a lowercase image reference", verr.Message, "the bundle's own pattern message")

	_, err = h.svc.Release(ctx, h.user, h.account, "hello", domain.ReleaseInput{Image: " "})
	require.ErrorAs(t, err, &verr)

	_, err = h.svc.Release(ctx, h.user, h.account, "nope", domain.ReleaseInput{Image: "x"})
	require.ErrorIs(t, err, domain.ErrNotFound)

	static := newTenantHarness(t, &domain.PaaS{Runtimes: []domain.Runtime{{ID: "ruby", Name: "Ruby", Project: "ruby", Template: "template"}}})
	static.app(t, "site")
	_, err = static.svc.Release(ctx, static.user, static.account, "site", domain.ReleaseInput{Image: "x"})
	require.ErrorIs(t, err, domain.ErrConflict, "a runtime without an app component takes no releases")
}

func TestReleaseInInstallMode(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, domain.AccountModeShared)
	a := h.ready(t)
	_, err := h.svc.CreateEnvironment(ctx, h.user, a, domain.CreateEnvironmentInput{Slug: "dev"})
	require.NoError(t, err)
	_, err = h.svc.Release(ctx, h.user, a, "dev", domain.ReleaseInput{Image: "x"})
	require.ErrorIs(t, err, domain.ErrConflict, "the catalog names no app component")

	cat := awsCatalog()
	cat.Bootstrap.AppComponent, cat.Bootstrap.ImageParam = "app", "image_uri"
	h = newHarness(t, domain.AccountModeShared, cat)
	a = h.ready(t)
	env, err := h.svc.CreateEnvironment(ctx, h.user, a, domain.CreateEnvironmentInput{Slug: "dev"})
	require.NoError(t, err)
	release, err := h.svc.Release(ctx, h.user, a, "dev", domain.ReleaseInput{Image: "123.dkr.ecr.us-east-1.amazonaws.com/app:v1"})
	require.NoError(t, err)
	assert.Equal(t, env.ID, release.EnvironmentID)
	calls := h.platform.DeployInstanceCalls()
	require.Len(t, calls, 1, "only the app instance deploys")
	assert.Equal(t, "serverless-dev-app", calls[0].InstanceID)
	assert.Equal(t, "123.dkr.ecr.us-east-1.amazonaws.com/app:v1", calls[0].Params["image_uri"])

	view, err := h.svc.EnvironmentStatus(ctx, a, env.ID)
	require.NoError(t, err)
	assert.Equal(t, &domain.ReleaseTarget{Component: "app", Param: "image_uri"}, view.Release)
}

func TestAdoptedAppsKeepTheirReleasedImage(t *testing.T) {
	h := newTenantHarness(t, &domain.PaaS{Runtimes: []domain.Runtime{{
		ID: "ruby", Name: "Ruby", Project: "ruby", Template: "template", AppComponent: "web", ImageParam: "image_uri",
		Pinned: map[string]map[string]any{"web": {"image_uri": "welcome:latest"}},
	}}})
	ctx := context.Background()
	released := h.app(t, "hello")
	_, err := h.svc.Release(ctx, h.user, h.account, released.ID, domain.ReleaseInput{Image: "registry/hello:abc"})
	require.NoError(t, err)
	fresh := h.app(t, "fresh")

	require.NoError(t, h.store.DeleteEnvironment(ctx, released.ID))
	require.NoError(t, h.store.DeleteEnvironment(ctx, fresh.ID))
	envs, err := h.svc.ListEnvironments(ctx, h.account)
	require.NoError(t, err)
	images := map[string]string{}
	for _, e := range envs {
		images[e.Slug] = e.Image
	}
	assert.Equal(t, map[string]string{"hello": "registry/hello:abc", "fresh": ""}, images, "the pinned welcome image is not a release")
}

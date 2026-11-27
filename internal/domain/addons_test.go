package domain_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/massdriver-cloud/rocketsled/internal/domain"
)

func deployedIDs(h *tenantHarness) []string {
	calls := h.platform.DeployInstanceCalls()
	ids := make([]string, 0, len(calls))
	for _, c := range calls {
		ids = append(ids, c.InstanceID)
	}
	return ids
}

func TestAddAddonDeploysItThenItsConsumers(t *testing.T) {
	h := newTenantHarness(t, &domain.PaaS{Pinned: map[string]map[string]any{"web": {"subdomain": "{{environment_id}}"}}})
	app := h.app(t, "hello")
	require.Equal(t, []string{"ruby-hello-web"}, deployedIDs(h))

	run, err := h.svc.AddAddon(context.Background(), h.user, h.account, app.ID, domain.AddonInput{Component: "db", Preset: "Production"})
	require.NoError(t, err)
	assert.Equal(t, 2, run.Number)
	assert.Equal(t, domain.TriggerAddon, run.Trigger)
	assert.Equal(t, "Added Postgres", run.Message)

	assert.Equal(t, []string{"ruby-hello-web", "ruby-hello-db", "ruby-hello-web"}, deployedIDs(h), "the add-on first, then web picks up the connection")
	calls := h.platform.DeployInstanceCalls()
	assert.Equal(t, map[string]any{"size": "production"}, calls[1].Params)
	assert.Equal(t, "ruby-hello", calls[2].Params["subdomain"], "pinned params apply over web's current ones")
	assert.Equal(t, "development", calls[2].Params["size"])

	stored, err := h.store.GetEnvironment(context.Background(), app.ID)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"web": "Development", "db": "Production"}, stored.Presets)
	assert.Equal(t, []string{"db"}, stored.Addons)
	assert.Contains(t, h.world.descriptions["ruby-hello"], "[deploy db=Production,web=Development] [owner password:ada]")
}

func TestAddAddonRejects(t *testing.T) {
	h := newTenantHarness(t, &domain.PaaS{Limits: domain.Limits{Addons: []string{"cache"}}})
	app := h.app(t, "hello", "cache")
	var verr *domain.ValidationError

	_, err := h.svc.AddAddon(context.Background(), h.user, h.account, app.ID, domain.AddonInput{Component: "db"})
	require.ErrorAs(t, err, &verr, "limits keep db off the menu")
	assert.Equal(t, "component", verr.Field)

	_, err = h.svc.AddAddon(context.Background(), h.user, h.account, app.ID, domain.AddonInput{Component: "web"})
	require.ErrorAs(t, err, &verr, "web is not an add-on")

	_, err = h.svc.AddAddon(context.Background(), h.user, h.account, app.ID, domain.AddonInput{Component: "cache"})
	require.ErrorIs(t, err, domain.ErrConflict)

	h2 := newTenantHarness(t, nil)
	app2 := h2.app(t, "other")
	_, err = h2.svc.AddAddon(context.Background(), h2.user, h2.account, app2.ID, domain.AddonInput{Component: "db", Preset: "Huge"})
	require.ErrorAs(t, err, &verr)
	assert.Equal(t, "preset", verr.Field)

	_, err = h2.svc.DestroyEnvironment(context.Background(), h2.account, app2.ID)
	require.NoError(t, err)
	_, err = h2.svc.AddAddon(context.Background(), h2.user, h2.account, app2.ID, domain.AddonInput{Component: "db"})
	require.ErrorIs(t, err, domain.ErrConflict, "nothing is added to an app being deleted")
}

func TestRemoveAddonDecommissionsItThenRedeploysConsumers(t *testing.T) {
	h := newTenantHarness(t, nil)
	app := h.app(t, "hello", "db", "cache")
	before := len(h.platform.DeployInstanceCalls())

	run, err := h.svc.RemoveAddon(context.Background(), h.user, h.account, app.ID, "db")
	require.NoError(t, err)
	assert.Equal(t, "Removed Postgres", run.Message)

	require.Len(t, h.platform.DecommissionInstanceCalls(), 1)
	gone := h.platform.DecommissionInstanceCalls()[0]
	assert.Equal(t, "ruby-hello-db", gone.InstanceID)
	assert.Equal(t, map[string]any{"size": "development"}, gone.Params, "decommission sends the instance's current params")
	assert.Equal(t, []string{"ruby-hello-web"}, deployedIDs(h)[before:], "only the consumer is redeployed")

	stored, err := h.store.GetEnvironment(context.Background(), app.ID)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"web": "Development", "cache": "Development"}, stored.Presets)
	assert.Equal(t, []string{"cache"}, stored.Addons)
	assert.Contains(t, h.world.descriptions["ruby-hello"], "[deploy cache=Development,web=Development]")

	runs, err := h.store.ListRuns(context.Background(), app.ID, 1)
	require.NoError(t, err)
	assert.Equal(t, domain.RunStatusRunning, runs[0].Status)

	_, err = h.svc.RemoveAddon(context.Background(), h.user, h.account, app.ID, "db")
	require.ErrorIs(t, err, domain.ErrNotFound)
}

func TestRemoveAddonThatNeverDeployedSkipsTheDecommission(t *testing.T) {
	h := newTenantHarness(t, nil)
	app := h.app(t, "hello", "cache")
	h.world.set("ruby-hello-cache", "INITIALIZED", nil)

	_, err := h.svc.RemoveAddon(context.Background(), h.user, h.account, app.ID, "cache")
	require.NoError(t, err)
	assert.Empty(t, h.platform.DecommissionInstanceCalls())
}

func TestAddonsAreTenantOnly(t *testing.T) {
	h := newHarness(t, domain.AccountModeShared)
	a := h.ready(t)
	_, err := h.svc.AddAddon(context.Background(), h.user, a, "x", domain.AddonInput{Component: "db"})
	require.ErrorIs(t, err, domain.ErrNotFound)
}

func TestAdoptedAppsReadTheirAddonsFromTheDescription(t *testing.T) {
	h := newTenantHarness(t, nil)
	app := h.app(t, "hello", "db", "cache")
	_, err := h.svc.RemoveAddon(context.Background(), h.user, h.account, app.ID, "db")
	require.NoError(t, err)

	require.NoError(t, h.store.DeleteEnvironment(context.Background(), app.ID))
	envs, err := h.svc.ListEnvironments(context.Background(), h.account)
	require.NoError(t, err)
	require.Len(t, envs, 1)
	assert.Equal(t, []string{"cache"}, envs[0].Addons, "a decommissioned add-on is not adopted back")
	assert.Equal(t, map[string]string{"web": "Development", "cache": "Development"}, envs[0].Presets)
}

func TestOneChangeAtATime(t *testing.T) {
	h := newTenantHarness(t, nil)
	ctx := context.Background()
	app := h.app(t, "hello", "cache")
	deploy := h.platform.DeployInstanceFunc
	var nested error
	h.platform.DeployInstanceFunc = func(ctx context.Context, id string, params map[string]any, msg string) error {
		if id == "ruby-hello-db" {
			_, nested = h.svc.RemoveAddon(ctx, h.user, h.account, app.ID, "cache")
		}
		return deploy(ctx, id, params, msg)
	}
	_, err := h.svc.AddAddon(ctx, h.user, h.account, app.ID, domain.AddonInput{Component: "db"})
	require.NoError(t, err)
	require.ErrorIs(t, nested, domain.ErrConflict, "a remove while the add is still deploying is refused")

	_, err = h.svc.RemoveAddon(ctx, h.user, h.account, app.ID, "cache")
	require.NoError(t, err, "once the add finished, the next change goes through")
}

func TestRedeployAfterAFailedAddUsesThePlanPicked(t *testing.T) {
	h := newTenantHarness(t, nil)
	ctx := context.Background()
	app := h.app(t, "hello")
	deploy := h.platform.DeployInstanceFunc
	h.platform.DeployInstanceFunc = func(ctx context.Context, id string, params map[string]any, msg string) error {
		if id == "ruby-hello-db" {
			return errors.New("throttled")
		}
		return deploy(ctx, id, params, msg)
	}
	_, err := h.svc.AddAddon(ctx, h.user, h.account, app.ID, domain.AddonInput{Component: "db", Preset: "Production"})
	require.NoError(t, err)

	h.platform.DeployInstanceFunc = deploy
	_, err = h.svc.DeployEnvironment(ctx, h.user, h.account, app.ID)
	require.NoError(t, err)
	var db map[string]any
	for _, c := range h.platform.DeployInstanceCalls() {
		if c.InstanceID == "ruby-hello-db" {
			db = c.Params
		}
	}
	assert.Equal(t, map[string]any{"size": "production"}, db, "the never-deployed add-on gets its plan, not empty params")
}

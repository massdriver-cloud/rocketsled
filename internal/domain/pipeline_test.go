package domain_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/massdriver-cloud/rocketsled/internal/domain"
)

func pipelinePaaS() *domain.PaaS {
	return &domain.PaaS{
		Pinned: map[string]map[string]any{"web": {"subdomain": "{{environment_id}}"}},
		Pipeline: domain.Pipeline{
			Stages: []domain.Stage{
				{ID: "staging", Name: "Staging", Presets: map[string]string{"web": "Development", "worker": "Development"}},
				{ID: "production", Name: "Production", Presets: map[string]string{"web": "Production", "db": "Production"}},
			},
			Promote: []string{"web", "db", "worker"},
		},
	}
}

func (h *tenantHarness) stagedApp(t *testing.T, slug, stage string, addons ...string) *domain.Environment {
	t.Helper()
	env, err := h.svc.CreateEnvironment(context.Background(), h.user, h.account, domain.CreateEnvironmentInput{Slug: slug, Runtime: "ruby", Stage: stage, Addons: addons})
	require.NoError(t, err)
	return env
}

func (h *tenantHarness) copyMergesIntoTheWorld() {
	h.platform.CopyInstanceFunc = func(_ context.Context, sourceID, destinationID string, overrides map[string]any) error {
		src := h.world.environment(sourceID[:strings.LastIndex(sourceID, "-")])
		dst := h.world.environment(destinationID[:strings.LastIndex(destinationID, "-")])
		var from map[string]any
		for _, pi := range src.Instances {
			if pi.ID == sourceID {
				from = pi.Params
			}
		}
		for _, pi := range dst.Instances {
			if pi.ID == destinationID {
				h.world.set(destinationID, pi.Status, domain.MergeParams(from, overrides))
			}
		}
		return nil
	}
}

func TestOfferReducesThePipelineToEachRuntime(t *testing.T) {
	h := newTenantHarness(t, pipelinePaaS())
	offer, err := h.svc.Offer(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []domain.StageSummary{{ID: "staging", Name: "Staging"}, {ID: "production", Name: "Production"}}, offer.Stages)

	ro := offer.Runtimes[0]
	require.Len(t, ro.Stages, 2)
	assert.Equal(t, map[string]string{"web": "Development"}, ro.Stages[0].Presets, "worker is not in the ruby template, so it is skipped")
	assert.Empty(t, ro.Stages[0].Addons)
	assert.Equal(t, map[string]string{"web": "Production", "db": "Production"}, ro.Stages[1].Presets)
	assert.Equal(t, []string{"db"}, ro.Stages[1].Addons, "an add-on a stage names starts switched on")
	assert.Equal(t, []string{"web", "db"}, ro.Promote)
}

func TestOfferDropsStagePicksTheLimitsDontOffer(t *testing.T) {
	paas := pipelinePaaS()
	paas.Limits = domain.Limits{Presets: []string{"Development"}, Addons: []string{"cache"}}
	h := newTenantHarness(t, paas)
	offer, err := h.svc.Offer(context.Background())
	require.NoError(t, err)
	assert.Empty(t, offer.Runtimes[0].Stages[1].Presets, "Production exists on the bundle but isn't offered")
	assert.Empty(t, offer.Runtimes[0].Stages[1].Addons, "db isn't offered")
}

func TestOfferRejectsAStagePresetTheBundleLacks(t *testing.T) {
	paas := pipelinePaaS()
	paas.Pipeline.Stages[1].Presets["web"] = "Huge"
	h := newTenantHarness(t, paas)
	_, err := h.svc.Offer(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), `stage "production" picks "Huge" for web`)
}

func TestStageIsStoredOnTheAppAndItsDescription(t *testing.T) {
	h := newTenantHarness(t, pipelinePaaS())
	app := h.stagedApp(t, "prd", "production", "db")
	assert.Equal(t, "production", app.Stage)
	assert.Equal(t, map[string]string{"web": "Production", "db": "Production"}, app.Presets, "the stage's presets fill what the customer left out")
	assert.Contains(t, h.world.descriptions["ruby-prd"], "[stage production]")

	require.NoError(t, h.store.DeleteEnvironment(context.Background(), app.ID))
	envs, err := h.svc.ListEnvironments(context.Background(), h.account)
	require.NoError(t, err)
	require.Len(t, envs, 1)
	assert.Equal(t, "production", envs[0].Stage, "an adopted app keeps its stage")

	_, err = h.svc.CreateEnvironment(context.Background(), h.user, h.account, domain.CreateEnvironmentInput{Slug: "x", Runtime: "ruby", Stage: "qa"})
	var verr *domain.ValidationError
	require.ErrorAs(t, err, &verr)
	assert.Equal(t, "stage", verr.Field)
}

func TestPromoteCopiesConfigButKeepsTheTargetsPresetAndPinnedParams(t *testing.T) {
	h := newTenantHarness(t, pipelinePaaS())
	h.copyMergesIntoTheWorld()
	stg := h.stagedApp(t, "stg", "staging", "db")
	prd := h.stagedApp(t, "prd", "production", "db")
	h.world.set("ruby-stg-web", "PROVISIONED", map[string]any{"size": "development", "subdomain": "ruby-stg", "image_uri": "app:v2"})
	before := len(h.platform.DeployInstanceCalls())

	run, err := h.svc.Promote(context.Background(), h.user, h.account, stg.ID, domain.PromoteInput{To: prd.ID})
	require.NoError(t, err)
	assert.Equal(t, domain.TriggerPromote, run.Trigger)
	assert.Equal(t, "Promoted from stg", run.Message)

	copies := h.platform.CopyInstanceCalls()
	require.Len(t, copies, 2)
	assert.Equal(t, "ruby-stg-web", copies[0].SourceID)
	assert.Equal(t, "ruby-prd-web", copies[0].DestinationID)
	assert.Equal(t, map[string]any{"size": "production", "subdomain": "ruby-prd"}, copies[0].Overrides)
	assert.Equal(t, "ruby-prd-db", copies[1].DestinationID)

	deploys := h.platform.DeployInstanceCalls()[before:]
	require.Len(t, deploys, 1, "db's params came out the same, so only web deploys")
	assert.Equal(t, "ruby-prd-web", deploys[0].InstanceID)
	assert.Equal(t, map[string]any{"size": "production", "subdomain": "ruby-prd", "image_uri": "app:v2"}, deploys[0].Params)

	_, err = h.svc.Promote(context.Background(), h.user, h.account, stg.ID, domain.PromoteInput{To: prd.ID})
	require.NoError(t, err)
	assert.Len(t, h.platform.DeployInstanceCalls(), before+1, "promoting again changes nothing, so nothing deploys")
}

func TestPromoteMovesTheBundleVersion(t *testing.T) {
	h := newTenantHarness(t, pipelinePaaS())
	h.copyMergesIntoTheWorld()
	stg := h.stagedApp(t, "stg", "staging")
	prd := h.stagedApp(t, "prd", "production")
	h.world.version["ruby-stg-web"], h.world.deployed["ruby-stg-web"] = "1.4.0", "1.3.0"
	h.world.version["ruby-prd-web"], h.world.deployed["ruby-prd-web"] = "1.2.0", "1.2.0"
	h.world.set("ruby-prd-web", "PROVISIONED", nil)
	before := len(h.platform.DeployInstanceCalls())

	_, err := h.svc.Promote(context.Background(), h.user, h.account, stg.ID, domain.PromoteInput{To: prd.ID})
	require.NoError(t, err)
	pins := h.platform.SetInstanceVersionCalls()
	require.Len(t, pins, 1)
	assert.Equal(t, "ruby-prd-web", pins[0].InstanceID)
	assert.Equal(t, "1.3.0", pins[0].Version, "the version staging deployed, not what its channel resolves to now")
	assert.Equal(t, []string{"ruby-prd-web"}, deployedIDs(h)[before:], "a new version deploys even when the params match")
	assert.Equal(t, "1.3.0", h.world.deployed["ruby-prd-web"])
}

func TestPromoteOnlyCopiesComponentsBothAppsDeploy(t *testing.T) {
	h := newTenantHarness(t, pipelinePaaS())
	h.copyMergesIntoTheWorld()
	stg := h.stagedApp(t, "stg", "staging")
	prd := h.stagedApp(t, "prd", "production", "db")

	_, err := h.svc.Promote(context.Background(), h.user, h.account, stg.ID, domain.PromoteInput{To: prd.ID})
	require.NoError(t, err)
	require.Len(t, h.platform.CopyInstanceCalls(), 1)
	assert.Equal(t, "ruby-prd-web", h.platform.CopyInstanceCalls()[0].DestinationID, "staging has no db, so production's is left alone")
}

func TestPromoteRejects(t *testing.T) {
	h := newTenantHarness(t, pipelinePaaS())
	stg := h.stagedApp(t, "stg", "staging")
	prd := h.stagedApp(t, "prd", "production")
	loose := h.app(t, "loose")
	ctx := context.Background()
	other := &domain.Environment{ID: "node-api", AccountID: h.account.ID, Slug: "api", Name: "api", EnvironmentID: "node-api", Runtime: "node", Stage: "production", Presets: map[string]string{"web": "Production"}, State: domain.EnvironmentStateRunning}
	require.NoError(t, h.store.CreateEnvironment(ctx, other))

	cases := []struct {
		name     string
		from, to string
	}{
		{"to an earlier stage", prd.ID, stg.ID},
		{"to the same stage", stg.ID, stg.ID},
		{"to an app that doesn't exist", stg.ID, "ruby-nope"},
		{"to another runtime", stg.ID, other.ID},
		{"from an app with no stage", loose.ID, prd.ID},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := h.svc.Promote(ctx, h.user, h.account, tc.from, domain.PromoteInput{To: tc.to})
			var verr *domain.ValidationError
			require.ErrorAs(t, err, &verr)
			assert.Equal(t, "to", verr.Field)
		})
	}

	_, err := h.svc.DestroyEnvironment(ctx, h.account, prd.ID)
	require.NoError(t, err)
	_, err = h.svc.Promote(ctx, h.user, h.account, stg.ID, domain.PromoteInput{To: prd.ID})
	require.ErrorIs(t, err, domain.ErrConflict, "nothing is promoted into an app being deleted")
	assert.Empty(t, h.platform.CopyInstanceCalls())
}

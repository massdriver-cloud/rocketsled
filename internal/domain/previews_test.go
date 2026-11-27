package domain_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/massdriver-cloud/rocketsled/internal/domain"
)

func previewPaaS() *domain.PaaS {
	return &domain.PaaS{
		Runtimes: []domain.Runtime{{
			ID: "ruby", Name: "Ruby", Project: "ruby", Template: "template", AppComponent: "web", ImageParam: "image_uri",
			Pinned: map[string]map[string]any{"web": {"image_uri": "welcome:latest"}},
		}},
		Previews: &domain.Previews{Presets: map[string]string{"web": "Development", "db": "Development", "worker": "Development"}, TTL: 72 * time.Hour},
	}
}

func TestPreviewIsMadeOnceThenReleasedTo(t *testing.T) {
	h := newTenantHarness(t, previewPaaS())
	ctx := context.Background()

	first, err := h.svc.Preview(ctx, h.user, h.account, "PR42", domain.PreviewInput{Image: "registry/app:abc", Description: "feature/login"})
	require.NoError(t, err)
	assert.Equal(t, 1, first.Number)
	assert.Equal(t, "ruby-pr42", first.EnvironmentID)
	assert.NotEmpty(t, first.RunID)

	env, err := h.store.GetEnvironment(ctx, "ruby-pr42")
	require.NoError(t, err)
	assert.True(t, env.Preview)
	assert.Equal(t, []string{"db"}, env.Addons, "add-ons the preview presets name are deployed, others are not")
	assert.Equal(t, map[string]string{"web": "Development", "db": "Development"}, env.Presets, "presets for components the runtime lacks are ignored")
	assert.Contains(t, h.world.descriptions["ruby-pr42"], "[preview] [owner password:ada]")

	var web map[string]any
	for _, c := range h.platform.DeployInstanceCalls() {
		if c.InstanceID == "ruby-pr42-web" {
			web = c.Params
		}
	}
	assert.Equal(t, "registry/app:abc", web["image_uri"], "the first deploy already runs the image, not the pinned welcome page")

	forks := len(h.platform.ForkEnvironmentCalls())
	second, err := h.svc.Preview(ctx, h.user, h.account, "pr42", domain.PreviewInput{Image: "registry/app:def"})
	require.NoError(t, err)
	assert.Equal(t, 2, second.Number)
	assert.Len(t, h.platform.ForkEnvironmentCalls(), forks, "the next push only releases")
	last := h.platform.DeployInstanceCalls()
	assert.Equal(t, "registry/app:def", last[len(last)-1].Params["image_uri"])
}

func TestPreviewRejects(t *testing.T) {
	ctx := context.Background()
	var verr *domain.ValidationError

	off := newTenantHarness(t, nil)
	_, err := off.svc.Preview(ctx, off.user, off.account, "pr1", domain.PreviewInput{Image: "x"})
	require.ErrorIs(t, err, domain.ErrNotFound, "previews need paas.previews")

	h := newTenantHarness(t, previewPaaS())
	_, err = h.svc.Preview(ctx, h.user, h.account, "feature/login", domain.PreviewInput{Image: "x"})
	require.ErrorAs(t, err, &verr)
	assert.Equal(t, "name", verr.Field)

	_, err = h.svc.Preview(ctx, h.user, h.account, "pr1", domain.PreviewInput{Image: "Bad Image"})
	require.ErrorAs(t, err, &verr)
	assert.Equal(t, "image", verr.Field)
	assert.Empty(t, h.platform.ForkEnvironmentCalls(), "nothing is forked for a bad image")

	h.app(t, "hello")
	_, err = h.svc.Preview(ctx, h.user, h.account, "hello", domain.PreviewInput{Image: "x"})
	require.ErrorIs(t, err, domain.ErrConflict, "a preview never releases to an app that isn't one")
	_, err = h.svc.DeletePreview(ctx, h.account, "hello")
	require.ErrorIs(t, err, domain.ErrConflict, "and never deletes one")

	two := previewPaaS()
	two.Runtimes = append(two.Runtimes, domain.Runtime{ID: "node", Name: "Node", Project: "ruby", Template: "template"})
	h2 := newTenantHarness(t, two)
	_, err = h2.svc.Preview(ctx, h2.user, h2.account, "pr1", domain.PreviewInput{Image: "x"})
	require.ErrorAs(t, err, &verr)
	assert.Equal(t, "runtime", verr.Field, "with two runtimes the build names one")
}

func TestDeletePreviewDecommissionsIt(t *testing.T) {
	h := newTenantHarness(t, previewPaaS())
	ctx := context.Background()
	_, err := h.svc.Preview(ctx, h.user, h.account, "pr7", domain.PreviewInput{Image: "x"})
	require.NoError(t, err)

	env, err := h.svc.DeletePreview(ctx, h.account, "pr7")
	require.NoError(t, err)
	assert.Equal(t, domain.EnvironmentStateDecommissioning, env.State)
	require.Len(t, h.platform.DecommissionEnvironmentCalls(), 1)
	assert.Equal(t, "ruby-pr7", h.platform.DecommissionEnvironmentCalls()[0].EnvironmentID)
}

func TestReapPreviewsRemovesOnlyQuietPreviews(t *testing.T) {
	h := newTenantHarness(t, previewPaaS())
	ctx := context.Background()
	_, err := h.svc.Preview(ctx, h.user, h.account, "quiet", domain.PreviewInput{Image: "x"})
	require.NoError(t, err)
	_, err = h.svc.Preview(ctx, h.user, h.account, "busy", domain.PreviewInput{Image: "x"})
	require.NoError(t, err)
	h.app(t, "production")

	h.now = h.now.Add(48 * time.Hour)
	_, err = h.svc.Preview(ctx, h.user, h.account, "busy", domain.PreviewInput{Image: "y"})
	require.NoError(t, err)

	h.now = h.now.Add(25 * time.Hour)
	h.svc.ReapPreviews(ctx)
	calls := h.platform.DecommissionEnvironmentCalls()
	require.Len(t, calls, 1, "only the preview 73 hours past its last release goes; the app made with it is not a preview")
	assert.Equal(t, "ruby-quiet", calls[0].EnvironmentID)
	quiet, err := h.store.GetEnvironment(ctx, "ruby-quiet")
	require.NoError(t, err)
	assert.Equal(t, domain.EnvironmentStateDecommissioning, quiet.State)

	h.svc.ReapPreviews(ctx)
	assert.Len(t, h.platform.DecommissionEnvironmentCalls(), 1, "a preview is not decommissioned twice within the hour")
}

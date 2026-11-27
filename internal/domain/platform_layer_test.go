package domain_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/massdriver-cloud/rocketsled/internal/domain"
)

type platformWorld struct {
	mu     sync.Mutex
	status map[string]string
	order  []string
}

func withPlatform(t *testing.T, layer *domain.PlatformLayer, status map[string]string) (*tenantHarness, *platformWorld) {
	t.Helper()
	pw := &platformWorld{status: status}
	paas := &domain.PaaS{
		Runtimes: []domain.Runtime{
			{ID: "ruby", Name: "Ruby", Project: "ruby", Template: "template"},
			{ID: "node", Name: "Node", Project: "node", Template: "template"},
		},
		Platform: layer,
	}
	h := newTenantHarness(t, paas)
	tenantEnv, tenantDeploy := h.platform.GetEnvironmentFunc, h.platform.DeployInstanceFunc
	h.platform.GetEnvironmentFunc = func(ctx context.Context, id string) (*domain.PlatformEnvironment, error) {
		if id == "node-template" {
			return tenantEnv(ctx, "ruby-template")
		}
		if id != "platform-main" {
			return tenantEnv(ctx, id)
		}
		pw.mu.Lock()
		defer pw.mu.Unlock()
		env := &domain.PlatformEnvironment{ID: id, ProjectID: "platform"}
		for _, slug := range []string{"network", "dns", "cluster"} {
			env.Instances = append(env.Instances, domain.PlatformInstance{
				ID: "platform-main-" + slug, ComponentSlug: slug, ComponentName: slug, Status: pw.status[slug],
				ParamsSchema: presetSchema("Development", "Production"),
			})
		}
		return env, nil
	}
	h.platform.DeployInstanceFunc = func(ctx context.Context, id string, params map[string]any, msg string) error {
		if slug, ok := strings.CutPrefix(id, "platform-main-"); ok {
			pw.mu.Lock()
			defer pw.mu.Unlock()
			pw.status[slug] = "PROVISIONED"
			pw.order = append(pw.order, slug)
			return nil
		}
		return tenantDeploy(ctx, id, params, msg)
	}
	return h, pw
}

func fauxrokuPlatform() *domain.PlatformLayer {
	return &domain.PlatformLayer{
		Project: "platform", Environment: "main", Domain: "fauxroku.dev",
		Components: []domain.PlatformComponent{
			{Slug: "network", Preset: "Development"},
			{Slug: "dns", Preset: "Production", Params: map[string]any{"domain": "{{platform_domain}}"}},
			{Slug: "cluster", Preset: "Development"},
		},
		Shares: []domain.Share{{Component: "network", Field: "network"}, {Component: "cluster", Field: "cluster"}},
	}
}

func TestEnsurePlatformDeploysInOrderThenSharesAndPins(t *testing.T) {
	h, pw := withPlatform(t, fauxrokuPlatform(), map[string]string{"network": "PROVISIONED", "dns": "INITIALIZED", "cluster": "INITIALIZED"})
	ctx := context.Background()

	require.NoError(t, h.svc.EnsurePlatform(ctx))
	assert.Equal(t, []string{"dns", "cluster"}, pw.order, "a provisioned component is not deployed again")
	var dns map[string]any
	for _, c := range h.platform.DeployInstanceCalls() {
		if c.InstanceID == "platform-main-dns" {
			dns = c.Params
		}
	}
	assert.Equal(t, map[string]any{"size": "production", "domain": "fauxroku.dev"}, dns, "the preset, with params over it and the domain filled in")

	grants := map[string][]string{}
	for _, g := range h.platform.GrantResourceToProjectCalls() {
		grants[g.ResourceID] = append(grants[g.ResourceID], g.ProjectID)
	}
	assert.Equal(t, map[string][]string{
		"platform-main-network.network": {"ruby", "node"},
		"platform-main-cluster.cluster": {"ruby", "node"},
	}, grants)
	defaults := map[string][]string{}
	for _, d := range h.platform.SetEnvironmentDefaultCalls() {
		defaults[d.EnvironmentID] = append(defaults[d.EnvironmentID], d.ResourceID)
	}
	assert.Equal(t, map[string][]string{
		"ruby-template": {"platform-main-network.network", "platform-main-cluster.cluster"},
		"node-template": {"platform-main-network.network", "platform-main-cluster.cluster"},
	}, defaults)
	assert.Equal(t, &domain.PlatformStatus{State: domain.PlatformStateRunning}, h.svc.PlatformStatus())

	pinned := map[string][]string{}
	for _, d := range h.platform.SetEnvironmentDefaultCalls() {
		pinned[d.EnvironmentID] = append(pinned[d.EnvironmentID], d.ResourceID)
	}
	h.platform.EnvironmentDefaultsFunc = func(_ context.Context, id string) ([]string, error) { return pinned[id], nil }
	require.NoError(t, h.svc.EnsurePlatform(ctx))
	assert.Len(t, pw.order, 2, "converged: nothing deploys twice")
	assert.Len(t, h.platform.SetEnvironmentDefaultCalls(), 4, "nor is a default pinned twice")

	offer, err := h.svc.Offer(ctx)
	require.NoError(t, err)
	assert.Equal(t, domain.PlatformStateRunning, offer.Platform.State)
	h.app(t, "hello")
}

func TestAppsWaitForThePlatform(t *testing.T) {
	layer := &domain.PlatformLayer{Project: "platform", Environment: "main", Shares: []domain.Share{{Component: "cluster", Field: "cluster"}}}
	h, _ := withPlatform(t, layer, map[string]string{"cluster": "PROVISIONING"})
	ctx := context.Background()
	assert.Equal(t, domain.PlatformStateDeploying, h.svc.PlatformStatus().State, "before the first check")

	require.Error(t, h.svc.EnsurePlatform(ctx))
	assert.Equal(t, &domain.PlatformStatus{State: domain.PlatformStateDeploying}, h.svc.PlatformStatus(), "a platform deployed elsewhere is waited for")
	assert.Empty(t, h.platform.GrantResourceToProjectCalls())

	_, err := h.svc.CreateEnvironment(ctx, h.user, h.account, domain.CreateEnvironmentInput{Slug: "hello", Runtime: "ruby"})
	require.ErrorIs(t, err, domain.ErrConflict)
	assert.Empty(t, h.platform.ForkEnvironmentCalls())
}

func TestPlatformFailuresAreReported(t *testing.T) {
	h, _ := withPlatform(t, fauxrokuPlatform(), map[string]string{"network": "INITIALIZED"})
	h.platform.DeployInstanceFunc = func(context.Context, string, map[string]any, string) error { return errors.New("quota exceeded") }

	err := h.svc.EnsurePlatform(context.Background())
	require.Error(t, err)
	st := h.svc.PlatformStatus()
	assert.Equal(t, domain.PlatformStateFailed, st.State)
	assert.Contains(t, st.Error, "quota exceeded")
}

func TestNoPlatformLayer(t *testing.T) {
	h := newTenantHarness(t, nil)
	require.NoError(t, h.svc.EnsurePlatform(context.Background()))
	assert.Nil(t, h.svc.PlatformStatus())
	offer, err := h.svc.Offer(context.Background())
	require.NoError(t, err)
	assert.Nil(t, offer.Platform)
}

package domain_test

import (
	"context"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/massdriver-cloud/rocketsled/internal/domain"
)

func TestNormalizeHostnames(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want []string
		bad  bool
	}{
		{name: "lowercases and drops the trailing dot", in: []string{" WWW.Example.com. "}, want: []string{"www.example.com"}},
		{name: "several", in: []string{"a.example.com", "b.example.com"}, want: []string{"a.example.com", "b.example.com"}},
		{name: "none", in: nil, want: []string{}},
		{name: "one label", in: []string{"localhost"}, bad: true},
		{name: "wildcard", in: []string{"*.example.com"}, bad: true},
		{name: "underscore", in: []string{"my_app.example.com"}, bad: true},
		{name: "leading hyphen", in: []string{"-app.example.com"}, bad: true},
		{name: "empty label", in: []string{"app..example.com"}, bad: true},
		{name: "label over 63", in: []string{"a234567890123456789012345678901234567890123456789012345678901234.com"}, bad: true},
		{name: "empty", in: []string{" "}, bad: true},
		{name: "an IP address", in: []string{"10.0.0.1"}, bad: true},
		{name: "duplicate after normalizing", in: []string{"www.example.com", "WWW.example.com."}, bad: true},
		{name: "a URL", in: []string{"https://www.example.com"}, bad: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := domain.NormalizeHostnames(c.in)
			if c.bad {
				var verr *domain.ValidationError
				require.ErrorAs(t, err, &verr)
				assert.Equal(t, "domains", verr.Field)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, c.want, got)
		})
	}
}

func domainsHarness(t *testing.T, paramType string) *tenantHarness {
	t.Helper()
	h := newTenantHarness(t, &domain.PaaS{
		Pinned:  map[string]map[string]any{"web": {"subdomain": "{{environment_id}}"}},
		Domains: &domain.Domains{Component: "web", Param: "subdomain", TargetField: "url"},
	})
	get := h.platform.GetEnvironmentFunc
	h.platform.GetEnvironmentFunc = func(ctx context.Context, id string) (*domain.PlatformEnvironment, error) {
		env, err := get(ctx, id)
		if err != nil {
			return nil, err
		}
		for i := range env.Instances {
			pi := &env.Instances[i]
			if pi.ComponentSlug != "web" {
				continue
			}
			pi.ParamsSchema["properties"] = map[string]any{"subdomain": map[string]any{"type": paramType}}
			pi.Resources = []domain.InstanceResource{{Field: "website", ResourceType: "website", Payload: map[string]any{"url": "https://" + id + ".fauxroku.app/"}}}
		}
		return env, nil
	}
	return h
}

func lastWebDeploy(h *tenantHarness) map[string]any {
	for _, c := range slices.Backward(h.platform.DeployInstanceCalls()) {
		if c.InstanceID == "ruby-hello-web" {
			return c.Params
		}
	}
	return nil
}

func TestSetDomainsDeploysOnlyTheComponentAndOutlivesPinnedParams(t *testing.T) {
	h := domainsHarness(t, "string")
	app := h.app(t, "hello")
	assert.Equal(t, "ruby-hello", lastWebDeploy(h)["subdomain"])
	before := len(h.platform.DeployInstanceCalls())

	run, err := h.svc.SetDomains(context.Background(), h.user, h.account, app.ID, domain.DomainsInput{Domains: []string{"WWW.Example.com."}})
	require.NoError(t, err)
	assert.Equal(t, domain.TriggerDomains, run.Trigger)
	assert.Equal(t, "Domains: www.example.com", run.Message)
	assert.Equal(t, []string{"ruby-hello-web"}, deployedIDs(h)[before:], "only the component the domain lands on deploys")
	assert.Equal(t, "www.example.com", lastWebDeploy(h)["subdomain"])

	view, err := h.svc.AppDomains(context.Background(), h.account, app.ID)
	require.NoError(t, err)
	assert.Equal(t, []domain.DomainRecord{{Domain: "www.example.com", RecordType: "CNAME", Target: "ruby-hello.fauxroku.app", Status: domain.DomainStatusConfigured}}, view.Domains)
	assert.False(t, view.Multiple)

	_, err = h.svc.DeployEnvironment(context.Background(), h.user, h.account, app.ID)
	require.NoError(t, err)
	assert.Equal(t, "www.example.com", lastWebDeploy(h)["subdomain"], "a redeploy keeps the domain over the pinned subdomain")

	_, err = h.svc.AddAddon(context.Background(), h.user, h.account, app.ID, domain.AddonInput{Component: "db"})
	require.NoError(t, err)
	assert.Equal(t, "www.example.com", lastWebDeploy(h)["subdomain"], "so does redeploying web for a new add-on")

	_, err = h.svc.SetDomains(context.Background(), h.user, h.account, app.ID, domain.DomainsInput{})
	require.NoError(t, err)
	assert.Equal(t, "ruby-hello", lastWebDeploy(h)["subdomain"], "clearing the domains brings the pinned subdomain back")
	view, err = h.svc.AppDomains(context.Background(), h.account, app.ID)
	require.NoError(t, err)
	assert.Empty(t, view.Domains)
}

func TestDomainsArePendingUntilTheComponentHasDeployedWithThem(t *testing.T) {
	h := domainsHarness(t, "string")
	app := h.app(t, "hello")
	_, err := h.svc.SetDomains(context.Background(), h.user, h.account, app.ID, domain.DomainsInput{Domains: []string{"www.example.com"}})
	require.NoError(t, err)

	h.world.set("ruby-hello-web", "PROVISIONING", nil)
	view, err := h.svc.AppDomains(context.Background(), h.account, app.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.DomainStatusPending, view.Domains[0].Status)

	h.world.set("ruby-hello-web", "PROVISIONED", map[string]any{"subdomain": "old.example.com"})
	view, err = h.svc.AppDomains(context.Background(), h.account, app.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.DomainStatusPending, view.Domains[0].Status, "deployed, but not with this domain")
}

func TestDomainsTakeOneOrManyByTheParamsSchema(t *testing.T) {
	h := domainsHarness(t, "string")
	app := h.app(t, "hello")
	var verr *domain.ValidationError
	_, err := h.svc.SetDomains(context.Background(), h.user, h.account, app.ID, domain.DomainsInput{Domains: []string{"a.example.com", "b.example.com"}})
	require.ErrorAs(t, err, &verr)
	assert.Equal(t, "domains", verr.Field)
	_, err = h.svc.SetDomains(context.Background(), h.user, h.account, app.ID, domain.DomainsInput{Domains: []string{"nope"}})
	require.ErrorAs(t, err, &verr)

	h = domainsHarness(t, "array")
	app = h.app(t, "hello")
	_, err = h.svc.SetDomains(context.Background(), h.user, h.account, app.ID, domain.DomainsInput{Domains: []string{"a.example.com", "b.example.com"}})
	require.NoError(t, err)
	assert.Equal(t, []any{"a.example.com", "b.example.com"}, lastWebDeploy(h)["subdomain"])
	view, err := h.svc.AppDomains(context.Background(), h.account, app.ID)
	require.NoError(t, err)
	assert.True(t, view.Multiple)
	assert.Len(t, view.Domains, 2)
	assert.Equal(t, domain.DomainStatusConfigured, view.Domains[1].Status)
}

func TestDomainsAreNotFoundWithoutTheConfig(t *testing.T) {
	h := newTenantHarness(t, nil)
	app := h.app(t, "hello")
	_, err := h.svc.SetDomains(context.Background(), h.user, h.account, app.ID, domain.DomainsInput{Domains: []string{"www.example.com"}})
	require.ErrorIs(t, err, domain.ErrNotFound)
	_, err = h.svc.AppDomains(context.Background(), h.account, app.ID)
	require.ErrorIs(t, err, domain.ErrNotFound)
}

func TestAdoptedAppsReadTheirDomainsBack(t *testing.T) {
	h := domainsHarness(t, "string")
	app := h.app(t, "hello")
	other := h.app(t, "plain")
	_, err := h.svc.SetDomains(context.Background(), h.user, h.account, app.ID, domain.DomainsInput{Domains: []string{"www.example.com"}})
	require.NoError(t, err)

	require.NoError(t, h.store.DeleteEnvironment(context.Background(), app.ID))
	require.NoError(t, h.store.DeleteEnvironment(context.Background(), other.ID))
	_, err = h.svc.ListEnvironments(context.Background(), h.account)
	require.NoError(t, err)

	adopted, err := h.store.GetEnvironment(context.Background(), app.ID)
	require.NoError(t, err)
	assert.Equal(t, &domain.DomainSetting{Hosts: []string{"www.example.com"}}, adopted.Domains)
	plain, err := h.store.GetEnvironment(context.Background(), other.ID)
	require.NoError(t, err)
	assert.Nil(t, plain.Domains, "the pinned subdomain is not a customer's domain")
}

func TestDomainsAreNotSharedAndARejectedOneIsDropped(t *testing.T) {
	h := domainsHarness(t, "string")
	ctx := context.Background()
	first := h.app(t, "hello")
	second := h.app(t, "other")
	_, err := h.svc.SetDomains(ctx, h.user, h.account, first.ID, domain.DomainsInput{Domains: []string{"www.example.com"}})
	require.NoError(t, err)

	var verr *domain.ValidationError
	_, err = h.svc.SetDomains(ctx, h.user, h.account, second.ID, domain.DomainsInput{Domains: []string{"WWW.example.com"}})
	require.ErrorAs(t, err, &verr, "another app already answers on it")

	deploy := h.platform.DeployInstanceFunc
	h.platform.DeployInstanceFunc = func(_ context.Context, id string, params map[string]any, _ string) error {
		h.world.set(id, "FAILED", params)
		return nil
	}
	_, err = h.svc.SetDomains(ctx, h.user, h.account, first.ID, domain.DomainsInput{Domains: []string{"bad.example.com"}})
	require.NoError(t, err)
	h.platform.DeployInstanceFunc = deploy
	stored, err := h.store.GetEnvironment(ctx, first.ID)
	require.NoError(t, err)
	assert.Equal(t, []string{"www.example.com"}, stored.Domains.Hosts, "the domain the deploy failed with is not kept")
}

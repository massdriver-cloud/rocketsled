package domain_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/massdriver-cloud/rocketsled/internal/domain"
	"github.com/massdriver-cloud/rocketsled/internal/domain/mocks"
	"github.com/massdriver-cloud/rocketsled/internal/store/memory"
)

var fixedTime = time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)

// shopProject is the blueprint every test shares: an app plus a database.
func shopProject() *domain.Project {
	return &domain.Project{
		ID:   "shop",
		Name: "Shop",
		Components: []domain.Component{
			{Slug: "api", Name: "API"},
			{Slug: "db", Name: "Database"},
		},
		Environments: []domain.EnvironmentSummary{{ID: "shop-prod", Name: "Production"}},
	}
}

func appSchema() map[string]any {
	return map[string]any{
		"properties": map[string]any{
			"image": map[string]any{"properties": map[string]any{"tag": map[string]any{"type": "string"}}},
		},
		"examples": []any{
			map[string]any{"__name": "Small", "replicas": 1},
			map[string]any{"__name": "Large", "replicas": 6},
		},
	}
}

func dbSchema() map[string]any {
	return map[string]any{
		"properties": map[string]any{"size": map[string]any{"type": "string"}},
		"examples": []any{
			map[string]any{"__name": "Development", "size": "small", "backups": map[string]any{"enabled": false}},
			map[string]any{"__name": "Production", "size": "large", "backups": map[string]any{"enabled": true}},
		},
	}
}

func environment(id string, status string) *domain.Environment {
	return &domain.Environment{
		ID:        id,
		Name:      id,
		ProjectID: "shop",
		Instances: []domain.PlatformInstance{
			{
				ID: id + "-api", ComponentSlug: "api", BundleName: "shop-api", Version: "1.2.0", Status: status,
				Params:       map[string]any{"image": map[string]any{"repository": "ghcr.io/acme/shop", "tag": "old"}, "replicas": 2},
				ParamsSchema: appSchema(),
			},
			{
				ID: id + "-db", ComponentSlug: "db", BundleName: "aws-postgres", Version: "3.0.1", Status: status,
				Params:       map[string]any{"size": "medium", "backups": map[string]any{"enabled": true, "days": 7}},
				ParamsSchema: dbSchema(),
			},
		},
	}
}

type harness struct {
	svc      *domain.Service
	platform *mocks.PlatformMock
	scm      *mocks.SCMMock
	builder  *mocks.BuilderMock
	store    *memory.Store
	user     *domain.User
	ids      []string
	link     domain.ProjectLink
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	h := &harness{
		platform: &mocks.PlatformMock{},
		scm:      &mocks.SCMMock{},
		builder:  &mocks.BuilderMock{},
		store:    memory.New(),
		link:     domain.ProjectLink{ID: "shop", BaseEnvironment: "prod", ImageTagParam: "image.tag"},
		user:     &domain.User{ID: "github:1", Provider: domain.ProviderGitHub, Login: "alice"},
		ids:      []string{"id-1", "id-2", "id-3", "id-4", "id-5", "id-6"},
	}
	h.scm.ProviderFunc = func() domain.Provider { return domain.ProviderGitHub }
	h.scm.CloneURLFunc = func(repo domain.Repo, token string) string {
		return "https://x-access-token:" + token + "@github.com/" + repo.FullName + ".git"
	}
	h.platform.GetProjectFunc = func(_ context.Context, id string) (*domain.Project, error) {
		if id != "shop" {
			return nil, domain.ErrNotFound
		}
		return shopProject(), nil
	}
	h.platform.GetEnvironmentFunc = func(_ context.Context, id string) (*domain.Environment, error) {
		return environment(id, "PROVISIONED"), nil
	}

	require.NoError(t, h.store.UpsertUser(context.Background(), h.user))
	require.NoError(t, h.store.SaveCredential(context.Background(), h.user.ID, "gh-token"))

	return h.build(t)
}

// build wires the service; tests that change h.link call it again.
func (h *harness) build(t *testing.T) *harness {
	t.Helper()
	svc, err := domain.NewService(domain.Options{
		Platform:      h.platform,
		Store:         h.store,
		SCMs:          []domain.SCM{h.scm},
		Projects:      []domain.ProjectLink{h.link},
		Builder:       h.builder,
		BaseURL:       "https://sled.example.com/",
		WebhookSecret: "hook-secret",
		RunAsync:      func(f func()) { f() },
		Now:           func() time.Time { return fixedTime },
		NewID: func() string {
			id := h.ids[0]
			h.ids = h.ids[1:]
			return id
		},
	})
	require.NoError(t, err)
	h.svc = svc
	return h
}

func (h *harness) latestDeploy(t *testing.T, instanceID string) domain.Deploy {
	t.Helper()
	deploys, err := h.store.ListDeploys(context.Background(), instanceID, 1)
	require.NoError(t, err)
	require.NotEmpty(t, deploys)
	return deploys[0]
}

func (h *harness) linkRepo(t *testing.T) *domain.Stack {
	t.Helper()
	h.scm.GetRepoFunc = func(_ context.Context, token, fullName string) (*domain.Repo, error) {
		assert.Equal(t, "gh-token", token)
		return &domain.Repo{Provider: domain.ProviderGitHub, ExternalID: "42", FullName: fullName, URL: "https://github.com/" + fullName, DefaultBranch: "main"}, nil
	}
	h.scm.CreateWebhookFunc = func(_ context.Context, _, _, callbackURL, secret string) (string, error) {
		assert.Equal(t, "https://sled.example.com/hooks/github", callbackURL)
		assert.Equal(t, "hook-secret", secret)
		return "hook-7", nil
	}
	stack, _, err := h.svc.CreateStack(context.Background(), h.user, domain.CreateStackInput{RepoFullName: "acme/shop", ProjectID: "shop"})
	require.NoError(t, err)
	return stack
}

func TestListProjectsEnrichesComponentsFromBaseEnvironment(t *testing.T) {
	h := newHarness(t)

	projects, err := h.svc.ListProjects(context.Background())
	require.NoError(t, err)
	require.Len(t, projects, 1)

	assert.Equal(t, "api", projects[0].AppComponent)
	assert.Equal(t, "shop-prod", h.platform.GetEnvironmentCalls()[0].EnvironmentID)
	db := projects[0].Project.Components[1]
	assert.Equal(t, "aws-postgres", db.Bundle.Name)
	assert.Equal(t, "3.0.1", db.Bundle.Version)
	assert.Equal(t, []string{"Development", "Production"}, presetNames(db.Bundle.Presets))
}

func presetNames(ps []domain.Preset) []string {
	out := make([]string, 0, len(ps))
	for _, p := range ps {
		out = append(out, p.Name)
	}
	return out
}

func TestCreateStackRejectsProjectsNotOptedIn(t *testing.T) {
	h := newHarness(t)

	_, _, err := h.svc.CreateStack(context.Background(), h.user, domain.CreateStackInput{RepoFullName: "acme/shop", ProjectID: "payments"})

	var verr *domain.ValidationError
	require.ErrorAs(t, err, &verr)
	assert.Equal(t, "projectId", verr.Field)
}

func TestCreateStackLinksRepoAndRegistersWebhook(t *testing.T) {
	h := newHarness(t)

	stack := h.linkRepo(t)

	assert.Equal(t, "shop", stack.Name, "name defaults to the repository name")
	assert.Equal(t, "acme/shop", stack.Repo.FullName)
	assert.Equal(t, "hook-7", stack.WebhookID)
	assert.NotEmpty(t, stack.DeployTokenHash)
	assert.Equal(t, fixedTime, stack.CreatedAt)

	stacks, err := h.svc.ListStacks(context.Background())
	require.NoError(t, err)
	assert.Len(t, stacks, 1)
}

func TestCreateStackSurvivesWebhookFailure(t *testing.T) {
	h := newHarness(t)
	h.scm.GetRepoFunc = func(context.Context, string, string) (*domain.Repo, error) {
		return &domain.Repo{Provider: domain.ProviderGitHub, FullName: "acme/shop", DefaultBranch: "main"}, nil
	}
	h.scm.CreateWebhookFunc = func(context.Context, string, string, string, string) (string, error) {
		return "", errors.New("403 admin rights required")
	}

	stack, token, err := h.svc.CreateStack(context.Background(), h.user, domain.CreateStackInput{Name: "Shop", RepoFullName: "acme/shop", ProjectID: "shop"})
	require.NoError(t, err)
	assert.Empty(t, stack.WebhookID)
	assert.Contains(t, token, "rsd_")
}

func TestCreateInstanceValidatesSlugAndPresets(t *testing.T) {
	h := newHarness(t)
	stack := h.linkRepo(t)

	_, err := h.svc.CreateInstance(context.Background(), h.user, stack.ID, domain.CreateInstanceInput{Slug: "Bad-Slug"})
	var verr *domain.ValidationError
	require.ErrorAs(t, err, &verr)
	assert.Equal(t, "slug", verr.Field)

	_, err = h.svc.CreateInstance(context.Background(), h.user, stack.ID, domain.CreateInstanceInput{
		Slug: "pr42", Presets: map[string]string{"db": "Gigantic"},
	})
	require.ErrorAs(t, err, &verr)
	assert.Equal(t, "presets.db", verr.Field)
	assert.Contains(t, verr.Message, `aws-postgres has no preset "Gigantic"`)

	_, err = h.svc.CreateInstance(context.Background(), h.user, stack.ID, domain.CreateInstanceInput{
		Slug: "pr42", Presets: map[string]string{"cache": "Small"},
	})
	require.ErrorAs(t, err, &verr)
	assert.Equal(t, "presets.cache", verr.Field)
	assert.Empty(t, h.platform.ForkEnvironmentCalls(), "nothing is forked until input is valid")
}

func TestCreateInstanceForksAppliesPresetsAndDeploys(t *testing.T) {
	h := newHarness(t)
	stack := h.linkRepo(t)
	h.scm.BranchHeadFunc = func(_ context.Context, _, fullName, branch string) (string, error) {
		assert.Equal(t, "acme/shop", fullName)
		assert.Equal(t, "feature/checkout", branch)
		return "abcdef1234567890", nil
	}
	h.platform.ForkEnvironmentFunc = func(_ context.Context, req domain.ForkRequest) (*domain.Environment, error) {
		assert.Equal(t, "shop-prod", req.ParentEnvironmentID)
		assert.Equal(t, "pr42", req.ID)
		assert.Equal(t, "Checkout preview", req.Name)
		assert.True(t, req.CopyEnvironmentDefaults)
		return environment("shop-pr42", "INITIALIZED"), nil
	}

	inst, err := h.svc.CreateInstance(context.Background(), h.user, stack.ID, domain.CreateInstanceInput{
		Slug:    "pr42",
		Name:    "Checkout preview",
		Branch:  "feature/checkout",
		Presets: map[string]string{"db": "Development"},
	})
	require.NoError(t, err)

	assert.Equal(t, "shop-pr42", inst.EnvironmentID)
	assert.Equal(t, domain.InstanceStateLaunching, inst.State)
	assert.Equal(t, map[string]string{"db": "Development"}, inst.Presets)

	copies := h.platform.CopyInstanceCalls()
	require.Len(t, copies, 2)
	assert.Equal(t, "shop-prod-api", copies[0].SourceID)
	assert.Equal(t, "shop-pr42-api", copies[0].DestinationID)
	assert.Equal(t, map[string]any{"image": map[string]any{"tag": "abcdef1234567890"}}, copies[0].Overrides)
	assert.Equal(t, "shop-prod-db", copies[1].SourceID)
	assert.Equal(t, "shop-pr42-db", copies[1].DestinationID)
	assert.Equal(t, map[string]any{"size": "small", "backups": map[string]any{"enabled": false}}, copies[1].Overrides,
		"preset params are sent as overrides with __name stripped")

	require.Len(t, h.platform.DeployEnvironmentCalls(), 1)
	assert.Equal(t, "shop-pr42", h.platform.DeployEnvironmentCalls()[0].EnvironmentID)

	deploys, err := h.store.ListDeploys(context.Background(), inst.ID, 10)
	require.NoError(t, err)
	require.Len(t, deploys, 1)
	assert.Equal(t, 1, deploys[0].Number)
	assert.Equal(t, domain.TriggerLaunch, deploys[0].Trigger)
	assert.Equal(t, "abcdef1234567890", deploys[0].SHA)
	assert.Equal(t, domain.DeployStatusRunning, deploys[0].Status)
}

func TestCreateInstanceDefaultsBranchAndRejectsDuplicateSlug(t *testing.T) {
	h := newHarness(t)
	stack := h.linkRepo(t)
	h.scm.BranchHeadFunc = func(_ context.Context, _, _, branch string) (string, error) {
		assert.Equal(t, "main", branch)
		return "1111111", nil
	}
	h.platform.ForkEnvironmentFunc = func(_ context.Context, req domain.ForkRequest) (*domain.Environment, error) {
		return environment("shop-"+req.ID, "INITIALIZED"), nil
	}

	inst, err := h.svc.CreateInstance(context.Background(), h.user, stack.ID, domain.CreateInstanceInput{Slug: "staging"})
	require.NoError(t, err)
	assert.Equal(t, "main", inst.Branch)
	assert.Equal(t, "staging", inst.Name)

	_, err = h.svc.CreateInstance(context.Background(), h.user, stack.ID, domain.CreateInstanceInput{Slug: "staging"})
	require.ErrorIs(t, err, domain.ErrConflict)
}

func TestCreateInstanceRecordsFailedLaunch(t *testing.T) {
	h := newHarness(t)
	stack := h.linkRepo(t)
	h.scm.BranchHeadFunc = func(context.Context, string, string, string) (string, error) { return "2222222", nil }
	h.platform.ForkEnvironmentFunc = func(_ context.Context, req domain.ForkRequest) (*domain.Environment, error) {
		return environment("shop-"+req.ID, "INITIALIZED"), nil
	}
	h.platform.DeployEnvironmentFunc = func(context.Context, string) error {
		return errors.New("unfulfilled dependency: aws-iam-role")
	}

	inst, err := h.svc.CreateInstance(context.Background(), h.user, stack.ID, domain.CreateInstanceInput{Slug: "dev"})
	require.NoError(t, err, "the launch request succeeds; the pipeline fails afterwards")

	stored, err := h.store.GetInstance(context.Background(), inst.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.InstanceStateFailed, stored.State, "the record is kept so the user can destroy or retry")

	run := h.latestDeploy(t, inst.ID)
	assert.Equal(t, domain.DeployStatusFailed, run.Status)
	assert.Contains(t, run.Message, "aws-iam-role")
}

func launched(t *testing.T, h *harness) (*domain.Stack, *domain.Instance) {
	t.Helper()
	stack := h.linkRepo(t)
	h.scm.BranchHeadFunc = func(context.Context, string, string, string) (string, error) { return "3333333", nil }
	h.platform.ForkEnvironmentFunc = func(_ context.Context, req domain.ForkRequest) (*domain.Environment, error) {
		return environment("shop-"+req.ID, "INITIALIZED"), nil
	}
	inst, err := h.svc.CreateInstance(context.Background(), h.user, stack.ID, domain.CreateInstanceInput{Slug: "staging", Branch: "main"})
	require.NoError(t, err)
	return stack, inst
}

func TestDeployResolvesBranchHeadAndCreatesDeployment(t *testing.T) {
	h := newHarness(t)
	_, inst := launched(t, h)
	h.scm.BranchHeadFunc = func(context.Context, string, string, string) (string, error) { return "4444444", nil }
	h.platform.CreateDeploymentFunc = func(_ context.Context, instanceID string, params map[string]any, message string) (string, error) {
		assert.Equal(t, "shop-staging-api", instanceID)
		assert.Equal(t, map[string]any{
			"image":    map[string]any{"repository": "ghcr.io/acme/shop", "tag": "4444444"},
			"replicas": 2,
		}, params, "the full current params are resent with only the tag changed")
		assert.Equal(t, "rocketsled run #2: main@4444444", message)
		return "dep-uuid", nil
	}

	d, err := h.svc.Deploy(context.Background(), inst.ID, domain.DeployInput{Actor: h.user})
	require.NoError(t, err)

	assert.Equal(t, 2, d.Number)
	assert.Equal(t, domain.TriggerManual, d.Trigger)
	assert.Equal(t, "4444444", d.SHA)
	assert.Equal(t, domain.DeployStatusQueued, d.Status, "the response is the accepted request")

	run := h.latestDeploy(t, inst.ID)
	assert.Equal(t, []string{"dep-uuid"}, run.PlatformDeploymentIDs)
	assert.Equal(t, domain.DeployStatusRunning, run.Status)
}

func TestDeployWithoutActorRequiresSHA(t *testing.T) {
	h := newHarness(t)
	_, inst := launched(t, h)

	_, err := h.svc.Deploy(context.Background(), inst.ID, domain.DeployInput{Trigger: domain.TriggerWebhook})

	var verr *domain.ValidationError
	require.ErrorAs(t, err, &verr)
	assert.Equal(t, "sha", verr.Field)
}

func TestHandlePushDeploysOnlyInstancesFollowingTheBranch(t *testing.T) {
	h := newHarness(t)
	stack, _ := launched(t, h)
	h.platform.ForkEnvironmentFunc = func(_ context.Context, req domain.ForkRequest) (*domain.Environment, error) {
		return environment("shop-"+req.ID, "INITIALIZED"), nil
	}
	_, err := h.svc.CreateInstance(context.Background(), h.user, stack.ID, domain.CreateInstanceInput{Slug: "pr7", Branch: "feature/x"})
	require.NoError(t, err)
	h.platform.CreateDeploymentFunc = func(_ context.Context, instanceID string, params map[string]any, _ string) (string, error) {
		tag, _ := domain.GetParam(params, "image.tag")
		assert.Equal(t, "feedface", tag)
		return "dep-" + instanceID, nil
	}

	deploys, err := h.svc.HandlePush(context.Background(), domain.PushEvent{
		Provider: domain.ProviderGitHub, RepoFullName: "acme/shop", Branch: "feature/x", SHA: "feedface",
	})
	require.NoError(t, err)
	require.Len(t, deploys, 1)
	assert.Equal(t, domain.TriggerWebhook, deploys[0].Trigger)
	assert.Equal(t, []string{"dep-shop-pr7-api"}, h.latestDeploy(t, deploys[0].InstanceID).PlatformDeploymentIDs)

	none, err := h.svc.HandlePush(context.Background(), domain.PushEvent{Provider: domain.ProviderGitHub, RepoFullName: "acme/shop", Branch: "feature/x", Deleted: true})
	require.NoError(t, err)
	assert.Empty(t, none)
}

func TestDeployFromCIChecksTokenAndBranch(t *testing.T) {
	h := newHarness(t)
	h.scm.GetRepoFunc = func(_ context.Context, _, fullName string) (*domain.Repo, error) {
		return &domain.Repo{Provider: domain.ProviderGitHub, FullName: fullName, DefaultBranch: "main"}, nil
	}
	stack, token, err := h.svc.CreateStack(context.Background(), h.user, domain.CreateStackInput{RepoFullName: "acme/shop", ProjectID: "shop"})
	require.NoError(t, err)
	h.scm.BranchHeadFunc = func(context.Context, string, string, string) (string, error) { return "5555555", nil }
	h.platform.ForkEnvironmentFunc = func(_ context.Context, req domain.ForkRequest) (*domain.Environment, error) {
		return environment("shop-"+req.ID, "INITIALIZED"), nil
	}
	_, err = h.svc.CreateInstance(context.Background(), h.user, stack.ID, domain.CreateInstanceInput{Slug: "staging", Branch: "main"})
	require.NoError(t, err)
	h.platform.CreateDeploymentFunc = func(_ context.Context, _ string, params map[string]any, _ string) (string, error) {
		tag, _ := domain.GetParam(params, "image.tag")
		assert.Equal(t, "v1.4.0", tag)
		return "dep-ci", nil
	}

	_, err = h.svc.DeployFromCI(context.Background(), stack.ID, "rsd_wrong", domain.DeployInput{Ref: "main", SHA: "6666666"})
	require.ErrorIs(t, err, domain.ErrForbidden)

	deploys, err := h.svc.DeployFromCI(context.Background(), stack.ID, token, domain.DeployInput{Ref: "main", SHA: "6666666", ImageTag: "v1.4.0"})
	require.NoError(t, err)
	require.Len(t, deploys, 1)
	assert.Equal(t, domain.TriggerCI, deploys[0].Trigger)
	assert.Equal(t, "v1.4.0", deploys[0].ImageTag)
}

func TestInstanceStatusRefreshesRunsAndState(t *testing.T) {
	h := newHarness(t)
	_, inst := launched(t, h)
	h.platform.CreateDeploymentFunc = func(context.Context, string, map[string]any, string) (string, error) { return "dep-1", nil }
	_, err := h.svc.Deploy(context.Background(), inst.ID, domain.DeployInput{SHA: "7777777"})
	require.NoError(t, err)
	h.platform.GetDeploymentFunc = func(_ context.Context, id string) (*domain.DeploymentSummary, error) {
		return &domain.DeploymentSummary{ID: id, Status: "COMPLETED"}, nil
	}

	view, err := h.svc.InstanceStatus(context.Background(), inst.ID)
	require.NoError(t, err)

	assert.Equal(t, domain.InstanceStateRunning, view.Instance.State, "all packages PROVISIONED")
	require.Len(t, view.Deploys, 2)
	assert.Equal(t, 2, view.Deploys[0].Number)
	assert.Equal(t, domain.DeployStatusSucceeded, view.Deploys[0].Status, "refreshed from the platform deployment")
	assert.Equal(t, domain.DeployStatusSucceeded, view.Deploys[1].Status, "launch run derived from package statuses")
	assert.Nil(t, view.Environment.Instances[0].Params, "params are not exposed in status views")
}

func TestInstanceStatusMarksFailedPackages(t *testing.T) {
	h := newHarness(t)
	_, inst := launched(t, h)
	h.platform.GetEnvironmentFunc = func(_ context.Context, id string) (*domain.Environment, error) {
		env := environment(id, "PROVISIONED")
		env.Instances[1].Status = "FAILED"
		return env, nil
	}

	view, err := h.svc.InstanceStatus(context.Background(), inst.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.InstanceStateFailed, view.Instance.State)
	assert.Equal(t, domain.DeployStatusFailed, view.Deploys[0].Status)
}

func TestDestroyInstanceDecommissionsThenDeletesWhenEmpty(t *testing.T) {
	h := newHarness(t)
	_, inst := launched(t, h)

	got, err := h.svc.DestroyInstance(context.Background(), inst.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.InstanceStateDecommissioning, got.State)
	require.Len(t, h.platform.DecommissionEnvironmentCalls(), 1)
	assert.Equal(t, "shop-staging", h.platform.DecommissionEnvironmentCalls()[0].EnvironmentID)

	_, err = h.svc.Deploy(context.Background(), inst.ID, domain.DeployInput{SHA: "8888888"})
	require.ErrorIs(t, err, domain.ErrConflict)

	view, err := h.svc.InstanceStatus(context.Background(), inst.ID)
	require.NoError(t, err)
	assert.False(t, view.Gone, "packages still PROVISIONED")
	assert.Empty(t, h.platform.DeleteEnvironmentCalls())

	h.platform.GetEnvironmentFunc = func(_ context.Context, id string) (*domain.Environment, error) {
		return environment(id, "DECOMMISSIONED"), nil
	}
	view, err = h.svc.InstanceStatus(context.Background(), inst.ID)
	require.NoError(t, err)
	assert.True(t, view.Gone)
	require.Len(t, h.platform.DeleteEnvironmentCalls(), 1)
	_, err = h.store.GetInstance(context.Background(), inst.ID)
	assert.ErrorIs(t, err, domain.ErrNotFound)
}

func TestDeleteStackRefusesWhileInstancesExist(t *testing.T) {
	h := newHarness(t)
	stack, _ := launched(t, h)

	err := h.svc.DeleteStack(context.Background(), h.user, stack.ID)
	require.ErrorIs(t, err, domain.ErrConflict)
}

func TestSignInStoresUserAndToken(t *testing.T) {
	h := newHarness(t)
	h.scm.ExchangeFunc = func(_ context.Context, code, redirect string) (string, error) {
		assert.Equal(t, "the-code", code)
		assert.Equal(t, "https://sled.example.com/auth/github/callback", redirect)
		return "fresh-token", nil
	}
	h.scm.CurrentUserFunc = func(_ context.Context, token string) (*domain.User, error) {
		assert.Equal(t, "fresh-token", token)
		return &domain.User{ProviderUserID: "99", Login: "bob", Name: "Bob"}, nil
	}

	u, err := h.svc.SignIn(context.Background(), domain.ProviderGitHub, "the-code", "https://sled.example.com/auth/github/callback")
	require.NoError(t, err)
	assert.Equal(t, "github:99", u.ID)
	assert.Equal(t, fixedTime, u.CreatedAt)

	tok, err := h.store.GetCredential(context.Background(), "github:99")
	require.NoError(t, err)
	assert.Equal(t, "fresh-token", tok)

	_, err = h.svc.SignIn(context.Background(), domain.Provider("bitbucket"), "x", "y")
	assert.ErrorIs(t, err, domain.ErrNotFound)
}

func TestClassifyDeployment(t *testing.T) {
	cases := map[domain.DeploymentStatus]domain.DeployStatus{
		"PENDING": domain.DeployStatusQueued, "PROPOSED": domain.DeployStatusQueued, "APPROVED": domain.DeployStatusQueued,
		"RUNNING": domain.DeployStatusRunning, "COMPLETED": domain.DeployStatusSucceeded,
		"FAILED": domain.DeployStatusFailed, "ABORTED": domain.DeployStatusFailed, "REJECTED": domain.DeployStatusFailed,
	}
	for in, want := range cases {
		assert.Equal(t, want, domain.ClassifyDeployment(in), string(in))
	}
}

func TestCreateStackValidatesPath(t *testing.T) {
	h := newHarness(t)
	h.scm.GetRepoFunc = func(_ context.Context, _, fullName string) (*domain.Repo, error) {
		return &domain.Repo{Provider: domain.ProviderGitHub, FullName: fullName, DefaultBranch: "main"}, nil
	}

	_, _, err := h.svc.CreateStack(context.Background(), h.user, domain.CreateStackInput{RepoFullName: "acme/shop", ProjectID: "shop", Path: "../../etc"})
	var verr *domain.ValidationError
	require.ErrorAs(t, err, &verr)
	assert.Equal(t, "path", verr.Field)

	stack, _, err := h.svc.CreateStack(context.Background(), h.user, domain.CreateStackInput{RepoFullName: "acme/shop", ProjectID: "shop", Path: "/example/hello-world/"})
	require.NoError(t, err)
	assert.Equal(t, "example/hello-world", stack.Path)
}

func withRegistry(t *testing.T) *harness {
	t.Helper()
	h := newHarness(t)
	h.link.ImageRegistry = "123.dkr.ecr.us-west-2.amazonaws.com/{{stack}}"
	h.link.ImageTagParam = "image_tag"
	h.link.AppComponent = "api"
	return h.build(t)
}

func TestLaunchBuildsImageBeforeConfiguringAndDeploying(t *testing.T) {
	h := withRegistry(t)
	h.scm.GetRepoFunc = func(_ context.Context, _, fullName string) (*domain.Repo, error) {
		return &domain.Repo{Provider: domain.ProviderGitHub, FullName: fullName, URL: "https://github.com/" + fullName, DefaultBranch: "main"}, nil
	}
	stack, _, err := h.svc.CreateStack(context.Background(), h.user, domain.CreateStackInput{Name: "Hello World", RepoFullName: "acme/shop", ProjectID: "shop", Path: "example/hello-world"})
	require.NoError(t, err)
	h.scm.BranchHeadFunc = func(context.Context, string, string, string) (string, error) { return "c0ffee0000", nil }
	h.platform.ForkEnvironmentFunc = func(_ context.Context, req domain.ForkRequest) (*domain.Environment, error) {
		return environment("shop-"+req.ID, "INITIALIZED"), nil
	}
	var order []string
	h.builder.BuildFunc = func(_ context.Context, req domain.BuildRequest) (*domain.BuildResult, error) {
		order = append(order, "build")
		assert.Equal(t, "https://x-access-token:gh-token@github.com/acme/shop.git", req.CloneURL)
		assert.Equal(t, "c0ffee0000", req.SHA)
		assert.Equal(t, "example/hello-world", req.Path)
		assert.Equal(t, "123.dkr.ecr.us-west-2.amazonaws.com/hello-world:c0ffee0000", req.Image)
		_, _ = req.Log.Write([]byte("Step 1/4 : FROM golang\n"))
		return &domain.BuildResult{Image: req.Image, Digest: "sha256:beef"}, nil
	}
	h.platform.CopyInstanceFunc = func(context.Context, string, string, map[string]any) error {
		order = append(order, "copy")
		return nil
	}
	h.platform.DeployEnvironmentFunc = func(context.Context, string) error {
		order = append(order, "deploy")
		return nil
	}

	inst, err := h.svc.CreateInstance(context.Background(), h.user, stack.ID, domain.CreateInstanceInput{Slug: "dev"})
	require.NoError(t, err)

	assert.Equal(t, []string{"build", "copy", "deploy"}, order)
	assert.Equal(t, map[string]any{"image_tag": "c0ffee0000"}, h.platform.CopyInstanceCalls()[0].Overrides)
	run := h.latestDeploy(t, inst.ID)
	assert.Equal(t, domain.DeployStatusRunning, run.Status)
	assert.Equal(t, "123.dkr.ecr.us-west-2.amazonaws.com/hello-world:c0ffee0000", run.Image)
	assert.Equal(t, "c0ffee0000", run.ImageTag)
	assert.Contains(t, run.Log, "Step 1/4")
}

func TestBuildFailureFailsTheRunAndKeepsTheLog(t *testing.T) {
	h := withRegistry(t)
	_, inst := launched(t, h)
	h.builder.BuildFunc = func(_ context.Context, req domain.BuildRequest) (*domain.BuildResult, error) {
		_, _ = req.Log.Write([]byte("no Dockerfile in \"app\"\n"))
		return nil, errors.New("docker build: exit status 1")
	}

	d, err := h.svc.Deploy(context.Background(), inst.ID, domain.DeployInput{SHA: "feed0000"})
	require.NoError(t, err)
	assert.Equal(t, domain.DeployStatusBuilding, d.Status)

	run := h.latestDeploy(t, inst.ID)
	assert.Equal(t, domain.DeployStatusFailed, run.Status)
	assert.Contains(t, run.Message, "docker build")
	assert.Contains(t, run.Log, "no Dockerfile")
	assert.Empty(t, h.platform.CreateDeploymentCalls(), "nothing is deployed when the build fails")
}

func TestDeployWithExplicitTagSkipsTheBuild(t *testing.T) {
	h := withRegistry(t)
	_, inst := launched(t, h)
	h.platform.CreateDeploymentFunc = func(_ context.Context, _ string, params map[string]any, _ string) (string, error) {
		tag, _ := domain.GetParam(params, "image_tag")
		assert.Equal(t, "v9", tag)
		return "dep-9", nil
	}

	_, err := h.svc.Deploy(context.Background(), inst.ID, domain.DeployInput{SHA: "abc", ImageTag: "v9"})
	require.NoError(t, err)

	assert.Len(t, h.builder.BuildCalls(), 1, "only the launch built an image")
	run := h.latestDeploy(t, inst.ID)
	assert.Equal(t, domain.DeployStatusRunning, run.Status)
	assert.Equal(t, "v9", run.ImageTag)
}

func TestWebhookDeployBuildsWithTheStackOwnersCredential(t *testing.T) {
	h := withRegistry(t)
	_, inst := launched(t, h)
	var cloneURL string
	h.builder.BuildFunc = func(_ context.Context, req domain.BuildRequest) (*domain.BuildResult, error) {
		cloneURL = req.CloneURL
		return &domain.BuildResult{Image: req.Image}, nil
	}

	_, err := h.svc.HandlePush(context.Background(), domain.PushEvent{Provider: domain.ProviderGitHub, RepoFullName: "acme/shop", Branch: "main", SHA: "dead0000"})
	require.NoError(t, err)
	assert.Equal(t, "https://x-access-token:gh-token@github.com/acme/shop.git", cloneURL)
	assert.Equal(t, domain.DeployStatusRunning, h.latestDeploy(t, inst.ID).Status)
}

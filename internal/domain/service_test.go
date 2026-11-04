package domain_test

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/massdriver-cloud/rocketsled/internal/domain"
	"github.com/massdriver-cloud/rocketsled/internal/domain/mocks"
	"github.com/massdriver-cloud/rocketsled/internal/store/memory"
)

var fixedTime = time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)

func awsCatalog() domain.Catalog {
	return domain.Catalog{
		ID: "aws-serverless", Name: "AWS Serverless", Cloud: "aws",
		Platforms: []domain.PlatformType{{
			ID: "aws-iam-role", Label: "AWS IAM Role",
			Schema: map[string]any{
				"required":   []any{"arn", "external_id"},
				"properties": map[string]any{"arn": map[string]any{"title": "Role ARN"}, "external_id": map[string]any{"title": "External ID"}},
			},
		}},
		ResourceTypes: []string{"aws-vpc"}, Bundles: []string{"shared-vpc", "http-api"},
		Bootstrap: domain.Bootstrap{Project: "serverless", BaseEnvironment: "production"},
		Dir:       "/catalogs/aws-serverless",
	}
}

func project() *domain.Project {
	return &domain.Project{
		ID: "serverless", Name: "AWS Serverless Platform",
		Components: []domain.Component{{Slug: "api", Name: "HTTP API"}, {Slug: "app", Name: "Application"}},
		Links:      []domain.Link{{From: "api", To: "app"}},
	}
}

func liveEnv(id, status string) *domain.PlatformEnvironment {
	return &domain.PlatformEnvironment{
		ID: id, Name: id, ProjectID: "serverless",
		Instances: []domain.PlatformInstance{
			{
				ID: id + "-api", ComponentSlug: "api", BundleName: "http-api", Version: "0.0.0", Status: status,
				ParamsSchema: map[string]any{"examples": []any{map[string]any{"__name": "Development", "throttle_rate": 50}, map[string]any{"__name": "Production", "throttle_rate": 1000}}},
				Resources:    []domain.InstanceResource{{Field: "api", Name: "HTTP API", Payload: map[string]any{"endpoint": "https://abc.execute-api.us-east-1.amazonaws.com"}}},
			},
			{
				ID: id + "-app", ComponentSlug: "app", BundleName: "lambda-function", Version: "0.0.0", Status: status,
				ParamsSchema: map[string]any{"examples": []any{map[string]any{"__name": "Free Tier", "memory_mb": 256}}},
			},
		},
	}
}

type harness struct {
	svc       *domain.Service
	platform  *mocks.PlatformMock
	installer *mocks.InstallerMock
	store     *memory.Store
	user      *domain.User
	creds     []domain.Credential
	ids       []string
}

func newHarness(t *testing.T, mode domain.AccountMode) *harness {
	t.Helper()
	h := &harness{
		platform:  &mocks.PlatformMock{},
		installer: &mocks.InstallerMock{},
		store:     memory.New(),
		user:      &domain.User{ID: "dev:cory", Provider: domain.ProviderDev, Login: "cory", Name: "Cory"},
	}
	for i := 0; i < 20; i++ {
		h.ids = append(h.ids, "id-"+string(rune('a'+i)))
	}
	h.platform.PingFunc = func(context.Context) (string, error) { return "RocketSled Test", nil }
	h.platform.GetProjectFunc = func(context.Context, string) (*domain.Project, error) { return project(), nil }
	h.platform.GetEnvironmentFunc = func(_ context.Context, id string) (*domain.PlatformEnvironment, error) {
		return liveEnv(id, "INITIALIZED"), nil
	}
	h.platform.ForkEnvironmentFunc = func(_ context.Context, req domain.ForkRequest) (*domain.PlatformEnvironment, error) {
		return liveEnv("serverless-"+req.ID, "INITIALIZED"), nil
	}
	h.platform.CreateResourceFunc = func(context.Context, string, string, map[string]any) (string, error) { return "res-role", nil }
	h.platform.CreateServiceAccountFunc = func(context.Context, string) (string, error) { return "md_sa_token", nil }
	h.installer.InstallFunc = func(_ context.Context, req domain.InstallRequest) (*domain.InstallResult, error) {
		_, _ = io.WriteString(req.Log, "==> publishing\n")
		return &domain.InstallResult{ProjectID: "serverless", Environments: map[string]string{"production": "serverless-production"}, State: []byte(`{"version":4}`)}, nil
	}
	scm := &mocks.SCMMock{ProviderFunc: func() domain.Provider { return domain.ProviderDev }}
	require.NoError(t, h.store.UpsertUser(context.Background(), h.user))

	svc, err := domain.NewService(domain.Options{
		Platforms: func(c domain.Credential) domain.Platform {
			h.creds = append(h.creds, c)
			return h.platform
		},
		Store:            h.store,
		SCMs:             []domain.SCM{scm},
		Installer:        h.installer,
		Catalogs:         []domain.Catalog{awsCatalog()},
		Mode:             mode,
		SharedCredential: domain.Credential{URL: "https://api.massdriver.cloud", OrganizationID: "rs-shared", APIKey: "md_shared"},
		OrgCreator:       domain.Credential{URL: "https://api.massdriver.cloud", APIKey: "md_pat"},
		DataDir:          t.TempDir(),
		StableReleases:   true,
		RunAsync:         func(f func()) { f() },
		Now:              func() time.Time { return fixedTime },
		NewID:            func() string { id := h.ids[0]; h.ids = h.ids[1:]; return id },
		NewOrgID:         func() string { return "rs-misty-river-42" },
	})
	require.NoError(t, err)
	h.svc = svc
	return h
}

func (h *harness) signedUp(t *testing.T) *domain.Account {
	t.Helper()
	a, err := h.svc.CreateAccount(context.Background(), h.user, "")
	require.NoError(t, err)
	return a
}

func (h *harness) installed(t *testing.T) *domain.Account {
	t.Helper()
	a := h.signedUp(t)
	_, err := h.svc.InstallCatalog(context.Background(), a, "aws-serverless")
	require.NoError(t, err)
	a, err = h.store.GetAccount(context.Background(), a.ID)
	require.NoError(t, err)
	return a
}

func (h *harness) ready(t *testing.T) *domain.Account {
	t.Helper()
	a := h.installed(t)
	a, err := h.svc.ProvidePlatform(context.Background(), a, "aws-iam-role", "", map[string]any{"arn": "arn:aws:iam::1:role/x", "external_id": "e"})
	require.NoError(t, err)
	return a
}

func TestSharedModeBindsAccountsToTheConfiguredOrganization(t *testing.T) {
	h := newHarness(t, domain.AccountModeShared)

	a := h.signedUp(t)

	assert.Equal(t, "Cory", a.Name)
	assert.Equal(t, "rs-shared", a.OrganizationID)
	assert.Equal(t, "RocketSled Test", a.OrganizationName)
	assert.Equal(t, domain.AccountStatusNew, a.Status)
	cred, err := h.store.GetAccountCredential(context.Background(), a.ID)
	require.NoError(t, err)
	assert.Equal(t, "md_shared", cred.APIKey)
	assert.Empty(t, h.platform.CreateOrganizationCalls())

	again, err := h.svc.CreateAccount(context.Background(), h.user, "ignored")
	require.NoError(t, err)
	assert.Equal(t, a.ID, again.ID, "signing up twice returns the same account")
}

func TestOrganizationModeCreatesAnOrgAndServiceAccount(t *testing.T) {
	h := newHarness(t, domain.AccountModeOrganization)

	a := h.signedUp(t)

	require.Len(t, h.platform.CreateOrganizationCalls(), 1)
	assert.Equal(t, "rs-misty-river-42", h.platform.CreateOrganizationCalls()[0].ID)
	assert.Contains(t, h.platform.CreateOrganizationCalls()[0].Name, "RocketSled ")
	assert.Equal(t, "rs-misty-river-42", a.OrganizationID)
	cred, err := h.store.GetAccountCredential(context.Background(), a.ID)
	require.NoError(t, err)
	assert.Equal(t, "md_sa_token", cred.APIKey, "the account uses the new service account, not the creator's PAT")
	assert.Equal(t, domain.Credential{URL: "https://api.massdriver.cloud", APIKey: "md_pat"}, h.creds[0], "the organization is created with the creator credential")
	assert.Equal(t, "rs-misty-river-42", h.creds[1].OrganizationID, "the service account is created inside the new organization")
}

func TestInstallCatalogRunsTheInstallerAndRecordsTheProject(t *testing.T) {
	h := newHarness(t, domain.AccountModeShared)
	a := h.signedUp(t)

	queued, err := h.svc.InstallCatalog(context.Background(), a, "aws-serverless")
	require.NoError(t, err)
	assert.Equal(t, domain.AccountStatusInstalling, queued.Status, "the request returns before the install finishes")

	req := h.installer.InstallCalls()[0].Req
	assert.Equal(t, "aws-serverless", req.Catalog.ID)
	assert.Equal(t, "md_shared", req.Credential.APIKey)
	assert.True(t, req.Stable)
	assert.Contains(t, req.WorkDir, a.ID)

	got, err := h.store.GetAccount(context.Background(), a.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.AccountStatusNeedsCredentials, got.Status)
	assert.Equal(t, "serverless", got.ProjectID)
	assert.Equal(t, "serverless-production", got.BaseEnvironmentID)
	assert.Contains(t, got.InstallLog, "==> publishing")
	state, err := h.store.GetBootstrapState(context.Background(), a.ID)
	require.NoError(t, err)
	assert.JSONEq(t, `{"version":4}`, string(state))

	_, err = h.svc.InstallCatalog(context.Background(), got, "aws-serverless")
	require.ErrorIs(t, err, domain.ErrConflict, "a second install is refused once one succeeded")
}

func TestInstallFailureKeepsLogAndAllowsRetryWithState(t *testing.T) {
	h := newHarness(t, domain.AccountModeShared)
	a := h.signedUp(t)
	h.installer.InstallFunc = func(_ context.Context, req domain.InstallRequest) (*domain.InstallResult, error) {
		_, _ = io.WriteString(req.Log, "tofu apply: boom\n")
		return &domain.InstallResult{State: []byte(`{"partial":true}`)}, errors.New("tofu apply: exit status 1")
	}

	_, err := h.svc.InstallCatalog(context.Background(), a, "aws-serverless")
	require.NoError(t, err)
	got, _ := h.store.GetAccount(context.Background(), a.ID)
	assert.Equal(t, domain.AccountStatusFailed, got.Status)
	assert.Contains(t, got.InstallError, "tofu apply")
	assert.Contains(t, got.InstallLog, "boom")

	h.installer.InstallFunc = func(_ context.Context, req domain.InstallRequest) (*domain.InstallResult, error) {
		assert.JSONEq(t, `{"partial":true}`, string(req.State), "the retry converges from the saved state")
		return &domain.InstallResult{ProjectID: "serverless", Environments: map[string]string{"production": "serverless-production"}}, nil
	}
	_, err = h.svc.InstallCatalog(context.Background(), got, "aws-serverless")
	require.NoError(t, err)
	got, _ = h.store.GetAccount(context.Background(), a.ID)
	assert.Equal(t, domain.AccountStatusNeedsCredentials, got.Status)
	assert.Empty(t, got.InstallError)
}

func TestInstallRejectsUnknownCatalog(t *testing.T) {
	h := newHarness(t, domain.AccountModeShared)
	a := h.signedUp(t)
	_, err := h.svc.InstallCatalog(context.Background(), a, "gcp-moon")
	var verr *domain.ValidationError
	require.ErrorAs(t, err, &verr)
	assert.Equal(t, "catalogId", verr.Field)
}

func TestProvidePlatformCreatesTheResourceAndPinsIt(t *testing.T) {
	h := newHarness(t, domain.AccountModeShared)
	a := h.installed(t)

	reqs, err := h.svc.PlatformRequirements(a)
	require.NoError(t, err)
	require.Len(t, reqs, 1)
	assert.False(t, reqs[0].Satisfied)
	assert.Equal(t, "AWS IAM Role", reqs[0].Type.Label)

	_, err = h.svc.ProvidePlatform(context.Background(), a, "aws-iam-role", "", map[string]any{"arn": "arn:aws:iam::1:role/x"})
	var verr *domain.ValidationError
	require.ErrorAs(t, err, &verr)
	assert.Equal(t, "external_id", verr.Field)
	assert.Equal(t, "External ID is required", verr.Message)

	a, err = h.svc.ProvidePlatform(context.Background(), a, "aws-iam-role", "", map[string]any{"arn": "arn:aws:iam::1:role/x", "external_id": "e"})
	require.NoError(t, err)

	call := h.platform.CreateResourceCalls()[0]
	assert.Equal(t, "aws-iam-role", call.ResourceTypeID)
	assert.Equal(t, "AWS IAM Role", call.Name, "the name defaults to the type's label")
	assert.Equal(t, map[string]string{"aws-iam-role": "res-role"}, a.Platforms)
	assert.Equal(t, domain.AccountStatusReady, a.Status)
	require.Len(t, h.platform.SetEnvironmentDefaultCalls(), 1)
	assert.Equal(t, "serverless-production", h.platform.SetEnvironmentDefaultCalls()[0].EnvironmentID)
	assert.Equal(t, "res-role", h.platform.SetEnvironmentDefaultCalls()[0].ResourceID)

	reqs, _ = h.svc.PlatformRequirements(a)
	assert.True(t, reqs[0].Satisfied)
}

func TestProjectViewReadsPresetsFromTheBaseEnvironment(t *testing.T) {
	h := newHarness(t, domain.AccountModeShared)
	a := h.ready(t)

	view, err := h.svc.Project(context.Background(), a)
	require.NoError(t, err)
	assert.Equal(t, "AWS Serverless Platform", view.Project.Name)
	assert.Equal(t, "aws-serverless", view.Catalog.ID)
	assert.Equal(t, "http-api", view.Project.Components[0].Bundle.Name)
	assert.Equal(t, []string{"Development", "Production"}, presetNames(view.Project.Components[0].Bundle.Presets))
	assert.Equal(t, "serverless-production", h.platform.GetEnvironmentCalls()[0].EnvironmentID)
	assert.Empty(t, view.Environments)
}

func presetNames(ps []domain.Preset) []string {
	out := make([]string, 0, len(ps))
	for _, p := range ps {
		out = append(out, p.Name)
	}
	return out
}

func TestCreateEnvironmentForksConfiguresPinsAndDeploys(t *testing.T) {
	h := newHarness(t, domain.AccountModeShared)
	a := h.ready(t)
	h.platform.ForkEnvironmentFunc = func(_ context.Context, req domain.ForkRequest) (*domain.PlatformEnvironment, error) {
		assert.Equal(t, "serverless-production", req.ParentEnvironmentID)
		assert.Equal(t, "dev", req.ID)
		assert.Equal(t, "Development", req.Name)
		assert.True(t, req.CopyEnvironmentDefaults)
		return liveEnv("serverless-dev", "INITIALIZED"), nil
	}

	env, err := h.svc.CreateEnvironment(context.Background(), h.user, a, domain.CreateEnvironmentInput{
		Slug: "dev", Name: "Development", Presets: map[string]string{"api": "Production"},
	})
	require.NoError(t, err)

	assert.Equal(t, "serverless-dev", env.EnvironmentID)
	assert.Equal(t, domain.EnvironmentStateDeploying, env.State)
	assert.Equal(t, map[string]string{"api": "Production", "app": "Free Tier"}, env.Presets, "components without a choice get the bundle's first preset")

	copies := h.platform.CopyInstanceCalls()
	require.Len(t, copies, 2)
	assert.Equal(t, "serverless-production-api", copies[0].SourceID)
	assert.Equal(t, "serverless-dev-api", copies[0].DestinationID)
	assert.Equal(t, map[string]any{"throttle_rate": 1000}, copies[0].Overrides, "the preset is sent with __name stripped")
	assert.Equal(t, map[string]any{"memory_mb": 256}, copies[1].Overrides)

	defaults := h.platform.SetEnvironmentDefaultCalls()
	assert.Equal(t, "serverless-dev", defaults[len(defaults)-1].EnvironmentID)
	assert.Equal(t, "res-role", defaults[len(defaults)-1].ResourceID)
	require.Len(t, h.platform.DeployEnvironmentCalls(), 1)
	assert.Equal(t, "serverless-dev", h.platform.DeployEnvironmentCalls()[0].EnvironmentID)

	runs, _ := h.store.ListRuns(context.Background(), env.ID, 5)
	require.Len(t, runs, 1)
	assert.Equal(t, domain.RunStatusRunning, runs[0].Status)
	assert.Equal(t, domain.TriggerCreate, runs[0].Trigger)
}

func TestCreateEnvironmentValidation(t *testing.T) {
	h := newHarness(t, domain.AccountModeShared)
	a := h.installed(t)

	_, err := h.svc.CreateEnvironment(context.Background(), h.user, a, domain.CreateEnvironmentInput{Slug: "dev"})
	require.ErrorIs(t, err, domain.ErrConflict, "no environments before the credential is provided")

	a, err = h.svc.ProvidePlatform(context.Background(), a, "aws-iam-role", "", map[string]any{"arn": "arn:aws:iam::1:role/x", "external_id": "e"})
	require.NoError(t, err)
	var verr *domain.ValidationError
	_, err = h.svc.CreateEnvironment(context.Background(), h.user, a, domain.CreateEnvironmentInput{Slug: "Dev!"})
	require.ErrorAs(t, err, &verr)
	assert.Equal(t, "slug", verr.Field)

	_, err = h.svc.CreateEnvironment(context.Background(), h.user, a, domain.CreateEnvironmentInput{Slug: "dev", Presets: map[string]string{"api": "Huge"}})
	require.ErrorAs(t, err, &verr)
	assert.Equal(t, "presets.api", verr.Field)
	assert.Empty(t, h.platform.ForkEnvironmentCalls(), "nothing is forked until input is valid")
}

func TestDeployFailureMarksTheRunAndEnvironment(t *testing.T) {
	h := newHarness(t, domain.AccountModeShared)
	a := h.ready(t)
	h.platform.DeployEnvironmentFunc = func(context.Context, string) error { return errors.New("unfulfilled dependency") }

	env, err := h.svc.CreateEnvironment(context.Background(), h.user, a, domain.CreateEnvironmentInput{Slug: "dev"})
	require.NoError(t, err)

	stored, _ := h.store.GetEnvironment(context.Background(), env.ID)
	assert.Equal(t, domain.EnvironmentStateFailed, stored.State)
	runs, _ := h.store.ListRuns(context.Background(), env.ID, 1)
	assert.Equal(t, domain.RunStatusFailed, runs[0].Status)
	assert.Contains(t, runs[0].Message, "unfulfilled dependency")
}

func TestEnvironmentStatusRefreshesAndHidesParams(t *testing.T) {
	h := newHarness(t, domain.AccountModeShared)
	a := h.ready(t)
	env, err := h.svc.CreateEnvironment(context.Background(), h.user, a, domain.CreateEnvironmentInput{Slug: "dev"})
	require.NoError(t, err)
	h.platform.GetEnvironmentFunc = func(_ context.Context, id string) (*domain.PlatformEnvironment, error) {
		e := liveEnv(id, "PROVISIONED")
		e.Instances[0].Params = map[string]any{"secret": "x"}
		return e, nil
	}

	view, err := h.svc.EnvironmentStatus(context.Background(), a, env.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.EnvironmentStateRunning, view.Environment.State)
	assert.Equal(t, domain.RunStatusSucceeded, view.Runs[0].Status)
	assert.Nil(t, view.Platform.Instances[0].Params)
	assert.Equal(t, "https://abc.execute-api.us-east-1.amazonaws.com", view.Platform.Instances[0].Resources[0].Payload["endpoint"])

	other := &domain.User{ID: "dev:mallory", Provider: domain.ProviderDev, Login: "mallory"}
	require.NoError(t, h.store.UpsertUser(context.Background(), other))
	otherAccount, err := h.svc.CreateAccount(context.Background(), other, "")
	require.NoError(t, err)
	_, err = h.svc.EnvironmentStatus(context.Background(), otherAccount, env.ID)
	require.ErrorIs(t, err, domain.ErrNotFound, "accounts cannot see each other's environments")
}

func TestDeployAndDestroyEnvironment(t *testing.T) {
	h := newHarness(t, domain.AccountModeShared)
	a := h.ready(t)
	env, err := h.svc.CreateEnvironment(context.Background(), h.user, a, domain.CreateEnvironmentInput{Slug: "dev"})
	require.NoError(t, err)

	run, err := h.svc.DeployEnvironment(context.Background(), h.user, a, env.ID)
	require.NoError(t, err)
	assert.Equal(t, 2, run.Number)
	assert.Len(t, h.platform.DeployEnvironmentCalls(), 2)

	got, err := h.svc.DestroyEnvironment(context.Background(), a, env.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.EnvironmentStateDecommissioning, got.State)
	_, err = h.svc.DeployEnvironment(context.Background(), h.user, a, env.ID)
	require.ErrorIs(t, err, domain.ErrConflict)

	h.platform.GetEnvironmentFunc = func(_ context.Context, id string) (*domain.PlatformEnvironment, error) {
		return liveEnv(id, "DECOMMISSIONED"), nil
	}
	view, err := h.svc.EnvironmentStatus(context.Background(), a, env.ID)
	require.NoError(t, err)
	assert.True(t, view.Gone)
	assert.Len(t, h.platform.DeleteEnvironmentCalls(), 1)
	_, err = h.store.GetEnvironment(context.Background(), env.ID)
	require.ErrorIs(t, err, domain.ErrNotFound)
}

func TestNewOrganizationIDFitsTheLimit(t *testing.T) {
	for i := 0; i < 200; i++ {
		id := domain.NewOrganizationID()
		assert.LessOrEqual(t, len(id), 20, id)
		assert.Regexp(t, `^rs-[a-z]+-[a-z]+-\d{2}$|^rs-[a-z]+-\d{2}$`, id)
	}
}

package sqlite_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/massdriver-cloud/rocketsled/internal/domain"
	"github.com/massdriver-cloud/rocketsled/internal/store/sqlite"
)

func open(t *testing.T) *sqlite.Store {
	t.Helper()
	s, err := sqlite.Open(filepath.Join(t.TempDir(), "test.db"), "0123456789abcdef0123456789abcdef")
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestAccountsStoreEncryptedCredentialsAndState(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	at := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	require.NoError(t, s.UpsertUser(ctx, &domain.User{ID: "dev:cory", Provider: domain.ProviderDev, ProviderUserID: "cory", Login: "cory", CreatedAt: at}))

	a := &domain.Account{ID: "acct", Name: "Cory", OwnerUserID: "dev:cory", OrganizationID: "rs-x", OrganizationName: "RocketSled", Status: domain.AccountStatusNew, Platforms: map[string]string{}, CreatedAt: at, UpdatedAt: at}
	require.NoError(t, s.CreateAccount(ctx, a, domain.Credential{URL: "https://api.massdriver.cloud", OrganizationID: "rs-x", APIKey: "md_secret"}))
	require.ErrorIs(t, s.CreateAccount(ctx, &domain.Account{ID: "acct2", OwnerUserID: "dev:cory", Status: domain.AccountStatusNew, CreatedAt: at, UpdatedAt: at}, domain.Credential{APIKey: "x"}), domain.ErrConflict)

	got, err := s.GetAccountByOwner(ctx, "dev:cory")
	require.NoError(t, err)
	assert.Equal(t, *a, *got)

	cred, err := s.GetAccountCredential(ctx, "acct")
	require.NoError(t, err)
	assert.Equal(t, domain.Credential{URL: "https://api.massdriver.cloud", OrganizationID: "rs-x", APIKey: "md_secret"}, cred)

	_, err = s.GetBootstrapState(ctx, "acct")
	require.ErrorIs(t, err, domain.ErrNotFound)
	require.NoError(t, s.SaveBootstrapState(ctx, "acct", []byte(`{"version":4}`)))
	state, err := s.GetBootstrapState(ctx, "acct")
	require.NoError(t, err)
	assert.JSONEq(t, `{"version":4}`, string(state))

	a.Status = domain.AccountStatusReady
	a.ProjectID = "serverless"
	a.Platforms = map[string]string{"aws-iam-role": "res-1"}
	a.InstallLog = "==> done"
	require.NoError(t, s.UpdateAccount(ctx, a))
	got, err = s.GetAccount(ctx, "acct")
	require.NoError(t, err)
	assert.Equal(t, domain.AccountStatusReady, got.Status)
	assert.Equal(t, map[string]string{"aws-iam-role": "res-1"}, got.Platforms)
	assert.Equal(t, "==> done", got.InstallLog)
}

func TestEnvironmentsAndRuns(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	at := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	require.NoError(t, s.UpsertUser(ctx, &domain.User{ID: "dev:cory", Provider: domain.ProviderDev, ProviderUserID: "cory", Login: "cory", CreatedAt: at}))
	require.NoError(t, s.CreateAccount(ctx, &domain.Account{ID: "acct", OwnerUserID: "dev:cory", OrganizationID: "rs-x", Status: domain.AccountStatusReady, CreatedAt: at, UpdatedAt: at}, domain.Credential{APIKey: "k"}))

	e := &domain.Environment{ID: "env1", AccountID: "acct", Slug: "dev", Name: "Dev", EnvironmentID: "serverless-dev", Presets: map[string]string{"api": "Development"}, State: domain.EnvironmentStateDeploying, CreatedBy: "dev:cory", CreatedAt: at, UpdatedAt: at}
	require.NoError(t, s.CreateEnvironment(ctx, e))
	dup := *e
	dup.ID = "env2"
	require.ErrorIs(t, s.CreateEnvironment(ctx, &dup), domain.ErrConflict)

	list, err := s.ListEnvironments(ctx, "acct")
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, *e, list[0])

	e.Presets["db"] = "Development"
	e.Runtime, e.Addons, e.Stage = "ruby", []string{"db"}, "staging"
	e.Domains = &domain.DomainSetting{Hosts: []string{"www.example.com"}, List: true}
	require.NoError(t, s.UpdateEnvironment(ctx, e))
	got, err := s.GetEnvironment(ctx, "env1")
	require.NoError(t, err)
	assert.Equal(t, *e, *got, "every mutable field is saved")

	for n := 1; n <= 3; n++ {
		require.NoError(t, s.CreateRun(ctx, &domain.Run{ID: "r" + string(rune('0'+n)), EnvironmentID: "env1", Number: n, Trigger: domain.TriggerDeploy, Status: domain.RunStatusRunning, CreatedAt: at, UpdatedAt: at}))
	}
	runs, err := s.ListRuns(ctx, "env1", 2)
	require.NoError(t, err)
	require.Len(t, runs, 2)
	assert.Equal(t, 3, runs[0].Number)

	runs[0].Status = domain.RunStatusSucceeded
	require.NoError(t, s.UpdateRun(ctx, &runs[0]))
	runs, _ = s.ListRuns(ctx, "env1", 1)
	assert.Equal(t, domain.RunStatusSucceeded, runs[0].Status)

	require.NoError(t, s.DeleteEnvironment(ctx, "env1"))
	runs, err = s.ListRuns(ctx, "env1", 10)
	require.NoError(t, err)
	assert.Empty(t, runs, "runs cascade with their environment")
}

func TestDeployTokensAndReleases(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	at := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	require.NoError(t, s.UpsertUser(ctx, &domain.User{ID: "dev:cory", Provider: domain.ProviderDev, ProviderUserID: "cory", Login: "cory", CreatedAt: at}))
	require.NoError(t, s.CreateAccount(ctx, &domain.Account{ID: "acct", OwnerUserID: "dev:cory", OrganizationID: "rs-x", Status: domain.AccountStatusReady, CreatedAt: at, UpdatedAt: at}, domain.Credential{APIKey: "k"}))

	tok := &domain.Token{ID: "tok1", AccountID: "acct", Name: "ci", CreatedBy: "dev:cory", CreatedAt: at}
	require.NoError(t, s.CreateToken(ctx, tok, "hash1"))
	got, err := s.GetTokenByHash(ctx, "hash1")
	require.NoError(t, err)
	assert.Equal(t, *tok, *got)
	_, err = s.GetTokenByHash(ctx, "nope")
	require.ErrorIs(t, err, domain.ErrNotFound)

	used := at.Add(time.Hour)
	require.NoError(t, s.TouchToken(ctx, "tok1", used))
	list, err := s.ListTokens(ctx, "acct")
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, used, *list[0].LastUsedAt)

	require.ErrorIs(t, s.DeleteToken(ctx, "other", "tok1"), domain.ErrNotFound, "another account cannot revoke it")
	require.NoError(t, s.DeleteToken(ctx, "acct", "tok1"))
	_, err = s.GetTokenByHash(ctx, "hash1")
	require.ErrorIs(t, err, domain.ErrNotFound)

	e := &domain.Environment{ID: "env1", AccountID: "acct", Slug: "dev", Name: "Dev", EnvironmentID: "ruby-dev", Presets: map[string]string{}, State: domain.EnvironmentStateRunning, CreatedBy: "dev:cory", CreatedAt: at, UpdatedAt: at, Image: "img:1"}
	require.NoError(t, s.CreateEnvironment(ctx, e))
	stored, err := s.GetEnvironment(ctx, "env1")
	require.NoError(t, err)
	assert.Equal(t, "img:1", stored.Image)

	for n := 1; n <= 3; n++ {
		require.NoError(t, s.CreateRelease(ctx, &domain.Release{ID: "rel" + string(rune('0'+n)), EnvironmentID: "env1", Number: n, Image: "img:" + string(rune('0'+n)), RequestedBy: "ci", RunID: "run", CreatedAt: at}))
	}
	releases, err := s.ListReleases(ctx, "env1", 2)
	require.NoError(t, err)
	require.Len(t, releases, 2)
	assert.Equal(t, 3, releases[0].Number)
	assert.Equal(t, "img:3", releases[0].Image)

	require.NoError(t, s.DeleteEnvironment(ctx, "env1"))
	releases, err = s.ListReleases(ctx, "env1", 10)
	require.NoError(t, err)
	assert.Empty(t, releases, "releases cascade with their environment")
}

func TestApps(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	at := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	require.NoError(t, s.UpsertUser(ctx, &domain.User{ID: "dev:cory", Provider: domain.ProviderDev, ProviderUserID: "cory", Login: "cory", CreatedAt: at}))
	require.NoError(t, s.CreateAccount(ctx, &domain.Account{ID: "acct", OwnerUserID: "dev:cory", OrganizationID: "rs-x", Status: domain.AccountStatusReady, CreatedAt: at, UpdatedAt: at}, domain.Credential{APIKey: "k"}))

	app := &domain.App{ID: "shop", AccountID: "acct", Name: "shop", Runtime: "ruby", CreatedBy: "dev:cory", CreatedAt: at}
	require.NoError(t, s.CreateApp(ctx, app))
	require.ErrorIs(t, s.CreateApp(ctx, app), domain.ErrConflict)
	got, err := s.GetApp(ctx, "shop")
	require.NoError(t, err)
	assert.Equal(t, *app, *got)
	list, err := s.ListApps(ctx, "acct")
	require.NoError(t, err)
	assert.Len(t, list, 1)

	e := &domain.Environment{ID: "shop-production", AccountID: "acct", Slug: "shop-production", Name: "production", EnvironmentID: "shop-production", Presets: map[string]string{}, State: domain.EnvironmentStateRunning, CreatedBy: "dev:cory", CreatedAt: at, UpdatedAt: at, App: "shop", Preview: true}
	require.NoError(t, s.CreateEnvironment(ctx, e))
	stored, err := s.GetEnvironment(ctx, "shop-production")
	require.NoError(t, err)
	assert.Equal(t, *e, *stored)

	require.NoError(t, s.DeleteApp(ctx, "shop"))
	_, err = s.GetApp(ctx, "shop")
	require.ErrorIs(t, err, domain.ErrNotFound)
}

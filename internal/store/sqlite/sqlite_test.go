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

func TestMigrationsAreIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "twice.db")
	s1, err := sqlite.Open(path, "0123456789abcdef0123456789abcdef")
	require.NoError(t, err)
	require.NoError(t, s1.Close())
	s2, err := sqlite.Open(path, "0123456789abcdef0123456789abcdef")
	require.NoError(t, err)
	require.NoError(t, s2.Close())
}

func TestUsersAndEncryptedCredentials(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	created := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

	u := &domain.User{ID: "github:1", Provider: domain.ProviderGitHub, ProviderUserID: "1", Login: "alice", Name: "Alice", CreatedAt: created}
	require.NoError(t, s.UpsertUser(ctx, u))
	u.Name = "Alice Liddell"
	u.CreatedAt = created.Add(time.Hour)
	require.NoError(t, s.UpsertUser(ctx, u))

	got, err := s.GetUser(ctx, "github:1")
	require.NoError(t, err)
	assert.Equal(t, "Alice Liddell", got.Name)
	assert.Equal(t, created, got.CreatedAt, "first sign-in time is kept")

	require.NoError(t, s.SaveCredential(ctx, "github:1", "gho_secret"))
	tok, err := s.GetCredential(ctx, "github:1")
	require.NoError(t, err)
	assert.Equal(t, "gho_secret", tok)

	require.NoError(t, s.SaveCredential(ctx, "github:1", "gho_rotated"))
	tok, err = s.GetCredential(ctx, "github:1")
	require.NoError(t, err)
	assert.Equal(t, "gho_rotated", tok)

	_, err = s.GetCredential(ctx, "github:2")
	require.ErrorIs(t, err, domain.ErrNotFound)

	other, err := sqlite.Open(filepath.Join(t.TempDir(), "other.db"), "another-secret-another-secret-12")
	require.NoError(t, err)
	defer func() { _ = other.Close() }()
	require.NoError(t, other.UpsertUser(ctx, u))
	require.NoError(t, other.SaveCredential(ctx, "github:1", "x"))
	_, err = s.GetCredential(ctx, "github:1")
	require.NoError(t, err, "each store decrypts with its own key")
}

func TestStacksInstancesAndDeploys(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	at := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)

	stack := &domain.Stack{
		ID:   "st1",
		Name: "Shop",
		Repo: domain.Repo{Provider: domain.ProviderGitHub, ExternalID: "42", FullName: "acme/shop", URL: "https://github.com/acme/shop", DefaultBranch: "main", Private: true},
		Path: "example/hello-world", ProjectID: "shop", WebhookID: "7", DeployTokenHash: "hash", CreatedBy: "github:1", CreatedAt: at,
	}
	require.NoError(t, s.CreateStack(ctx, stack))
	require.ErrorIs(t, s.CreateStack(ctx, stack), domain.ErrConflict)

	got, err := s.GetStack(ctx, "st1")
	require.NoError(t, err)
	assert.Equal(t, *stack, *got)

	byRepo, err := s.FindStacksByRepo(ctx, domain.ProviderGitHub, "acme/shop")
	require.NoError(t, err)
	assert.Len(t, byRepo, 1)
	none, err := s.FindStacksByRepo(ctx, domain.ProviderGitLab, "acme/shop")
	require.NoError(t, err)
	assert.Empty(t, none)

	inst := &domain.Instance{
		ID: "in1", StackID: "st1", Slug: "staging", Name: "Staging", Branch: "main", EnvironmentID: "shop-staging",
		Presets: map[string]string{"db": "Development"}, State: domain.InstanceStateLaunching, CreatedBy: "github:1", CreatedAt: at, UpdatedAt: at,
	}
	require.NoError(t, s.CreateInstance(ctx, inst))
	dup := *inst
	dup.ID = "in2"
	require.ErrorIs(t, s.CreateInstance(ctx, &dup), domain.ErrConflict, "slug is unique per stack")

	inst.State = domain.InstanceStateRunning
	inst.UpdatedAt = at.Add(time.Minute)
	require.NoError(t, s.UpdateInstance(ctx, inst))
	list, err := s.ListInstances(ctx, "st1")
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, *inst, list[0])

	for n := 1; n <= 3; n++ {
		d := &domain.Deploy{
			ID: "d" + string(rune('0'+n)), InstanceID: "in1", Number: n, Trigger: domain.TriggerManual, Ref: "main", SHA: "abc",
			ImageTag: "abc", Image: "reg/app:abc", Status: domain.DeployStatusRunning, PlatformDeploymentIDs: []string{"p" + string(rune('0'+n))},
			CreatedAt: at, UpdatedAt: at,
		}
		require.NoError(t, s.CreateDeploy(ctx, d))
	}
	deploys, err := s.ListDeploys(ctx, "in1", 2)
	require.NoError(t, err)
	require.Len(t, deploys, 2)
	assert.Equal(t, 3, deploys[0].Number, "newest first")
	assert.Equal(t, []string{"p3"}, deploys[0].PlatformDeploymentIDs)

	deploys[0].Status = domain.DeployStatusSucceeded
	deploys[0].Log = "==> pushed"
	require.NoError(t, s.UpdateDeploy(ctx, &deploys[0]))
	deploys, _ = s.ListDeploys(ctx, "in1", 1)
	assert.Equal(t, domain.DeployStatusSucceeded, deploys[0].Status)
	assert.Equal(t, "==> pushed", deploys[0].Log)
	assert.Equal(t, "reg/app:abc", deploys[0].Image)

	require.NoError(t, s.DeleteInstance(ctx, "in1"))
	deploys, err = s.ListDeploys(ctx, "in1", 10)
	require.NoError(t, err)
	assert.Empty(t, deploys, "deploys cascade with their instance")

	require.NoError(t, s.DeleteStack(ctx, "st1"))
	require.ErrorIs(t, s.DeleteStack(ctx, "st1"), domain.ErrNotFound)
}

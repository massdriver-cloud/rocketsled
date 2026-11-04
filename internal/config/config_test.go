package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/massdriver-cloud/rocketsled/internal/config"
	"github.com/massdriver-cloud/rocketsled/internal/masscfg"
)

func TestParseWithExplicitCredential(t *testing.T) {
	t.Setenv("RS_TEST_KEY", "md_env")
	cfg, err := config.Parse([]byte(`
server:
  session_secret: 0123456789abcdef0123456789abcdef
massdriver:
  organization_id: rs-test
  api_key: ${RS_TEST_KEY}
auth:
  providers:
    dev: {login: cory}
`))
	require.NoError(t, err)
	assert.Equal(t, "md_env", cfg.Massdriver.APIKey)
	assert.Equal(t, "shared", cfg.Massdriver.AccountMode)
	assert.Equal(t, "stable", cfg.Catalogs.ReleaseChannel)
	assert.Equal(t, "data/rocketsled.db", cfg.Database.Path)
}

func TestParseFallsBackToTheMassProfile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte("version: 1\nprofiles:\n  citizens:\n    organization_id: citizens\n    api_key: md_profile\n"), 0o600))
	t.Setenv(masscfg.PathEnv, path)
	t.Setenv(masscfg.ProfileEnv, "citizens")

	cfg, err := config.Parse([]byte(`
server:
  session_secret: 0123456789abcdef0123456789abcdef
auth:
  providers:
    dev: {login: cory}
`))
	require.NoError(t, err)
	assert.Equal(t, "citizens", cfg.Massdriver.OrganizationID)
	assert.Equal(t, "md_profile", cfg.Massdriver.APIKey)
}

func TestParseReportsEveryProblem(t *testing.T) {
	t.Setenv(masscfg.PathEnv, filepath.Join(t.TempDir(), "missing.yaml"))
	_, err := config.Parse([]byte(`
massdriver:
  account_mode: tenant
catalogs:
  release_channel: beta
`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "massdriver credential")

	_, err = config.Parse([]byte(`
massdriver:
  organization_id: x
  api_key: y
  account_mode: tenant
catalogs:
  release_channel: beta
server:
  base_url: https://sled.example.com
auth:
  providers:
    dev: {login: cory}
`))
	require.Error(t, err)
	for _, want := range []string{"session_secret", "account_mode must be", "release_channel must be", "dev is for local use"} {
		assert.Contains(t, err.Error(), want)
	}
}

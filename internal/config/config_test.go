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
  account_mode: landlord
catalogs:
  release_channel: beta
`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "massdriver credential")

	_, err = config.Parse([]byte(`
massdriver:
  organization_id: x
  api_key: y
  account_mode: landlord
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

func TestTenantModeReadsThePaaSAndResolvesTheSkin(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "paas.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`
server:
  session_secret: 0123456789abcdef0123456789abcdef
massdriver:
  account_mode: tenant
  organization_id: rs-test
  api_key: md_test
skin: skins/purple
paas:
  runtimes:
    - {id: ruby, name: Ruby, project: ruby, template: template}
  pinned:
    web: {subdomain: "{{environment_id}}"}
  addons:
    worker: {title: Worker}
    db: {title: Postgres}
  pipeline: {stages: []}
auth:
  providers:
    password: {password: hunter22}
`), 0o600))
	cfg, err := config.Load(path)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dir, "skins/purple"), cfg.Skin)
	assert.Equal(t, "ruby", cfg.PaaS.Runtimes[0].Project)
	assert.Equal(t, []string{"worker", "db"}, []string{cfg.PaaS.Addons[0].ID, cfg.PaaS.Addons[1].ID})
	assert.Equal(t, "{{environment_id}}", cfg.PaaS.Pinned["web"]["subdomain"])
}

func TestCatalogDirs(t *testing.T) {
	const base = `
server:
  session_secret: 0123456789abcdef0123456789abcdef
massdriver:
  organization_id: rs-test
  api_key: md_test
auth:
  providers:
    dev: {login: cory}
`
	dir := t.TempDir()
	tests := []struct {
		name     string
		catalogs string
		want     []string
		wantErr  string
	}{
		{name: "default", want: []string{"catalogs"}},
		{name: "dir only", catalogs: "catalogs: {dir: mine}", want: []string{"mine"}},
		{
			name:     "dirs only, relative to the config",
			catalogs: "catalogs: {dirs: [../aws/catalogs, /abs/catalogs]}",
			want:     []string{filepath.Join(dir, "../aws/catalogs"), "/abs/catalogs"},
		},
		{name: "both", catalogs: "catalogs: {dir: catalogs, dirs: [more]}", want: []string{"catalogs", filepath.Join(dir, "more")}},
		{name: "empty entry", catalogs: `catalogs: {dirs: [""]}`, wantErr: "catalogs.dirs must not have empty entries"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(dir, "rocketsled.yaml")
			require.NoError(t, os.WriteFile(path, []byte(base+tt.catalogs+"\n"), 0o600))
			cfg, err := config.Load(path)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, cfg.Catalogs.All())
		})
	}
}

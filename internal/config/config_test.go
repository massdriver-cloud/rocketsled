package config_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

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
  domains: {component: web, param: subdomain, target_field: url}
  previews:
    presets: {web: Development, db: Development}
    ttl: 72h
  platform:
    project: platform
    environment: main
    domain: fauxroku.dev
    components:
      network: {preset: Development}
      dns: {preset: Development, params: {domain: "{{platform_domain}}"}}
    shares: [network.network, cluster.cluster]
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
	require.NotNil(t, cfg.PaaS.Platform)
	assert.Equal(t, []string{"network", "dns"}, []string{cfg.PaaS.Platform.Components[0].Slug, cfg.PaaS.Platform.Components[1].Slug}, "components keep their deploy order")
	assert.Equal(t, "{{platform_domain}}", cfg.PaaS.Platform.Components[1].Params["domain"])
	assert.Equal(t, []string{"network.network", "cluster.cluster"}, cfg.PaaS.Platform.Shares)
	assert.Equal(t, &config.Previews{Presets: map[string]string{"web": "Development", "db": "Development"}, TTL: 72 * time.Hour}, cfg.PaaS.Previews)
	assert.Equal(t, &config.Domains{Component: "web", Param: "subdomain", TargetField: "url"}, cfg.PaaS.Domains)
}

func TestTenantDomainsNeedEveryKey(t *testing.T) {
	_, err := config.Parse([]byte(`
server: {session_secret: 0123456789abcdef0123456789abcdef}
massdriver: {account_mode: tenant, organization_id: rs-test, api_key: md_test}
paas:
  runtimes: [{id: ruby, project: ruby, template: template}]
  domains: {component: web, param: subdomain}
auth: {providers: {password: {password: hunter22}}}
`))
	require.ErrorContains(t, err, "paas.domains needs component, param and target_field")
}

func TestCatalogInclude(t *testing.T) {
	cfg, err := config.Parse([]byte(`
server:
  session_secret: 0123456789abcdef0123456789abcdef
massdriver:
  organization_id: rs-test
  api_key: md_test
catalogs:
  include: [fauxroku]
auth:
  providers:
    dev: {login: cory}
`))
	require.NoError(t, err)
	assert.Equal(t, []string{"fauxroku"}, cfg.Catalogs.Include)
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
	dir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
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
		{name: "empty include", catalogs: `catalogs: {include: [fauxroku, ""]}`, wantErr: "catalogs.include must not have empty entries"},
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

func TestPipelineIsValidated(t *testing.T) {
	base := `
server:
  session_secret: 0123456789abcdef0123456789abcdef
massdriver:
  account_mode: tenant
  organization_id: rs-test
  api_key: md_test
auth:
  providers:
    password: {password: hunter22}
paas:
  runtimes:
    - {id: ruby, project: ruby, template: template}
  pipeline:
`
	cases := []struct {
		name     string
		pipeline string
		wantErr  string
	}{
		{"valid", `
    stages:
      - {id: staging, presets: {web: Development}}
      - {id: production, name: Production}
    promote: [web]`, ""},
		{"empty id", `
    stages:
      - {name: Staging}`, "stages[0].id must be"},
		{"not slug-safe", `
    stages:
      - {id: Staging}`, "stages[0].id must be"},
		{"duplicate", `
    stages:
      - {id: staging}
      - {id: staging}`, `"staging" is listed twice`},
		{"promote without stages", `
    stages:
      - {id: staging}
    promote: [web]`, "promote needs at least two stages"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := config.Parse([]byte(base + tc.pipeline + "\n"))
			if tc.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErr)
				return
			}
			require.NoError(t, err)
			stages := cfg.PaaS.Pipeline.Stages
			assert.Equal(t, "staging", stages[0].Name, "a stage without a name is called by its id")
			assert.Equal(t, "Production", stages[1].Name)
			assert.Equal(t, map[string]string{"web": "Development"}, stages[0].Presets)
			assert.Equal(t, []string{"web"}, cfg.PaaS.Pipeline.Promote)
		})
	}
}

func TestPlatformLayerValidation(t *testing.T) {
	_, err := config.Parse([]byte(`
server: {session_secret: 0123456789abcdef0123456789abcdef}
massdriver: {account_mode: tenant, organization_id: rs-test, api_key: md_test}
paas:
  runtimes: [{id: ruby, project: ruby, template: template}]
  platform:
    environment: main
    components:
      network: {}
    shares: [network]
auth: {providers: {password: {password: hunter22}}}
`))
	require.Error(t, err)
	for _, want := range []string{"paas.platform needs project and environment", `"network" must be component.field`, "components.network needs a preset"} {
		assert.Contains(t, err.Error(), want)
	}
}

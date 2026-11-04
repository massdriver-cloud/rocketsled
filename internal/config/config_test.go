package config_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/massdriver-cloud/rocketsled/internal/config"
)

const valid = `
server:
  session_secret: 0123456789abcdef0123456789abcdef
massdriver:
  organization_id: acme
  api_key: ${ROCKETSLED_TEST_KEY}
  projects:
    - id: shop
      base_environment: prod
auth:
  admins: [alice, "gitlab:bob"]
  providers:
    github:
      client_id: gh-id
      client_secret: gh-secret
  webhooks:
    secret: hook
`

func TestParseAppliesDefaultsAndExpandsEnv(t *testing.T) {
	t.Setenv("ROCKETSLED_TEST_KEY", "sekrit")

	cfg, err := config.Parse([]byte(valid))
	require.NoError(t, err)

	assert.Equal(t, ":8080", cfg.Server.Addr)
	assert.Equal(t, "https://api.massdriver.cloud", cfg.Massdriver.URL)
	assert.Equal(t, "sekrit", cfg.Massdriver.APIKey)
	assert.Equal(t, "image.tag", cfg.Massdriver.Projects[0].ImageTagParam)
	assert.True(t, cfg.IsAdmin("github", "alice"))
	assert.True(t, cfg.IsAdmin("gitlab", "bob"))
	assert.False(t, cfg.IsAdmin("github", "bob"))
}

func TestParseReportsEveryProblem(t *testing.T) {
	_, err := config.Parse([]byte(`
massdriver:
  projects:
    - id: "Bad Id"
      base_environment: prod
    - id: dup
      base_environment: prod
      image_registry: 123.dkr.ecr.us-west-2.amazonaws.com/x
    - id: dup
`))
	require.Error(t, err)
	msg := err.Error()
	for _, want := range []string{
		"session_secret",
		"organization_id",
		"api_key",
		`"Bad Id" is not a valid project identifier`,
		`"dup" is listed twice`,
		"projects[2].base_environment is required",
		"auth.providers must configure github, gitlab, or dev",
		"image_registry needs builder.docker",
		"webhooks.secret",
	} {
		assert.Contains(t, msg, want)
	}
}

func TestProjectLookup(t *testing.T) {
	t.Setenv("ROCKETSLED_TEST_KEY", "sekrit")
	cfg, err := config.Parse([]byte(valid))
	require.NoError(t, err)

	p, ok := cfg.Project("shop")
	assert.True(t, ok)
	assert.Equal(t, "prod", p.BaseEnvironment)

	_, ok = cfg.Project("nope")
	assert.False(t, ok)
}

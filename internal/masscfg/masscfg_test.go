package masscfg_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/massdriver-cloud/rocketsled/internal/masscfg"
)

func TestLoadProfile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`
version: 1
profiles:
  default:
    organization_id: acme
    api_key: md_default
  citizens:
    organization_id: citizens
    api_key: md_citizens
    url: https://api.example.com/
`), 0o600))

	p, err := masscfg.Load(path, "citizens")
	require.NoError(t, err)
	assert.Equal(t, "citizens", p.OrganizationID)
	assert.Equal(t, "md_citizens", p.APIKey)
	assert.Equal(t, "https://api.example.com", p.URL)

	p, err = masscfg.Load(path, "default")
	require.NoError(t, err)
	assert.Equal(t, masscfg.DefaultURL, p.URL)

	_, err = masscfg.Load(path, "nope")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `profile "nope" not found`)
}

func TestResolveReadsEnv(t *testing.T) {
	t.Setenv(masscfg.PathEnv, "~/somewhere/config.yaml")
	t.Setenv(masscfg.ProfileEnv, "citizens")
	path, profile := masscfg.Resolve()
	home, _ := os.UserHomeDir()
	assert.Equal(t, filepath.Join(home, "somewhere/config.yaml"), path)
	assert.Equal(t, "citizens", profile)

	t.Setenv(masscfg.PathEnv, "")
	t.Setenv(masscfg.ProfileEnv, "")
	path, profile = masscfg.Resolve()
	assert.Equal(t, masscfg.DefaultPath(), path)
	assert.Equal(t, masscfg.DefaultProfile, profile)
}

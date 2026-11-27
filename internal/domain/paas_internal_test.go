package domain

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestOwnerMarkerRoundTrips(t *testing.T) {
	id := "open:0123456789ab:ada"
	marker := ownerMarker("ada", id, "", map[string]string{"web": "Development", "db": "Development"})
	assert.Equal(t, id, ownerOf(marker))
	assert.Equal(t, map[string]string{"web": "Development", "db": "Development"}, picksOf(marker))
	assert.Empty(t, stageOf(marker))
	assert.Empty(t, ownerOf("hand-made staging"))
	assert.Empty(t, ownerOf(""))

	staged := ownerMarker("ada", id, "staging", map[string]string{"web": "Development"})
	assert.Equal(t, "staging", stageOf(staged))
	assert.Equal(t, id, ownerOf(staged))
	assert.Equal(t, "staging", stageOf(withPicks(staged, map[string]string{"web": "Production"})), "rewriting the picks keeps the stage")
}

func TestChangedComparesParamsAndVersion(t *testing.T) {
	env := func(webVersion, apiSize string) *PlatformEnvironment {
		return &PlatformEnvironment{Instances: []PlatformInstance{
			{ComponentSlug: "web", Status: "PROVISIONED", Version: webVersion, DeployedVersion: webVersion, Params: map[string]any{"size": "small"}},
			{ComponentSlug: "api", Status: "PROVISIONED", Version: "1.0.0", DeployedVersion: "1.0.0", Params: map[string]any{"size": apiSize}},
			{ComponentSlug: "db", Status: "PROVISIONED", Version: "1.0.0", DeployedVersion: "1.0.0", Params: map[string]any{"size": "small"}},
		}}
	}
	got := changed(env("1.0.0", "small"), env("1.1.0", "large"), []string{"web", "api"})
	assert.Equal(t, map[string]map[string]any{"web": {"size": "small"}, "api": {"size": "large"}}, got)
	assert.Empty(t, changed(env("1.0.0", "small"), env("1.0.0", "small"), []string{"web", "api", "db"}))

	failed := env("1.0.0", "small")
	failed.Instances[0].Status = "FAILED"
	assert.Contains(t, changed(env("1.0.0", "small"), failed, []string{"web"}), "web", "a promotion whose deploy failed deploys again on retry")
	behind := env("1.0.0", "small")
	behind.Instances[0].DeployedVersion = "0.9.0"
	assert.Contains(t, changed(env("1.0.0", "small"), behind, []string{"web"}), "web", "so does one configured past what is deployed")
}

func TestOpenUserRebuildsFromTheSessionID(t *testing.T) {
	u, ok := openUser("open:0123456789ab:ada.l")
	assert.True(t, ok)
	assert.Equal(t, "ada.l", u.Login)
	assert.Equal(t, ProviderOpen, u.Provider)

	_, ok = openUser("github:42")
	assert.False(t, ok)
	_, ok = openUser("open:abc:<script>")
	assert.False(t, ok)
}

func TestPinnedParamsFillInTheEnvironment(t *testing.T) {
	got := renderPinned(map[string]any{
		"subdomain": "{{environment_id}}",
		"spot":      true,
		"nested":    map[string]any{"name": "{{runtime}}-{{app}}"},
	}, map[string]string{"environment_id": "ruby-hello", "runtime": "ruby", "app": "hello"})
	assert.Equal(t, "ruby-hello", got["subdomain"])
	assert.Equal(t, true, got["spot"])
	assert.Equal(t, "ruby-hello", got["nested"].(map[string]any)["name"])
}

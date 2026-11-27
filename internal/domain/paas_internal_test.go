package domain

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestOwnerMarkerRoundTrips(t *testing.T) {
	id := "open:0123456789ab:ada"
	marker := ownerMarker("ada", id, map[string]string{"web": "Development", "db": "Development"})
	assert.Equal(t, id, ownerOf(marker))
	assert.Equal(t, map[string]string{"web": "Development", "db": "Development"}, picksOf(marker))
	assert.Empty(t, ownerOf("hand-made staging"))
	assert.Empty(t, ownerOf(""))
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

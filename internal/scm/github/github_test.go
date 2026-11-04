package github_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/massdriver-cloud/rocketsled/internal/scm/github"
)

func TestCurrentUserAndEnterpriseURLs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer tok", r.Header.Get("Authorization"))
		if r.URL.Path == "/api/v3/user" {
			_, _ = w.Write([]byte(`{"id":7,"login":"alice","name":"Alice","avatar_url":"https://a"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	p := github.New("id", "secret", srv.URL, srv.Client())

	u, err := p.CurrentUser(context.Background(), "tok")
	require.NoError(t, err)
	assert.Equal(t, "7", u.ProviderUserID)
	assert.Equal(t, "alice", u.Login)
	assert.Contains(t, p.AuthCodeURL("st", "https://sled/auth/github/callback"), srv.URL+"/login/oauth/authorize?")
	assert.Contains(t, p.AuthCodeURL("st", "https://sled/auth/github/callback"), "scope=read%3Auser+user%3Aemail")
}

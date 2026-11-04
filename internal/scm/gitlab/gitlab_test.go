package gitlab_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/massdriver-cloud/rocketsled/internal/scm/gitlab"
)

func TestCurrentUser(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v4/user" {
			_, _ = w.Write([]byte(`{"id":3,"username":"bob","name":"Bob"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	p := gitlab.New("id", "secret", srv.URL, srv.Client())

	u, err := p.CurrentUser(context.Background(), "tok")
	require.NoError(t, err)
	assert.Equal(t, "bob", u.Login)
	assert.Contains(t, p.AuthCodeURL("st", "https://sled/auth/gitlab/callback"), srv.URL+"/oauth/authorize?")
}

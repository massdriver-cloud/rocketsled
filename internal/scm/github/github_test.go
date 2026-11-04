package github_test

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/massdriver-cloud/rocketsled/internal/domain"
	"github.com/massdriver-cloud/rocketsled/internal/scm/github"
)

func sign(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func TestParsePushVerifiesSignatureAndDecodes(t *testing.T) {
	p := github.New("id", "secret", "", nil)
	body := []byte(`{"ref":"refs/heads/feature/x","after":"abc123","deleted":false,"repository":{"full_name":"acme/shop"}}`)

	req := httptest.NewRequest(http.MethodPost, "/hooks/github", bytes.NewReader(body))
	req.Header.Set("X-GitHub-Event", "push")
	req.Header.Set("X-Hub-Signature-256", sign(body, "hook"))

	ev, err := p.ParsePush(req, "hook")
	require.NoError(t, err)
	assert.Equal(t, &domain.PushEvent{Provider: domain.ProviderGitHub, RepoFullName: "acme/shop", Branch: "feature/x", SHA: "abc123"}, ev)

	bad := httptest.NewRequest(http.MethodPost, "/hooks/github", bytes.NewReader(body))
	bad.Header.Set("X-GitHub-Event", "push")
	bad.Header.Set("X-Hub-Signature-256", sign(body, "wrong"))
	_, err = p.ParsePush(bad, "hook")
	require.ErrorIs(t, err, domain.ErrForbidden)

	ping := httptest.NewRequest(http.MethodPost, "/hooks/github", bytes.NewReader(body))
	ping.Header.Set("X-GitHub-Event", "ping")
	ping.Header.Set("X-Hub-Signature-256", sign(body, "hook"))
	ev, err = p.ParsePush(ping, "hook")
	require.NoError(t, err)
	assert.Nil(t, ev)
}

func TestAPIEndpointsForEnterprise(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.RequestURI())
		assert.Equal(t, "Bearer tok", r.Header.Get("Authorization"))
		switch {
		case r.URL.Path == "/api/v3/user":
			_, _ = w.Write([]byte(`{"id":7,"login":"alice","name":"Alice","avatar_url":"https://a"}`))
		case r.URL.Path == "/api/v3/repos/acme/shop":
			_, _ = w.Write([]byte(`{"id":42,"full_name":"acme/shop","html_url":"https://ghe/acme/shop","default_branch":"main","private":true}`))
		case r.URL.Path == "/api/v3/repos/acme/shop/commits/feature/x":
			_, _ = w.Write([]byte(`{"sha":"deadbeef"}`))
		case r.URL.Path == "/api/v3/repos/acme/shop/hooks" && r.Method == http.MethodPost:
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":99}`))
		case r.URL.Path == "/api/v3/repos/acme/shop/hooks/99" && r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		case r.URL.Path == "/api/v3/user/repos":
			_, _ = w.Write([]byte(`[{"id":1,"full_name":"acme/a","archived":true},{"id":2,"full_name":"acme/b","default_branch":"trunk"}]`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	p := github.New("id", "secret", srv.URL, srv.Client())
	ctx := context.Background()

	u, err := p.CurrentUser(ctx, "tok")
	require.NoError(t, err)
	assert.Equal(t, "7", u.ProviderUserID)
	assert.Equal(t, "alice", u.Login)

	repo, err := p.GetRepo(ctx, "tok", "acme/shop")
	require.NoError(t, err)
	assert.Equal(t, domain.Repo{Provider: domain.ProviderGitHub, ExternalID: "42", FullName: "acme/shop", URL: "https://ghe/acme/shop", DefaultBranch: "main", Private: true}, *repo)

	sha, err := p.BranchHead(ctx, "tok", "acme/shop", "feature/x")
	require.NoError(t, err)
	assert.Equal(t, "deadbeef", sha)

	id, err := p.CreateWebhook(ctx, "tok", "acme/shop", "https://sled/hooks/github", "hook")
	require.NoError(t, err)
	assert.Equal(t, "99", id)
	require.NoError(t, p.DeleteWebhook(ctx, "tok", "acme/shop", "99"))

	repos, err := p.ListRepos(ctx, "tok")
	require.NoError(t, err)
	require.Len(t, repos, 1, "archived repositories are skipped")
	assert.Equal(t, "acme/b", repos[0].FullName)

	_, err = p.GetRepo(ctx, "tok", "acme/missing")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "HTTP 404")
	assert.Contains(t, p.AuthCodeURL("st", "https://sled/auth/github/callback"), srv.URL+"/login/oauth/authorize?")
	assert.Equal(t, "https://x-access-token:tok@ghe/acme/shop.git", p.CloneURL(*repo, "tok"))
}

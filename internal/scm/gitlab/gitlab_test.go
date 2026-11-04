package gitlab_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/massdriver-cloud/rocketsled/internal/domain"
	"github.com/massdriver-cloud/rocketsled/internal/scm/gitlab"
)

func TestParsePushChecksTokenAndDecodes(t *testing.T) {
	p := gitlab.New("id", "secret", "", nil)
	body := []byte(`{"ref":"refs/heads/main","after":"abc123","project":{"path_with_namespace":"acme/shop"}}`)

	req := httptest.NewRequest(http.MethodPost, "/hooks/gitlab", bytes.NewReader(body))
	req.Header.Set("X-Gitlab-Event", "Push Hook")
	req.Header.Set("X-Gitlab-Token", "hook")
	ev, err := p.ParsePush(req, "hook")
	require.NoError(t, err)
	assert.Equal(t, &domain.PushEvent{Provider: domain.ProviderGitLab, RepoFullName: "acme/shop", Branch: "main", SHA: "abc123"}, ev)

	deleted := []byte(`{"ref":"refs/heads/gone","after":"0000000000000000000000000000000000000000","project":{"path_with_namespace":"acme/shop"}}`)
	req = httptest.NewRequest(http.MethodPost, "/hooks/gitlab", bytes.NewReader(deleted))
	req.Header.Set("X-Gitlab-Event", "Push Hook")
	req.Header.Set("X-Gitlab-Token", "hook")
	ev, err = p.ParsePush(req, "hook")
	require.NoError(t, err)
	assert.True(t, ev.Deleted)

	req = httptest.NewRequest(http.MethodPost, "/hooks/gitlab", bytes.NewReader(body))
	req.Header.Set("X-Gitlab-Event", "Push Hook")
	req.Header.Set("X-Gitlab-Token", "nope")
	_, err = p.ParsePush(req, "hook")
	require.ErrorIs(t, err, domain.ErrForbidden)

	req = httptest.NewRequest(http.MethodPost, "/hooks/gitlab", bytes.NewReader(body))
	req.Header.Set("X-Gitlab-Event", "Merge Request Hook")
	req.Header.Set("X-Gitlab-Token", "hook")
	ev, err = p.ParsePush(req, "hook")
	require.NoError(t, err)
	assert.Nil(t, ev)
}

func TestAPIPathsEscapeProjectPaths(t *testing.T) {
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.RawPath+r.URL.RawQuery)
		switch {
		case r.URL.RawPath == "/api/v4/projects/acme%2Fshop":
			_, _ = w.Write([]byte(`{"id":5,"path_with_namespace":"acme/shop","web_url":"https://gl/acme/shop","default_branch":"main","visibility":"private"}`))
		case r.URL.RawPath == "/api/v4/projects/acme%2Fshop/repository/branches/feature%2Fx":
			_, _ = w.Write([]byte(`{"commit":{"id":"cafebabe"}}`))
		case r.URL.RawPath == "/api/v4/projects/acme%2Fshop/hooks" && r.Method == http.MethodPost:
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":12}`))
		case r.URL.Path == "/api/v4/user":
			_, _ = w.Write([]byte(`{"id":3,"username":"bob","name":"Bob"}`))
		case r.URL.Path == "/api/v4/projects":
			_, _ = w.Write([]byte(`[{"id":1,"path_with_namespace":"acme/a","visibility":"public","archived":true},{"id":2,"path_with_namespace":"acme/b","visibility":"public"}]`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	p := gitlab.New("id", "secret", srv.URL, srv.Client())
	ctx := context.Background()

	u, err := p.CurrentUser(ctx, "tok")
	require.NoError(t, err)
	assert.Equal(t, "bob", u.Login)

	repo, err := p.GetRepo(ctx, "tok", "acme/shop")
	require.NoError(t, err)
	assert.Equal(t, "5", repo.ExternalID)
	assert.True(t, repo.Private)

	assert.Equal(t, "https://oauth2:tok@gl/acme/shop.git", p.CloneURL(*repo, "tok"))

	sha, err := p.BranchHead(ctx, "tok", "acme/shop", "feature/x")
	require.NoError(t, err)
	assert.Equal(t, "cafebabe", sha)

	id, err := p.CreateWebhook(ctx, "tok", "acme/shop", "https://sled/hooks/gitlab", "hook")
	require.NoError(t, err)
	assert.Equal(t, "12", id)

	repos, err := p.ListRepos(ctx, "tok")
	require.NoError(t, err)
	require.Len(t, repos, 1)
	assert.False(t, repos[0].Private)
	assert.Contains(t, seen, "GET /api/v4/projects/acme%2Fshop")
}

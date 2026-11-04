package dev_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/massdriver-cloud/rocketsled/internal/domain"
	"github.com/massdriver-cloud/rocketsled/internal/scm/dev"
)

func TestSignInShortCircuits(t *testing.T) {
	p := dev.New("cory", nil, "", nil)

	assert.Equal(t, "http://localhost:8080/auth/dev/callback?code=dev&state=abc", p.AuthCodeURL("abc", "http://localhost:8080/auth/dev/callback"))

	tok, err := p.Exchange(context.Background(), "dev", "")
	require.NoError(t, err)
	u, err := p.CurrentUser(context.Background(), tok)
	require.NoError(t, err)
	assert.Equal(t, "cory", u.Login)
	assert.Equal(t, domain.ProviderDev, u.Provider)

	_, err = p.Exchange(context.Background(), "forged", "")
	require.ErrorIs(t, err, domain.ErrForbidden)
}

func TestReposFallBackWhenGitHubIsUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) }))
	defer srv.Close()
	p := dev.New("cory", []string{"acme/shop"}, srv.URL, srv.Client())

	repos, err := p.ListRepos(context.Background(), "")
	require.NoError(t, err)
	require.Len(t, repos, 1)
	assert.Equal(t, "main", repos[0].DefaultBranch)

	sha, err := p.BranchHead(context.Background(), "", "acme/shop", "main")
	require.NoError(t, err)
	assert.Len(t, sha, 40)

	_, err = p.GetRepo(context.Background(), "", "nope")
	require.Error(t, err)
}

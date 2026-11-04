package dev_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/massdriver-cloud/rocketsled/internal/domain"
	"github.com/massdriver-cloud/rocketsled/internal/scm/dev"
)

func TestSignInShortCircuits(t *testing.T) {
	p := dev.New("cory")

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

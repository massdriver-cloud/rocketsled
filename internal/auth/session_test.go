package auth_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/massdriver-cloud/rocketsled/internal/auth"
)

func TestIssueAndVerify(t *testing.T) {
	s := auth.New("secret-secret-secret-secret-1234", time.Hour, false)

	tok := s.Issue("github:1")
	sub, err := s.Verify(tok)
	require.NoError(t, err)
	assert.Equal(t, "github:1", sub)

	_, err = s.Verify(tok + "x")
	require.ErrorIs(t, err, auth.ErrInvalidSession)

	other := auth.New("a-different-secret-a-different-1", time.Hour, false)
	_, err = other.Verify(tok)
	require.ErrorIs(t, err, auth.ErrInvalidSession)

	expired := auth.New("secret-secret-secret-secret-1234", -time.Minute, false)
	_, err = expired.Verify(expired.Issue("github:1"))
	require.ErrorIs(t, err, auth.ErrInvalidSession)
}

func TestCookieRoundTrip(t *testing.T) {
	s := auth.New("secret-secret-secret-secret-1234", time.Hour, true)
	rec := httptest.NewRecorder()
	s.SetSession(rec, "gitlab:9")
	cookie := rec.Result().Cookies()[0]
	assert.Equal(t, auth.SessionCookie, cookie.Name)
	assert.True(t, cookie.HttpOnly)
	assert.True(t, cookie.Secure)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	req.AddCookie(cookie)
	sub, err := s.UserFromRequest(req)
	require.NoError(t, err)
	assert.Equal(t, "gitlab:9", sub)

	_, err = s.UserFromRequest(httptest.NewRequest(http.MethodGet, "/", nil))
	require.ErrorIs(t, err, auth.ErrInvalidSession)
}

func TestOAuthStateBindsNonceAndNext(t *testing.T) {
	s := auth.New("secret-secret-secret-secret-1234", time.Hour, false)
	rec := httptest.NewRecorder()
	state := s.BeginOAuth(rec, "/stacks/new")
	cookie := rec.Result().Cookies()[0]

	req := httptest.NewRequest(http.MethodGet, "/auth/github/callback?state="+state, nil)
	req.AddCookie(cookie)
	got, err := s.FinishOAuth(httptest.NewRecorder(), req, state)
	require.NoError(t, err)
	assert.Equal(t, "/stacks/new", got.Next)

	req = httptest.NewRequest(http.MethodGet, "/auth/github/callback?state=forged", nil)
	req.AddCookie(cookie)
	_, err = s.FinishOAuth(httptest.NewRecorder(), req, "forged")
	require.ErrorIs(t, err, auth.ErrInvalidSession)

	rec = httptest.NewRecorder()
	s.BeginOAuth(rec, "https://evil.example/phish")
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(rec.Result().Cookies()[0])
	got, err = s.FinishOAuth(httptest.NewRecorder(), req, rec.Result().Cookies()[0].Value[:0]+stateOf(t, s, rec))
	require.NoError(t, err)
	assert.Equal(t, "/", got.Next, "open redirects are refused")
}

// stateOf recovers the nonce embedded in the state cookie so the test can present it.
func stateOf(t *testing.T, s *auth.Sessions, rec *httptest.ResponseRecorder) string {
	t.Helper()
	payload, err := s.Verify(rec.Result().Cookies()[0].Value)
	require.NoError(t, err)
	for i := range payload {
		if payload[i] == '|' {
			return payload[:i]
		}
	}
	t.Fatal("no nonce in state cookie")
	return ""
}

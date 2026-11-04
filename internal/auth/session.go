// Package auth issues and verifies the signed cookies that keep a user logged in
// and protect the OAuth round trip.
package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	SessionCookie = "rocketsled_session"
	StateCookie   = "rocketsled_oauth"
)

var ErrInvalidSession = errors.New("invalid session")

// Sessions signs opaque tokens with HMAC-SHA256.
type Sessions struct {
	key    []byte
	ttl    time.Duration
	secure bool
	now    func() time.Time
}

// New builds a Sessions. secure marks cookies Secure (set when serving over HTTPS).
func New(secret string, ttl time.Duration, secure bool) *Sessions {
	sum := sha256.Sum256([]byte("rocketsled-session:" + secret))
	return &Sessions{key: sum[:], ttl: ttl, secure: secure, now: time.Now}
}

// Issue creates a token binding subject until expiry.
func (s *Sessions) Issue(subject string) string {
	return s.issue(subject, s.now().Add(s.ttl))
}

func (s *Sessions) issue(subject string, exp time.Time) string {
	payload := base64.RawURLEncoding.EncodeToString([]byte(subject)) + "." + strconv.FormatInt(exp.Unix(), 10)
	return payload + "." + s.sign(payload)
}

// Verify returns the subject for a valid, unexpired token.
func (s *Sessions) Verify(token string) (string, error) {
	i := strings.LastIndex(token, ".")
	if i < 0 {
		return "", ErrInvalidSession
	}
	payload, sig := token[:i], token[i+1:]
	if !hmac.Equal([]byte(sig), []byte(s.sign(payload))) {
		return "", ErrInvalidSession
	}
	subjectB64, expStr, ok := strings.Cut(payload, ".")
	if !ok {
		return "", ErrInvalidSession
	}
	exp, err := strconv.ParseInt(expStr, 10, 64)
	if err != nil || s.now().Unix() > exp {
		return "", fmt.Errorf("%w: expired", ErrInvalidSession)
	}
	subject, err := base64.RawURLEncoding.DecodeString(subjectB64)
	if err != nil {
		return "", ErrInvalidSession
	}
	return string(subject), nil
}

func (s *Sessions) sign(payload string) string {
	mac := hmac.New(sha256.New, s.key)
	mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// SetSession writes the login cookie.
func (s *Sessions) SetSession(w http.ResponseWriter, userID string) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookie,
		Value:    s.Issue(userID),
		Path:     "/",
		HttpOnly: true,
		Secure:   s.secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(s.ttl.Seconds()),
	})
}

// ClearSession logs the browser out.
func (s *Sessions) ClearSession(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: SessionCookie, Value: "", Path: "/", HttpOnly: true, Secure: s.secure, SameSite: http.SameSiteLaxMode, MaxAge: -1})
}

// UserFromRequest reads and verifies the login cookie.
func (s *Sessions) UserFromRequest(r *http.Request) (string, error) {
	c, err := r.Cookie(SessionCookie)
	if err != nil {
		return "", ErrInvalidSession
	}
	return s.Verify(c.Value)
}

// OAuthState is the CSRF token plus where to send the user afterwards.
type OAuthState struct {
	Nonce string
	Next  string
}

// BeginOAuth stores a short-lived state cookie and returns the state string to
// pass to the provider.
func (s *Sessions) BeginOAuth(w http.ResponseWriter, next string) string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		panic("crypto/rand unavailable: " + err.Error())
	}
	nonce := base64.RawURLEncoding.EncodeToString(buf)
	if next == "" || !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") {
		next = "/"
	}
	payload := nonce + "|" + next
	http.SetCookie(w, &http.Cookie{
		Name:     StateCookie,
		Value:    s.issue(payload, s.now().Add(10*time.Minute)),
		Path:     "/",
		HttpOnly: true,
		Secure:   s.secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   600,
	})
	return nonce
}

// FinishOAuth validates the provider's state against the cookie and clears it.
func (s *Sessions) FinishOAuth(w http.ResponseWriter, r *http.Request, state string) (OAuthState, error) {
	http.SetCookie(w, &http.Cookie{Name: StateCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: s.secure, SameSite: http.SameSiteLaxMode})
	c, err := r.Cookie(StateCookie)
	if err != nil {
		return OAuthState{}, fmt.Errorf("%w: missing state cookie", ErrInvalidSession)
	}
	payload, err := s.Verify(c.Value)
	if err != nil {
		return OAuthState{}, err
	}
	nonce, next, _ := strings.Cut(payload, "|")
	if state == "" || !hmac.Equal([]byte(nonce), []byte(state)) {
		return OAuthState{}, fmt.Errorf("%w: state mismatch", ErrInvalidSession)
	}
	return OAuthState{Nonce: nonce, Next: next}, nil
}

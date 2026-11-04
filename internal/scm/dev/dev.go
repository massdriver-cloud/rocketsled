// Package dev is a local stand-in for GitHub or GitLab sign-in. It signs in a fixed
// user without an OAuth application. It is for laptops, not for anything reachable
// from the internet.
package dev

import (
	"context"
	"fmt"
	"net/url"

	"github.com/massdriver-cloud/rocketsled/internal/domain"
)

// Provider is the stub.
type Provider struct {
	user domain.User
}

var _ domain.SCM = (*Provider)(nil)

// New builds the stub; login names the signed-in user.
func New(login string) *Provider {
	if login == "" {
		login = "dev"
	}
	return &Provider{user: domain.User{Provider: domain.ProviderDev, ProviderUserID: login, Login: login, Name: login}}
}

func (p *Provider) Provider() domain.Provider { return domain.ProviderDev }

// AuthCodeURL sends the browser straight back to the callback.
func (p *Provider) AuthCodeURL(state, redirectURL string) string {
	q := url.Values{"code": {"dev"}, "state": {state}}
	return redirectURL + "?" + q.Encode()
}

func (p *Provider) Exchange(_ context.Context, code, _ string) (string, error) {
	if code != "dev" {
		return "", fmt.Errorf("%w: unexpected code", domain.ErrForbidden)
	}
	return "dev-token", nil
}

func (p *Provider) CurrentUser(context.Context, string) (*domain.User, error) {
	u := p.user
	return &u, nil
}

// Package github implements sign-in with GitHub.com or GitHub Enterprise.
package github

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"golang.org/x/oauth2"

	"github.com/massdriver-cloud/rocketsled/internal/domain"
	"github.com/massdriver-cloud/rocketsled/internal/scm"
)

// Provider talks to one GitHub host.
type Provider struct {
	oauth  oauth2.Config
	apiURL string
	http   *http.Client
}

var _ domain.SCM = (*Provider)(nil)

// New configures GitHub. baseURL is "https://github.com" unless self-hosted.
func New(clientID, clientSecret, baseURL string, httpClient *http.Client) *Provider {
	if baseURL == "" {
		baseURL = "https://github.com"
	}
	baseURL = strings.TrimRight(baseURL, "/")
	apiURL := "https://api.github.com"
	if baseURL != "https://github.com" {
		apiURL = baseURL + "/api/v3"
	}
	if httpClient == nil {
		httpClient = scm.DefaultHTTPClient()
	}
	return &Provider{
		oauth: oauth2.Config{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			Scopes:       []string{"read:user", "user:email"},
			Endpoint: oauth2.Endpoint{
				AuthURL:  baseURL + "/login/oauth/authorize",
				TokenURL: baseURL + "/login/oauth/access_token",
			},
		},
		apiURL: apiURL,
		http:   httpClient,
	}
}

func (p *Provider) Provider() domain.Provider { return domain.ProviderGitHub }

func (p *Provider) AuthCodeURL(state, redirectURL string) string {
	cfg := p.oauth
	cfg.RedirectURL = redirectURL
	return cfg.AuthCodeURL(state)
}

func (p *Provider) Exchange(ctx context.Context, code, redirectURL string) (string, error) {
	cfg := p.oauth
	cfg.RedirectURL = redirectURL
	tok, err := cfg.Exchange(context.WithValue(ctx, oauth2.HTTPClient, p.http), code)
	if err != nil {
		return "", err
	}
	return tok.AccessToken, nil
}

func (p *Provider) CurrentUser(ctx context.Context, token string) (*domain.User, error) {
	var u struct {
		ID        int64  `json:"id"`
		Login     string `json:"login"`
		Name      string `json:"name"`
		Email     string `json:"email"`
		AvatarURL string `json:"avatar_url"`
	}
	if err := scm.GetJSON(ctx, p.http, p.apiURL+"/user", token, &u, map[string]string{"Accept": "application/vnd.github+json"}); err != nil {
		return nil, err
	}
	return &domain.User{
		Provider:       domain.ProviderGitHub,
		ProviderUserID: strconv.FormatInt(u.ID, 10),
		Login:          u.Login,
		Name:           u.Name,
		Email:          u.Email,
		AvatarURL:      u.AvatarURL,
	}, nil
}

// Package gitlab implements sign-in with GitLab.com or self-managed GitLab.
package gitlab

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"golang.org/x/oauth2"

	"github.com/massdriver-cloud/rocketsled/internal/domain"
	"github.com/massdriver-cloud/rocketsled/internal/scm"
)

// Provider talks to one GitLab host.
type Provider struct {
	oauth  oauth2.Config
	apiURL string
	http   *http.Client
}

var _ domain.SCM = (*Provider)(nil)

// New configures GitLab. baseURL is "https://gitlab.com" unless self-managed.
func New(clientID, clientSecret, baseURL string, httpClient *http.Client) *Provider {
	if baseURL == "" {
		baseURL = "https://gitlab.com"
	}
	baseURL = strings.TrimRight(baseURL, "/")
	if httpClient == nil {
		httpClient = scm.DefaultHTTPClient()
	}
	return &Provider{
		oauth: oauth2.Config{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			Scopes:       []string{"read_user"},
			Endpoint: oauth2.Endpoint{
				AuthURL:  baseURL + "/oauth/authorize",
				TokenURL: baseURL + "/oauth/token",
			},
		},
		apiURL: baseURL + "/api/v4",
		http:   httpClient,
	}
}

func (p *Provider) Provider() domain.Provider { return domain.ProviderGitLab }

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
		Username  string `json:"username"`
		Name      string `json:"name"`
		Email     string `json:"email"`
		AvatarURL string `json:"avatar_url"`
	}
	if err := scm.GetJSON(ctx, p.http, p.apiURL+"/user", token, &u, nil); err != nil {
		return nil, err
	}
	return &domain.User{
		Provider:       domain.ProviderGitLab,
		ProviderUserID: strconv.FormatInt(u.ID, 10),
		Login:          u.Username,
		Name:           u.Name,
		Email:          u.Email,
		AvatarURL:      u.AvatarURL,
	}, nil
}

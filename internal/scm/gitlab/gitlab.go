// Package gitlab implements domain.SCM for GitLab.com and self-managed GitLab.
package gitlab

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
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
			Scopes:       []string{"read_user", "api"},
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

type glUser struct {
	ID        int64  `json:"id"`
	Username  string `json:"username"`
	Name      string `json:"name"`
	Email     string `json:"email"`
	AvatarURL string `json:"avatar_url"`
}

func (p *Provider) CurrentUser(ctx context.Context, token string) (*domain.User, error) {
	var u glUser
	if err := p.get(ctx, token, "/user", &u); err != nil {
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

type glProject struct {
	ID                int64  `json:"id"`
	PathWithNamespace string `json:"path_with_namespace"`
	WebURL            string `json:"web_url"`
	DefaultBranch     string `json:"default_branch"`
	Visibility        string `json:"visibility"`
	Archived          bool   `json:"archived"`
}

func (r glProject) toDomain() domain.Repo {
	return domain.Repo{
		Provider:      domain.ProviderGitLab,
		ExternalID:    strconv.FormatInt(r.ID, 10),
		FullName:      r.PathWithNamespace,
		URL:           r.WebURL,
		DefaultBranch: r.DefaultBranch,
		Private:       r.Visibility != "public",
	}
}

func (p *Provider) ListRepos(ctx context.Context, token string) ([]domain.Repo, error) {
	var out []domain.Repo
	for page := 1; page <= 5; page++ {
		var projects []glProject
		path := fmt.Sprintf("/projects?membership=true&simple=false&order_by=last_activity_at&per_page=100&page=%d", page)
		if err := p.get(ctx, token, path, &projects); err != nil {
			return nil, err
		}
		for _, r := range projects {
			if r.Archived {
				continue
			}
			out = append(out, r.toDomain())
		}
		if len(projects) < 100 {
			break
		}
	}
	return out, nil
}

func (p *Provider) GetRepo(ctx context.Context, token, fullName string) (*domain.Repo, error) {
	var r glProject
	if err := p.get(ctx, token, "/projects/"+url.PathEscape(fullName), &r); err != nil {
		return nil, err
	}
	repo := r.toDomain()
	return &repo, nil
}

func (p *Provider) BranchHead(ctx context.Context, token, fullName, branch string) (string, error) {
	var out struct {
		Commit struct {
			ID string `json:"id"`
		} `json:"commit"`
	}
	path := "/projects/" + url.PathEscape(fullName) + "/repository/branches/" + url.PathEscape(branch)
	if err := p.get(ctx, token, path, &out); err != nil {
		return "", err
	}
	return out.Commit.ID, nil
}

func (p *Provider) CreateWebhook(ctx context.Context, token, fullName, callbackURL, secret string) (string, error) {
	body := map[string]any{
		"url":                     callbackURL,
		"push_events":             true,
		"token":                   secret,
		"enable_ssl_verification": true,
	}
	var out struct {
		ID int64 `json:"id"`
	}
	if err := scm.DoJSON(ctx, p.http, http.MethodPost, p.apiURL+"/projects/"+url.PathEscape(fullName)+"/hooks", token, body, &out, nil); err != nil {
		return "", err
	}
	return strconv.FormatInt(out.ID, 10), nil
}

func (p *Provider) DeleteWebhook(ctx context.Context, token, fullName, hookID string) error {
	return scm.DoJSON(ctx, p.http, http.MethodDelete, p.apiURL+"/projects/"+url.PathEscape(fullName)+"/hooks/"+hookID, token, nil, nil, nil)
}

type pushPayload struct {
	Ref     string `json:"ref"`
	After   string `json:"after"`
	Project struct {
		PathWithNamespace string `json:"path_with_namespace"`
	} `json:"project"`
}

const zeroSHA = "0000000000000000000000000000000000000000"

// ParsePush checks X-Gitlab-Token and decodes a Push Hook.
func (p *Provider) ParsePush(r *http.Request, secret string) (*domain.PushEvent, error) {
	if subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Gitlab-Token")), []byte(secret)) != 1 {
		return nil, fmt.Errorf("%w: bad token", domain.ErrForbidden)
	}
	if r.Header.Get("X-Gitlab-Event") != "Push Hook" {
		return nil, nil
	}
	body, err := scm.ReadBody(r)
	if err != nil {
		return nil, err
	}
	var payload pushPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("decode push: %w", err)
	}
	branch, ok := strings.CutPrefix(payload.Ref, "refs/heads/")
	if !ok {
		return nil, nil
	}
	return &domain.PushEvent{
		Provider:     domain.ProviderGitLab,
		RepoFullName: payload.Project.PathWithNamespace,
		Branch:       branch,
		SHA:          payload.After,
		Deleted:      payload.After == zeroSHA,
	}, nil
}

// CloneURL embeds the OAuth token the way GitLab expects for HTTPS clones.
func (p *Provider) CloneURL(repo domain.Repo, token string) string {
	u, err := url.Parse(repo.URL)
	if err != nil || u.Host == "" {
		u, _ = url.Parse(strings.TrimSuffix(p.apiURL, "/api/v4") + "/" + repo.FullName)
	}
	if token != "" {
		u.User = url.UserPassword("oauth2", token)
	}
	if !strings.HasSuffix(u.Path, ".git") {
		u.Path += ".git"
	}
	return u.String()
}

func (p *Provider) get(ctx context.Context, token, path string, out any) error {
	return scm.DoJSON(ctx, p.http, http.MethodGet, p.apiURL+path, token, nil, out, nil)
}

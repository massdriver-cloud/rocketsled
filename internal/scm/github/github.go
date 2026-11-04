// Package github implements domain.SCM for GitHub.com and GitHub Enterprise.
package github

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
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
			Scopes:       []string{"read:user", "user:email", "repo"},
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

type ghUser struct {
	ID        int64  `json:"id"`
	Login     string `json:"login"`
	Name      string `json:"name"`
	Email     string `json:"email"`
	AvatarURL string `json:"avatar_url"`
}

func (p *Provider) CurrentUser(ctx context.Context, token string) (*domain.User, error) {
	var u ghUser
	if err := p.get(ctx, token, "/user", &u); err != nil {
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

type ghRepo struct {
	ID            int64  `json:"id"`
	FullName      string `json:"full_name"`
	HTMLURL       string `json:"html_url"`
	DefaultBranch string `json:"default_branch"`
	Private       bool   `json:"private"`
	Archived      bool   `json:"archived"`
}

func (r ghRepo) toDomain() domain.Repo {
	return domain.Repo{
		Provider:      domain.ProviderGitHub,
		ExternalID:    strconv.FormatInt(r.ID, 10),
		FullName:      r.FullName,
		URL:           r.HTMLURL,
		DefaultBranch: r.DefaultBranch,
		Private:       r.Private,
	}
}

func (p *Provider) ListRepos(ctx context.Context, token string) ([]domain.Repo, error) {
	var out []domain.Repo
	for page := 1; page <= 5; page++ {
		var repos []ghRepo
		path := fmt.Sprintf("/user/repos?per_page=100&sort=pushed&affiliation=owner,collaborator,organization_member&page=%d", page)
		if err := p.get(ctx, token, path, &repos); err != nil {
			return nil, err
		}
		for _, r := range repos {
			if r.Archived {
				continue
			}
			out = append(out, r.toDomain())
		}
		if len(repos) < 100 {
			break
		}
	}
	return out, nil
}

func (p *Provider) GetRepo(ctx context.Context, token, fullName string) (*domain.Repo, error) {
	var r ghRepo
	if err := p.get(ctx, token, "/repos/"+fullName, &r); err != nil {
		return nil, err
	}
	repo := r.toDomain()
	return &repo, nil
}

func (p *Provider) BranchHead(ctx context.Context, token, fullName, branch string) (string, error) {
	var out struct {
		SHA string `json:"sha"`
	}
	if err := p.get(ctx, token, "/repos/"+fullName+"/commits/"+url.PathEscape(branch), &out); err != nil {
		return "", err
	}
	return out.SHA, nil
}

func (p *Provider) CreateWebhook(ctx context.Context, token, fullName, callbackURL, secret string) (string, error) {
	body := map[string]any{
		"name":   "web",
		"active": true,
		"events": []string{"push"},
		"config": map[string]any{
			"url":          callbackURL,
			"content_type": "json",
			"secret":       secret,
			"insecure_ssl": "0",
		},
	}
	var out struct {
		ID int64 `json:"id"`
	}
	if err := scm.DoJSON(ctx, p.http, http.MethodPost, p.apiURL+"/repos/"+fullName+"/hooks", token, body, &out, nil); err != nil {
		return "", err
	}
	return strconv.FormatInt(out.ID, 10), nil
}

func (p *Provider) DeleteWebhook(ctx context.Context, token, fullName, hookID string) error {
	return scm.DoJSON(ctx, p.http, http.MethodDelete, p.apiURL+"/repos/"+fullName+"/hooks/"+hookID, token, nil, nil, nil)
}

type pushPayload struct {
	Ref        string `json:"ref"`
	After      string `json:"after"`
	Deleted    bool   `json:"deleted"`
	Repository struct {
		FullName string `json:"full_name"`
	} `json:"repository"`
}

// ParsePush verifies X-Hub-Signature-256 and decodes a push event.
func (p *Provider) ParsePush(r *http.Request, secret string) (*domain.PushEvent, error) {
	body, err := scm.ReadBody(r)
	if err != nil {
		return nil, err
	}
	if !ValidSignature(body, r.Header.Get("X-Hub-Signature-256"), secret) {
		return nil, fmt.Errorf("%w: bad signature", domain.ErrForbidden)
	}
	if r.Header.Get("X-GitHub-Event") != "push" {
		return nil, nil
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
		Provider:     domain.ProviderGitHub,
		RepoFullName: payload.Repository.FullName,
		Branch:       branch,
		SHA:          payload.After,
		Deleted:      payload.Deleted,
	}, nil
}

// CloneURL embeds the OAuth token the way GitHub expects for HTTPS clones.
func (p *Provider) CloneURL(repo domain.Repo, token string) string {
	u, err := url.Parse(repo.URL)
	if err != nil || u.Host == "" {
		u = &url.URL{Scheme: "https", Host: "github.com", Path: "/" + repo.FullName}
	}
	if token != "" {
		u.User = url.UserPassword("x-access-token", token)
	}
	if !strings.HasSuffix(u.Path, ".git") {
		u.Path += ".git"
	}
	return u.String()
}

// ValidSignature checks GitHub's HMAC-SHA256 delivery signature.
func ValidSignature(body []byte, header, secret string) bool {
	sig, ok := strings.CutPrefix(header, "sha256=")
	if !ok {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	want := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(sig), []byte(want))
}

func (p *Provider) get(ctx context.Context, token, path string, out any) error {
	return scm.DoJSON(ctx, p.http, http.MethodGet, p.apiURL+path, token, nil, out, map[string]string{
		"Accept": "application/vnd.github+json",
	})
}

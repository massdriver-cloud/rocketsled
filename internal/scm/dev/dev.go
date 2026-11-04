// Package dev is a local stand-in for GitHub or GitLab sign-in. It signs in a fixed
// user without an OAuth application and reads public repositories through the
// unauthenticated GitHub API. It is for laptops, not for anything reachable from
// the internet.
package dev

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/massdriver-cloud/rocketsled/internal/domain"
	"github.com/massdriver-cloud/rocketsled/internal/scm"
	"github.com/massdriver-cloud/rocketsled/internal/scm/github"
)

// Provider is the stub.
type Provider struct {
	user   domain.User
	repos  []string
	apiURL string
	http   *http.Client
}

var _ domain.SCM = (*Provider)(nil)

// New builds the stub. login names the signed-in user; repos lists public GitHub
// repositories (owner/name) offered for linking; apiURL overrides the GitHub API host.
func New(login string, repos []string, apiURL string, httpClient *http.Client) *Provider {
	if login == "" {
		login = "dev"
	}
	if apiURL == "" {
		apiURL = "https://api.github.com"
	}
	if httpClient == nil {
		httpClient = scm.DefaultHTTPClient()
	}
	return &Provider{
		user:   domain.User{Provider: domain.ProviderDev, ProviderUserID: login, Login: login, Name: login},
		repos:  repos,
		apiURL: strings.TrimRight(apiURL, "/"),
		http:   httpClient,
	}
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

func (p *Provider) ListRepos(ctx context.Context, token string) ([]domain.Repo, error) {
	out := make([]domain.Repo, 0, len(p.repos))
	for _, name := range p.repos {
		r, err := p.GetRepo(ctx, token, name)
		if err != nil {
			return nil, err
		}
		out = append(out, *r)
	}
	return out, nil
}

// GetRepo reads a public repository from GitHub; when that fails the repository
// is still returned with a default branch of main so linking works offline.
func (p *Provider) GetRepo(ctx context.Context, _, fullName string) (*domain.Repo, error) {
	if !strings.Contains(fullName, "/") {
		return nil, domain.Invalid("repoFullName", "use owner/name")
	}
	repo := domain.Repo{
		Provider:      domain.ProviderDev,
		ExternalID:    fullName,
		FullName:      fullName,
		URL:           "https://github.com/" + fullName,
		DefaultBranch: "main",
	}
	var gh struct {
		ID            int64  `json:"id"`
		HTMLURL       string `json:"html_url"`
		DefaultBranch string `json:"default_branch"`
		Private       bool   `json:"private"`
	}
	if err := scm.DoJSON(ctx, p.http, http.MethodGet, p.apiURL+"/repos/"+fullName, "", nil, &gh, nil); err == nil {
		repo.ExternalID = strconv.FormatInt(gh.ID, 10)
		repo.URL = gh.HTMLURL
		repo.DefaultBranch = gh.DefaultBranch
		repo.Private = gh.Private
	}
	return &repo, nil
}

// BranchHead resolves the branch on GitHub; unknown repositories fall back to a
// synthetic sha so launches still exercise the Massdriver flow.
func (p *Provider) BranchHead(ctx context.Context, _, fullName, branch string) (string, error) {
	var out struct {
		SHA string `json:"sha"`
	}
	err := scm.DoJSON(ctx, p.http, http.MethodGet, p.apiURL+"/repos/"+fullName+"/commits/"+url.PathEscape(branch), "", nil, &out, nil)
	if err == nil && out.SHA != "" {
		return out.SHA, nil
	}
	var httpErr *scm.HTTPError
	if ok := errorsAs(err, &httpErr); ok && httpErr.Status == http.StatusNotFound {
		return "", fmt.Errorf("branch %q not found", branch)
	}
	return "dev0000" + strings.Repeat("0", 33), nil
}

func errorsAs(err error, target **scm.HTTPError) bool {
	e, ok := err.(*scm.HTTPError) //nolint:errorlint // DoJSON returns the error unwrapped
	if ok {
		*target = e
	}
	return ok
}

// CloneURL is the public HTTPS URL; the stub only knows public repositories.
func (p *Provider) CloneURL(repo domain.Repo, _ string) string {
	return strings.TrimSuffix(repo.URL, "/") + ".git"
}

// CreateWebhook is a no-op: nothing on the internet can reach a laptop anyway.
func (p *Provider) CreateWebhook(context.Context, string, string, string, string) (string, error) {
	return "", nil
}

func (p *Provider) DeleteWebhook(context.Context, string, string, string) error { return nil }

type pushPayload struct {
	Ref        string `json:"ref"`
	After      string `json:"after"`
	Deleted    bool   `json:"deleted"`
	Repository struct {
		FullName string `json:"full_name"`
	} `json:"repository"`
}

// ParsePush accepts GitHub-shaped push payloads signed with the webhook secret, so
// a push can be simulated with curl.
func (p *Provider) ParsePush(r *http.Request, secret string) (*domain.PushEvent, error) {
	body, err := scm.ReadBody(r)
	if err != nil {
		return nil, err
	}
	if !github.ValidSignature(body, r.Header.Get("X-Hub-Signature-256"), secret) {
		return nil, fmt.Errorf("%w: bad signature", domain.ErrForbidden)
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
		Provider:     domain.ProviderDev,
		RepoFullName: payload.Repository.FullName,
		Branch:       branch,
		SHA:          payload.After,
		Deleted:      payload.Deleted,
	}, nil
}

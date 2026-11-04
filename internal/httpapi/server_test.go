package httpapi_test

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/massdriver-cloud/rocketsled/internal/auth"
	"github.com/massdriver-cloud/rocketsled/internal/domain"
	"github.com/massdriver-cloud/rocketsled/internal/domain/mocks"
	"github.com/massdriver-cloud/rocketsled/internal/httpapi"
	"github.com/massdriver-cloud/rocketsled/internal/scm/github"
	"github.com/massdriver-cloud/rocketsled/internal/store/memory"
)

type env struct {
	srv      *httptest.Server
	sessions *auth.Sessions
	platform *mocks.PlatformMock
	store    *memory.Store
	user     *domain.User
}

func projectEnv(id string) *domain.Environment {
	return &domain.Environment{
		ID: id, Name: id, ProjectID: "shop",
		Instances: []domain.PlatformInstance{{
			ID: id + "-api", ComponentSlug: "api", BundleName: "shop-api", Version: "1.0.0", Status: "PROVISIONED",
			Params:       map[string]any{"image": map[string]any{"tag": "old"}},
			ParamsSchema: map[string]any{"properties": map[string]any{"image": map[string]any{"properties": map[string]any{"tag": map[string]any{}}}}},
		}},
	}
}

func newEnv(t *testing.T) *env {
	t.Helper()
	platform := &mocks.PlatformMock{
		GetProjectFunc: func(context.Context, string) (*domain.Project, error) {
			return &domain.Project{ID: "shop", Name: "Shop", Components: []domain.Component{{Slug: "api", Name: "API"}}}, nil
		},
		GetEnvironmentFunc: func(_ context.Context, id string) (*domain.Environment, error) { return projectEnv(id), nil },
		ForkEnvironmentFunc: func(_ context.Context, req domain.ForkRequest) (*domain.Environment, error) {
			return projectEnv("shop-" + req.ID), nil
		},
		CreateDeploymentFunc: func(context.Context, string, map[string]any, string) (string, error) { return "dep-1", nil },
	}
	store := memory.New()
	// A real GitHub provider so webhook signature checking is exercised end to end;
	// API calls it would make are never reached in these tests.
	gh := github.New("id", "secret", "", nil)
	svc, err := domain.NewService(domain.Options{
		Platform: platform, Store: store, SCMs: []domain.SCM{gh},
		Projects:      []domain.ProjectLink{{ID: "shop", BaseEnvironment: "prod", ImageTagParam: "image.tag"}},
		BaseURL:       "http://sled.test",
		WebhookSecret: "hook",
	})
	require.NoError(t, err)
	sessions := auth.New("0123456789abcdef0123456789abcdef", time.Hour, false)
	ui := fstest.MapFS{
		"index.html":    {Data: []byte("<html>rocketsled</html>")},
		"assets/app.js": {Data: []byte("console.log(1)")},
	}
	handler := httpapi.New(httpapi.Options{Service: svc, Sessions: sessions, BaseURL: "http://sled.test", WebhookSecret: "hook", UI: ui})
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	user := &domain.User{ID: "github:1", Provider: domain.ProviderGitHub, ProviderUserID: "1", Login: "alice", CreatedAt: time.Now()}
	require.NoError(t, store.UpsertUser(context.Background(), user))
	require.NoError(t, store.SaveCredential(context.Background(), user.ID, "tok"))
	return &env{srv: srv, sessions: sessions, platform: platform, store: store, user: user}
}

type result struct {
	StatusCode int
	Header     http.Header
	Cookies    []*http.Cookie
	Body       []byte
}

func (e *env) do(t *testing.T, method, path string, body any, signedIn bool) (result, []byte) {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		require.NoError(t, json.NewEncoder(&buf).Encode(body))
	}
	req, err := http.NewRequest(method, e.srv.URL+path, &buf)
	require.NoError(t, err)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if signedIn {
		req.AddCookie(&http.Cookie{Name: auth.SessionCookie, Value: e.sessions.Issue(e.user.ID)})
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	var out bytes.Buffer
	_, _ = out.ReadFrom(resp.Body)
	return result{StatusCode: resp.StatusCode, Header: resp.Header, Cookies: resp.Cookies(), Body: out.Bytes()}, out.Bytes()
}

func (e *env) seedStack(t *testing.T) {
	t.Helper()
	stack := &domain.Stack{
		ID: "st1", Name: "Shop", ProjectID: "shop", CreatedBy: e.user.ID, CreatedAt: time.Now(),
		Repo:            domain.Repo{Provider: domain.ProviderGitHub, FullName: "acme/shop", DefaultBranch: "main"},
		DeployTokenHash: sha256hex("rsd_test"),
	}
	require.NoError(t, e.store.CreateStack(context.Background(), stack))
}

func sha256hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func TestAuthenticationIsRequiredForTheAPI(t *testing.T) {
	e := newEnv(t)

	resp, body := e.do(t, http.MethodGet, "/api/v1/stacks", nil, false)
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	assert.JSONEq(t, `{"error":"sign in to continue"}`, string(body))

	resp, body = e.do(t, http.MethodGet, "/api/v1/me", nil, true)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Contains(t, string(body), `"login":"alice"`)

	resp, body = e.do(t, http.MethodGet, "/api/v1/meta", nil, false)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.JSONEq(t, `{"providers":["github"],"baseUrl":"http://sled.test"}`, string(body))
}

func TestLoginRedirectsToProviderWithStateCookie(t *testing.T) {
	e := newEnv(t)

	resp, _ := e.do(t, http.MethodGet, "/auth/github/login?next=/stacks", nil, false)
	assert.Equal(t, http.StatusFound, resp.StatusCode)
	loc := resp.Header.Get("Location")
	assert.Contains(t, loc, "https://github.com/login/oauth/authorize?")
	assert.Contains(t, loc, "redirect_uri=http%3A%2F%2Fsled.test%2Fauth%2Fgithub%2Fcallback")
	require.Len(t, resp.Cookies, 1)
	assert.Equal(t, auth.StateCookie, resp.Cookies[0].Name)

	resp, _ = e.do(t, http.MethodGet, "/auth/bitbucket/login", nil, false)
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)

	resp, _ = e.do(t, http.MethodGet, "/auth/github/callback?state=forged&code=x", nil, false)
	assert.Equal(t, http.StatusFound, resp.StatusCode)
	assert.Contains(t, resp.Header.Get("Location"), "/login?error=")
}

func TestProjectsListIncludesPresetsAndAppComponent(t *testing.T) {
	e := newEnv(t)
	e.platform.GetEnvironmentFunc = func(_ context.Context, id string) (*domain.Environment, error) {
		env := projectEnv(id)
		env.Instances[0].ParamsSchema["examples"] = []any{map[string]any{"__name": "Small", "replicas": 1}}
		return env, nil
	}

	resp, body := e.do(t, http.MethodGet, "/api/v1/projects", nil, true)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var out []struct {
		AppComponent string `json:"appComponent"`
		Project      struct {
			Components []struct {
				Slug   string `json:"slug"`
				Bundle struct {
					Name    string `json:"name"`
					Presets []struct {
						Name string `json:"name"`
					} `json:"presets"`
				} `json:"bundle"`
			} `json:"components"`
		} `json:"project"`
	}
	require.NoError(t, json.Unmarshal(body, &out))
	require.Len(t, out, 1)
	assert.Equal(t, "api", out[0].AppComponent)
	assert.Equal(t, "shop-api", out[0].Project.Components[0].Bundle.Name)
	assert.Equal(t, "Small", out[0].Project.Components[0].Bundle.Presets[0].Name)
}

func TestValidationErrorsAre422(t *testing.T) {
	e := newEnv(t)
	e.seedStack(t)

	resp, body := e.do(t, http.MethodPost, "/api/v1/stacks/st1/instances", map[string]any{"slug": "Nope!"}, true)
	assert.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)
	assert.JSONEq(t, `{"error":"use 1-20 lowercase letters or digits","field":"slug"}`, string(body))

	resp, body = e.do(t, http.MethodPost, "/api/v1/stacks", map[string]any{"projectId": "nope", "repoFullName": "a/b"}, true)
	assert.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)
	assert.Contains(t, string(body), `"field":"projectId"`)

	resp, _ = e.do(t, http.MethodGet, "/api/v1/stacks/missing", nil, true)
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)

	resp, body = e.do(t, http.MethodPost, "/api/v1/stacks", map[string]any{"unknownField": true}, true)
	assert.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)
	assert.Contains(t, string(body), "invalid JSON")
}

func TestWebhookDeploysMatchingInstances(t *testing.T) {
	e := newEnv(t)
	e.seedStack(t)
	require.NoError(t, e.store.CreateInstance(context.Background(), &domain.Instance{
		ID: "in1", StackID: "st1", Slug: "staging", Branch: "main", EnvironmentID: "shop-staging", State: domain.InstanceStateRunning, CreatedAt: time.Now(),
	}))
	payload := []byte(`{"ref":"refs/heads/main","after":"c0ffee","repository":{"full_name":"acme/shop"}}`)
	mac := hmac.New(sha256.New, []byte("hook"))
	mac.Write(payload)

	req, _ := http.NewRequest(http.MethodPost, e.srv.URL+"/hooks/github", bytes.NewReader(payload))
	req.Header.Set("X-GitHub-Event", "push")
	req.Header.Set("X-Hub-Signature-256", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusAccepted, resp.StatusCode)

	require.Len(t, e.platform.CreateDeploymentCalls(), 1)
	call := e.platform.CreateDeploymentCalls()[0]
	assert.Equal(t, "shop-staging-api", call.InstanceID)
	tag, _ := domain.GetParam(call.Params, "image.tag")
	assert.Equal(t, "c0ffee", tag)

	req, _ = http.NewRequest(http.MethodPost, e.srv.URL+"/hooks/github", bytes.NewReader(payload))
	req.Header.Set("X-GitHub-Event", "push")
	req.Header.Set("X-Hub-Signature-256", "sha256=bad")
	resp, err = http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestCIDeployUsesStackToken(t *testing.T) {
	e := newEnv(t)
	e.seedStack(t)
	require.NoError(t, e.store.CreateInstance(context.Background(), &domain.Instance{
		ID: "in1", StackID: "st1", Slug: "staging", Branch: "main", EnvironmentID: "shop-staging", State: domain.InstanceStateRunning, CreatedAt: time.Now(),
	}))
	body := bytes.NewBufferString(`{"ref":"main","sha":"abc1234","imageTag":"1.2.3"}`)

	req, _ := http.NewRequest(http.MethodPost, e.srv.URL+"/ci/v1/stacks/st1/deploys", body)
	req.Header.Set("Authorization", "Bearer rsd_test")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusAccepted, resp.StatusCode)
	var out struct {
		Deploys []domain.Deploy `json:"deploys"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	require.Len(t, out.Deploys, 1)
	assert.Equal(t, "1.2.3", out.Deploys[0].ImageTag)
	assert.Equal(t, domain.TriggerCI, out.Deploys[0].Trigger)

	req, _ = http.NewRequest(http.MethodPost, e.srv.URL+"/ci/v1/stacks/st1/deploys", bytes.NewBufferString(`{"ref":"main","sha":"abc"}`))
	req.Header.Set("Authorization", "Bearer rsd_wrong")
	resp, err = http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestInstanceLifecycleOverHTTP(t *testing.T) {
	e := newEnv(t)
	e.seedStack(t)
	require.NoError(t, e.store.CreateInstance(context.Background(), &domain.Instance{
		ID: "in1", StackID: "st1", Slug: "staging", Branch: "main", EnvironmentID: "shop-staging", State: domain.InstanceStateLaunching, CreatedAt: time.Now(),
	}))

	resp, body := e.do(t, http.MethodGet, "/api/v1/instances/in1", nil, true)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Contains(t, string(body), `"state":"running"`)
	assert.Contains(t, string(body), `"componentSlug":"api"`)

	resp, body = e.do(t, http.MethodPost, "/api/v1/instances/in1/deploys", map[string]any{"sha": "beefcafe"}, true)
	require.Equal(t, http.StatusAccepted, resp.StatusCode)
	assert.Contains(t, string(body), `"sha":"beefcafe"`)

	resp, body = e.do(t, http.MethodDelete, "/api/v1/instances/in1", nil, true)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Contains(t, string(body), `"state":"decommissioning"`)
	require.Len(t, e.platform.DecommissionEnvironmentCalls(), 1)

	resp, _ = e.do(t, http.MethodDelete, "/api/v1/stacks/st1", nil, true)
	assert.Equal(t, http.StatusConflict, resp.StatusCode)
}

func TestUIServesAssetsAndFallsBackToIndex(t *testing.T) {
	e := newEnv(t)

	resp, body := e.do(t, http.MethodGet, "/stacks/abc/instances/def", nil, false)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "<html>rocketsled</html>", string(body))

	resp, body = e.do(t, http.MethodGet, "/assets/app.js", nil, false)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "console.log(1)", string(body))
	assert.Contains(t, resp.Header.Get("Cache-Control"), "immutable")

	resp, _ = e.do(t, http.MethodGet, "/api/v1/nothing", nil, true)
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

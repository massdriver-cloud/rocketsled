package httpapi_test

import (
	"bytes"
	"context"
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
	"github.com/massdriver-cloud/rocketsled/internal/scm/dev"
	"github.com/massdriver-cloud/rocketsled/internal/store/memory"
)

type env struct {
	srv      *httptest.Server
	sessions *auth.Sessions
	platform *mocks.PlatformMock
	store    *memory.Store
	user     *domain.User
}

func liveEnv(id, status string) *domain.PlatformEnvironment {
	return &domain.PlatformEnvironment{ID: id, Name: id, ProjectID: "serverless", Instances: []domain.PlatformInstance{{
		ID: id + "-api", ComponentSlug: "api", BundleName: "http-api", Version: "0.0.0", Status: status,
		ParamsSchema: map[string]any{"examples": []any{map[string]any{"__name": "Development", "throttle_rate": 50}}},
		Resources:    []domain.InstanceResource{{Field: "api", Name: "HTTP API", Payload: map[string]any{"endpoint": "https://x"}}},
	}}}
}

func newEnv(t *testing.T) *env {
	t.Helper()
	platform := &mocks.PlatformMock{
		PingFunc: func(context.Context) (string, error) { return "RocketSled Test", nil },
		GetProjectFunc: func(context.Context, string) (*domain.Project, error) {
			return &domain.Project{ID: "serverless", Name: "AWS Serverless Platform", Components: []domain.Component{{Slug: "api", Name: "HTTP API"}}}, nil
		},
		GetEnvironmentFunc: func(_ context.Context, id string) (*domain.PlatformEnvironment, error) {
			return liveEnv(id, "PROVISIONED"), nil
		},
		ForkEnvironmentFunc: func(_ context.Context, req domain.ForkRequest) (*domain.PlatformEnvironment, error) {
			return liveEnv("serverless-"+req.ID, "INITIALIZED"), nil
		},
		CreateResourceFunc:   func(context.Context, string, string, map[string]any) (string, error) { return "res-role", nil },
		DeploymentStatusFunc: func(context.Context, string) (string, error) { return "COMPLETED", nil },
	}
	platform.StartDeploymentFunc = func(ctx context.Context, id string, params map[string]any, msg string) (string, error) {
		return "dep-1", platform.DeployInstance(ctx, id, params, msg)
	}
	installer := &mocks.InstallerMock{InstallFunc: func(context.Context, domain.InstallRequest) (*domain.InstallResult, error) {
		return &domain.InstallResult{ProjectID: "serverless", Environments: map[string]string{"production": "serverless-production"}}, nil
	}}
	store := memory.New()
	catalog := domain.Catalog{
		ID: "aws-serverless", Name: "AWS Serverless", Cloud: "aws", Bootstrap: domain.Bootstrap{Project: "serverless", BaseEnvironment: "production", AppComponent: "api", ImageParam: "image"},
		Platforms: []domain.PlatformType{{ID: "aws-iam-role", Label: "AWS IAM Role", Schema: map[string]any{"required": []any{"arn"}, "properties": map[string]any{"arn": map[string]any{"title": "Role ARN"}}}}},
	}
	svc, err := domain.NewService(domain.Options{
		Platforms: func(domain.Credential) domain.Platform { return platform }, Store: store, SCMs: []domain.SCM{dev.New("cory")},
		Installer: installer, Catalogs: []domain.Catalog{catalog}, Mode: domain.AccountModeShared,
		SharedCredential: domain.Credential{OrganizationID: "rs-test", APIKey: "md_test"}, DataDir: t.TempDir(),
		RunAsync: func(f func()) { f() },
	})
	require.NoError(t, err)
	sessions := auth.New("0123456789abcdef0123456789abcdef", time.Hour, false)
	ui := fstest.MapFS{"index.html": {Data: []byte("<html>rocketsled</html>")}, "assets/app.js": {Data: []byte("console.log(1)")}}
	srv := httptest.NewServer(httpapi.New(httpapi.Options{Service: svc, Sessions: sessions, BaseURL: "http://sled.test", UI: ui}))
	t.Cleanup(srv.Close)
	user := &domain.User{ID: "dev:cory", Provider: domain.ProviderDev, ProviderUserID: "cory", Login: "cory", CreatedAt: time.Now()}
	require.NoError(t, store.UpsertUser(context.Background(), user))
	return &env{srv: srv, sessions: sessions, platform: platform, store: store, user: user}
}

type result struct {
	StatusCode int
	Header     http.Header
	Cookies    []*http.Cookie
	Body       []byte
}

func (e *env) do(t *testing.T, method, path string, body any, signedIn bool) result {
	t.Helper()
	return e.doWith(t, method, path, body, signedIn, "")
}

func (e *env) doWith(t *testing.T, method, path string, body any, signedIn bool, authorization string) result {
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
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
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
	return result{StatusCode: resp.StatusCode, Header: resp.Header, Cookies: resp.Cookies(), Body: out.Bytes()}
}

func TestAuthenticationAndSignupGates(t *testing.T) {
	e := newEnv(t)

	r := e.do(t, http.MethodGet, "/api/v1/account", nil, false)
	assert.Equal(t, http.StatusUnauthorized, r.StatusCode)

	r = e.do(t, http.MethodGet, "/api/v1/account", nil, true)
	assert.Equal(t, http.StatusPreconditionFailed, r.StatusCode, "signed in but not signed up")

	r = e.do(t, http.MethodGet, "/api/v1/me", nil, true)
	assert.Equal(t, http.StatusOK, r.StatusCode)
	assert.NotContains(t, string(r.Body), `"account"`)

	r = e.do(t, http.MethodGet, "/auth/dev/login?next=/catalogs", nil, false)
	assert.Equal(t, http.StatusFound, r.StatusCode)
	assert.Contains(t, r.Header.Get("Location"), "http://sled.test/auth/dev/callback?code=dev&state=")
}

func TestDomainsRoutes(t *testing.T) {
	e := newEnv(t)
	r := e.do(t, http.MethodPost, "/api/v1/account", map[string]any{"name": "Cory"}, true)
	require.Equal(t, http.StatusCreated, r.StatusCode, string(r.Body))

	r = e.do(t, http.MethodGet, "/api/v1/environments/x/domains", nil, true)
	assert.Equal(t, http.StatusNotFound, r.StatusCode, "domains are a tenant-mode offer")
	r = e.do(t, http.MethodPut, "/api/v1/environments/x/domains", map[string]any{"domains": []string{"www.example.com"}}, true)
	assert.Equal(t, http.StatusNotFound, r.StatusCode)
	r = e.do(t, http.MethodPut, "/api/v1/environments/x/domains", map[string]any{"hosts": []string{"www.example.com"}}, true)
	assert.Equal(t, http.StatusUnprocessableEntity, r.StatusCode)
	r = e.do(t, http.MethodPut, "/api/v1/environments/x/domains", nil, false)
	assert.Equal(t, http.StatusUnauthorized, r.StatusCode)
}

func TestPromoteRoute(t *testing.T) {
	e := newEnv(t)
	r := e.do(t, http.MethodPost, "/api/v1/account", map[string]string{"name": "Cory"}, true)
	require.Equal(t, http.StatusCreated, r.StatusCode)

	r = e.do(t, http.MethodPost, "/api/v1/environments/staging/promote", map[string]any{"to": 1}, true)
	assert.Equal(t, http.StatusUnprocessableEntity, r.StatusCode, "to is a string")

	r = e.do(t, http.MethodPost, "/api/v1/environments/staging/promote", map[string]string{"to": "production"}, true)
	assert.Equal(t, http.StatusNotFound, r.StatusCode, "only tenant mode has pipelines")
}

func TestOnboardingFlowOverHTTP(t *testing.T) {
	e := newEnv(t)

	r := e.do(t, http.MethodPost, "/api/v1/account", map[string]any{"name": "Cory"}, true)
	require.Equal(t, http.StatusCreated, r.StatusCode, string(r.Body))
	assert.Contains(t, string(r.Body), `"status":"new"`)

	r = e.do(t, http.MethodGet, "/api/v1/catalogs", nil, true)
	require.Equal(t, http.StatusOK, r.StatusCode)
	assert.Contains(t, string(r.Body), `"id":"aws-serverless"`)

	r = e.do(t, http.MethodPost, "/api/v1/account/install", map[string]any{"catalogId": "aws-serverless"}, true)
	require.Equal(t, http.StatusAccepted, r.StatusCode, string(r.Body))

	r = e.do(t, http.MethodGet, "/api/v1/account", nil, true)
	require.Equal(t, http.StatusOK, r.StatusCode)
	assert.Contains(t, string(r.Body), `"status":"needs_credentials"`)
	assert.Contains(t, string(r.Body), `"projectId":"serverless"`)

	r = e.do(t, http.MethodGet, "/api/v1/account/platforms", nil, true)
	require.Equal(t, http.StatusOK, r.StatusCode)
	assert.Contains(t, string(r.Body), `"satisfied":false`)

	r = e.do(t, http.MethodPost, "/api/v1/account/platforms/aws-iam-role", map[string]any{"payload": map[string]any{}}, true)
	assert.Equal(t, http.StatusUnprocessableEntity, r.StatusCode)
	assert.JSONEq(t, `{"error":"Role ARN is required","field":"arn"}`, string(r.Body))

	r = e.do(t, http.MethodPost, "/api/v1/account/platforms/aws-iam-role", map[string]any{"name": "My role", "payload": map[string]any{"arn": "arn:aws:iam::1:role/x"}}, true)
	require.Equal(t, http.StatusOK, r.StatusCode, string(r.Body))
	assert.Contains(t, string(r.Body), `"status":"ready"`)

	r = e.do(t, http.MethodGet, "/api/v1/project", nil, true)
	require.Equal(t, http.StatusOK, r.StatusCode)
	assert.Contains(t, string(r.Body), `"name":"Development"`)

	r = e.do(t, http.MethodPost, "/api/v1/environments", map[string]any{"slug": "dev", "name": "Dev", "presets": map[string]string{"api": "Development"}}, true)
	require.Equal(t, http.StatusAccepted, r.StatusCode, string(r.Body))
	var created domain.Environment
	require.NoError(t, json.Unmarshal(r.Body, &created))

	r = e.do(t, http.MethodGet, "/api/v1/environments/"+created.ID, nil, true)
	require.Equal(t, http.StatusOK, r.StatusCode)
	assert.Contains(t, string(r.Body), `"state":"running"`)
	assert.Contains(t, string(r.Body), `"endpoint":"https://x"`)

	r = e.do(t, http.MethodPost, "/api/v1/environments/"+created.ID+"/deploys", nil, true)
	assert.Equal(t, http.StatusAccepted, r.StatusCode)

	r = e.do(t, http.MethodDelete, "/api/v1/environments/"+created.ID, nil, true)
	assert.Equal(t, http.StatusOK, r.StatusCode)
	assert.Contains(t, string(r.Body), `"state":"decommissioning"`)

	r = e.do(t, http.MethodPost, "/api/v1/environments", map[string]any{"slug": "Bad"}, true)
	assert.Equal(t, http.StatusUnprocessableEntity, r.StatusCode)
}

func TestUIServesAssetsAndFallsBackToIndex(t *testing.T) {
	e := newEnv(t)
	r := e.do(t, http.MethodGet, "/environments/abc", nil, false)
	assert.Equal(t, http.StatusOK, r.StatusCode)
	assert.Equal(t, "<html>rocketsled</html>", string(r.Body))
	r = e.do(t, http.MethodGet, "/assets/app.js", nil, false)
	assert.Contains(t, r.Header.Get("Cache-Control"), "immutable")
	r = e.do(t, http.MethodGet, "/api/v1/nothing", nil, true)
	assert.Equal(t, http.StatusNotFound, r.StatusCode)
}

func TestDeployTokensReleaseOverBearerAuth(t *testing.T) {
	e := newEnv(t)
	for _, step := range []struct {
		method, path string
		body         any
	}{
		{http.MethodPost, "/api/v1/account", map[string]any{"name": "Cory"}},
		{http.MethodPost, "/api/v1/account/install", map[string]any{"catalogId": "aws-serverless"}},
		{http.MethodPost, "/api/v1/account/platforms/aws-iam-role", map[string]any{"payload": map[string]any{"arn": "arn:aws:iam::1:role/x"}}},
		{http.MethodPost, "/api/v1/environments", map[string]any{"slug": "production"}},
	} {
		r := e.do(t, step.method, step.path, step.body, true)
		require.Less(t, r.StatusCode, 300, "%s %s: %s", step.method, step.path, r.Body)
	}

	r := e.do(t, http.MethodPost, "/api/v1/account/tokens", map[string]any{"name": "github"}, true)
	require.Equal(t, http.StatusCreated, r.StatusCode, string(r.Body))
	var created struct {
		ID    string `json:"id"`
		Token string `json:"token"`
	}
	require.NoError(t, json.Unmarshal(r.Body, &created))
	bearer := "Bearer " + created.Token

	r = e.doWith(t, http.MethodPost, "/api/v1/environments/production/releases", map[string]any{"image": "app:v1", "description": "abc"}, false, bearer)
	require.Equal(t, http.StatusAccepted, r.StatusCode, string(r.Body))
	assert.Contains(t, string(r.Body), `"number":1`)
	assert.Contains(t, string(r.Body), `"requestedBy":"github"`)
	require.Len(t, e.platform.DeployInstanceCalls(), 1)
	assert.Equal(t, "app:v1", e.platform.DeployInstanceCalls()[0].Params["image"])

	r = e.doWith(t, http.MethodGet, "/api/v1/environments/production/releases", nil, false, bearer)
	require.Equal(t, http.StatusOK, r.StatusCode)
	assert.Contains(t, string(r.Body), `"image":"app:v1"`)

	r = e.do(t, http.MethodGet, "/api/v1/account/tokens", nil, true)
	require.Equal(t, http.StatusOK, r.StatusCode)
	assert.NotContains(t, string(r.Body), created.Token, "the secret is shown once")
	assert.Contains(t, string(r.Body), `"lastUsedAt"`)

	r = e.doWith(t, http.MethodGet, "/api/v1/environments", nil, false, bearer)
	assert.Equal(t, http.StatusUnauthorized, r.StatusCode, "a deploy token only releases")
	r = e.doWith(t, http.MethodPost, "/api/v1/environments/production/releases", map[string]any{"image": "app:v2"}, false, "Bearer rs_wrong")
	assert.Equal(t, http.StatusUnauthorized, r.StatusCode)
	r = e.doWith(t, http.MethodPost, "/api/v1/environments/production/releases", map[string]any{"image": "app:v2"}, false, "Basic abc")
	assert.Equal(t, http.StatusUnauthorized, r.StatusCode, "another scheme falls back to the session, and there is none")
	r = e.doWith(t, http.MethodGet, "/api/v1/environments/production/releases", nil, false, "bearer "+created.Token)
	assert.Equal(t, http.StatusOK, r.StatusCode, "the scheme is case-insensitive")

	r = e.doWith(t, http.MethodPut, "/api/v1/previews/pr1", map[string]any{"image": "app:v1"}, false, bearer)
	assert.Equal(t, http.StatusNotFound, r.StatusCode, "previews take a deploy token, and are only offered in tenant mode")
	r = e.doWith(t, http.MethodDelete, "/api/v1/previews/pr1", nil, false, bearer)
	assert.Equal(t, http.StatusNotFound, r.StatusCode)

	r = e.do(t, http.MethodDelete, "/api/v1/account/tokens/"+created.ID, nil, true)
	assert.Equal(t, http.StatusNoContent, r.StatusCode)
	r = e.doWith(t, http.MethodPost, "/api/v1/environments/production/releases", map[string]any{"image": "app:v2"}, false, bearer)
	assert.Equal(t, http.StatusUnauthorized, r.StatusCode, "a revoked token stops working")
}

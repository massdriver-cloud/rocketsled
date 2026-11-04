package massdriver_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/massdriver-cloud/rocketsled/internal/domain"
	"github.com/massdriver-cloud/rocketsled/internal/massdriver"
)

type capture struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables"`
}

// server records the last GraphQL request and replies with the canned body.
func server(t *testing.T, reply string, status int) (*massdriver.Client, *capture, *http.Header) {
	t.Helper()
	var last capture
	var headers http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v2", r.URL.Path)
		headers = r.Header.Clone()
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&last))
		w.WriteHeader(status)
		_, _ = w.Write([]byte(reply))
	}))
	t.Cleanup(srv.Close)
	return massdriver.New(srv.URL, "acme", "md_secret", srv.Client()), &last, &headers
}

func TestBearerAuthForAccessTokensAndBasicForLegacyKeys(t *testing.T) {
	c, _, headers := server(t, `{"data":{"organization":{"id":"acme","name":"Acme"}}}`, 200)
	_, err := c.Ping(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "Bearer md_secret", headers.Get("Authorization"))

	var basic http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		basic = r.Header.Clone()
		_, _ = w.Write([]byte(`{"data":{"organization":{"id":"acme","name":"Acme"}}}`))
	}))
	defer srv.Close()
	legacy := massdriver.New(srv.URL, "acme", "legacy-key", srv.Client())
	_, err = legacy.Ping(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "Basic YWNtZTpsZWdhY3kta2V5", basic.Get("Authorization"), `base64("acme:legacy-key")`)
}

func TestGetProjectMapsComponentsToLocalSlugs(t *testing.T) {
	c, last, _ := server(t, `{"data":{"project":{
		"id":"shop","name":"Shop","description":"d",
		"environments":{"items":[{"id":"shop-prod","name":"Production"}]},
		"components":[{"id":"shop-api","name":"API","ociRepo":{"name":"shop-api"}},{"id":"shop-db","name":"DB","ociRepo":null}]
	}}}`, 200)

	p, err := c.GetProject(context.Background(), "shop")
	require.NoError(t, err)

	assert.Equal(t, "acme", last.Variables["organizationId"])
	assert.Equal(t, "shop", last.Variables["id"])
	assert.Equal(t, []domain.Component{
		{Slug: "api", Name: "API", Bundle: domain.Bundle{Name: "shop-api"}},
		{Slug: "db", Name: "DB"},
	}, p.Components)
	assert.Equal(t, []domain.EnvironmentSummary{{ID: "shop-prod", Name: "Production"}}, p.Environments)
}

func TestGetEnvironmentExposesSchemaParamsAndUnfulfilled(t *testing.T) {
	c, _, _ := server(t, `{"data":{"environment":{
		"id":"shop-prod","name":"Production","project":{"id":"shop"},
		"instances":[{"id":"shop-prod-api","name":"API","status":"PROVISIONED","params":{"image":{"tag":"v1"}},
		  "paramsSchema":{"examples":[{"__name":"Small","replicas":1}]},"resolvedVersion":"1.2.0","deployedVersion":"1.1.0",
		  "component":{"id":"shop-api"},"bundle":{"name":"shop-api"}}],
		"unfulfilledDependencies":[{"instance":{"id":"shop-prod-db"},"field":"aws_authentication","resourceType":{"id":"aws-iam-role"}}]
	}}}`, 200)

	env, err := c.GetEnvironment(context.Background(), "shop-prod")
	require.NoError(t, err)

	require.Len(t, env.Instances, 1)
	pi := env.Instances[0]
	assert.Equal(t, "api", pi.ComponentSlug)
	assert.Equal(t, "shop-api", pi.BundleName)
	assert.Equal(t, "1.2.0", pi.Version)
	assert.Equal(t, "1.1.0", pi.DeployedVersion)
	assert.Equal(t, []domain.Preset{{Name: "Small", Params: map[string]any{"__name": "Small", "replicas": float64(1)}}}, pi.Presets())
	assert.Equal(t, []domain.UnfulfilledDependency{{InstanceID: "shop-prod-db", Field: "aws_authentication", ResourceType: "aws-iam-role"}}, env.Unfulfilled)
}

func TestNotFoundErrorsMapToDomainSentinel(t *testing.T) {
	c, _, _ := server(t, `{"data":{"environment":null},"errors":[{"message":"Environment not found","code":"NOT_FOUND","path":["environment"]}]}`, 200)
	_, err := c.GetEnvironment(context.Background(), "shop-nope")
	require.ErrorIs(t, err, domain.ErrNotFound)

	c, _, _ = server(t, `{"data":null,"errors":[{"message":"nope","extensions":{"code":"UNAUTHENTICATED"}}]}`, 200)
	_, err = c.GetProject(context.Background(), "shop")
	assert.ErrorIs(t, err, domain.ErrForbidden)
}

func TestForkSendsInputAndSurfacesValidationMessages(t *testing.T) {
	c, last, _ := server(t, `{"data":{"forkEnvironment":{"successful":true,"messages":null,"result":{
		"id":"shop-pr1","name":"PR 1","project":{"id":"shop"},"instances":[],"unfulfilledDependencies":[]}}}}`, 200)

	env, err := c.ForkEnvironment(context.Background(), domain.ForkRequest{
		ParentEnvironmentID: "shop-prod", ID: "pr1", Name: "PR 1", CopyEnvironmentDefaults: true, CopySecrets: true,
	})
	require.NoError(t, err)
	assert.Equal(t, "shop-pr1", env.ID)
	assert.Equal(t, "shop-prod", last.Variables["parentId"])
	input := last.Variables["input"].(map[string]any)
	assert.Equal(t, "pr1", input["id"])
	assert.Equal(t, true, input["copyEnvironmentDefaults"])
	assert.Equal(t, true, input["copySecrets"])
	assert.Equal(t, false, input["copyRemoteReferences"])

	c, _, _ = server(t, `{"data":{"forkEnvironment":{"successful":false,"messages":[{"field":"id","message":"has already been taken","code":"taken"}],"result":null}}}`, 200)
	_, err = c.ForkEnvironment(context.Background(), domain.ForkRequest{ParentEnvironmentID: "shop-prod", ID: "pr1", Name: "PR 1"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "forkEnvironment failed: id: has already been taken")
}

func TestCopyInstanceAndCreateDeployment(t *testing.T) {
	c, last, _ := server(t, `{"data":{"copyInstance":{"successful":true,"result":{"id":"shop-pr1-db"}}}}`, 200)
	err := c.CopyInstance(context.Background(), "shop-prod-db", "shop-pr1-db", map[string]any{"size": "small"})
	require.NoError(t, err)
	assert.Equal(t, "shop-prod-db", last.Variables["sourceId"])
	assert.Equal(t, "shop-pr1-db", last.Variables["destinationId"])
	assert.Equal(t, map[string]any{"overrides": map[string]any{"size": "small"}}, last.Variables["input"])

	c, last, _ = server(t, `{"data":{"createDeployment":{"successful":true,"result":{"id":"6b4c","status":"PENDING"}}}}`, 200)
	id, err := c.CreateDeployment(context.Background(), "shop-pr1-api", map[string]any{"image": map[string]any{"tag": "abc"}}, "run #2")
	require.NoError(t, err)
	assert.Equal(t, "6b4c", id)
	input := last.Variables["input"].(map[string]any)
	assert.Equal(t, "PROVISION", input["action"])
	assert.Equal(t, "run #2", input["message"])
	assert.Equal(t, map[string]any{"image": map[string]any{"tag": "abc"}}, input["params"])
}

func TestDeploymentStatusAndServerErrors(t *testing.T) {
	c, _, _ := server(t, `{"data":{"deployment":{"id":"6b4c","status":"RUNNING","action":"PROVISION","message":"m","updatedAt":"2026-09-09T00:00:00Z"}}}`, 200)
	d, err := c.GetDeployment(context.Background(), "6b4c")
	require.NoError(t, err)
	assert.Equal(t, domain.DeploymentStatus("RUNNING"), d.Status)

	c, _, _ = server(t, `upstream exploded`, 502)
	_, err = c.GetDeployment(context.Background(), "6b4c")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "server error")

	c, _, _ = server(t, ``, 401)
	err = c.DeployEnvironment(context.Background(), "shop-pr1")
	assert.ErrorIs(t, err, domain.ErrForbidden)
}

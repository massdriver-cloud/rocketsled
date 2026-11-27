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

func TestBearerAuthAndPing(t *testing.T) {
	c, last, headers := server(t, `{"data":{"organization":{"id":"acme","name":"Acme"}}}`, 200)
	name, err := c.Ping(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "Acme", name)
	assert.Equal(t, "Bearer md_secret", headers.Get("Authorization"))
	assert.Equal(t, "acme", last.Variables["organizationId"])
}

func TestGetProjectMapsComponentsAndLinks(t *testing.T) {
	c, _, _ := server(t, `{"data":{"project":{
		"id":"serverless","name":"AWS Serverless Platform","description":"d",
		"environments":{"items":[{"id":"serverless-production","name":"Production"}]},
		"components":[{"id":"serverless-api","name":"HTTP API","ociRepo":{"name":"http-api"}},{"id":"serverless-app","name":"Application","ociRepo":{"name":"lambda-function"}}],
		"links":[{"fromComponent":{"id":"serverless-api"},"toComponent":{"id":"serverless-app"}}]
	}}}`, 200)

	p, err := c.GetProject(context.Background(), "serverless")
	require.NoError(t, err)
	assert.Equal(t, []domain.Component{
		{Slug: "api", Name: "HTTP API", Bundle: domain.Bundle{Name: "http-api"}},
		{Slug: "app", Name: "Application", Bundle: domain.Bundle{Name: "lambda-function"}},
	}, p.Components)
	assert.Equal(t, []domain.Link{{From: "api", To: "app"}}, p.Links)
}

func TestGetEnvironmentExposesSchemaResourcesAndUnfulfilled(t *testing.T) {
	c, _, _ := server(t, `{"data":{"environment":{
		"id":"serverless-dev","name":"Dev","project":{"id":"serverless"},
		"instances":[{"id":"serverless-dev-api","name":"HTTP API","status":"PROVISIONED","params":{"region":"us-east-1"},
		  "paramsSchema":{"examples":[{"__name":"Development","region":"us-east-1"}]},"resolvedVersion":"0.0.0","deployedVersion":"0.0.0",
		  "component":{"id":"serverless-api","name":"HTTP API"},"bundle":{"name":"http-api"},
		  "resources":[{"field":"api","resource":{"id":"serverless-dev-api.api","name":"HTTP API","payload":{"endpoint":"https://x"},
		    "resourceType":{"id":"aws-http-api@0.0.0","schema":{"properties":{"region":{"title":"Region"},"endpoint":{"title":"Endpoint URL"},"id":{}}},"uiSchema":{}}}}]}],
		"unfulfilledDependencies":[{"instance":{"id":"serverless-dev-app"},"field":"aws_authentication","resourceType":{"id":"aws-iam-role@0.0.0"}}]
	}}}`, 200)

	env, err := c.GetEnvironment(context.Background(), "serverless-dev")
	require.NoError(t, err)
	require.Len(t, env.Instances, 1)
	pi := env.Instances[0]
	assert.Equal(t, "api", pi.ComponentSlug)
	assert.Equal(t, "HTTP API", pi.ComponentName)
	assert.Equal(t, []domain.Preset{{Name: "Development", Params: map[string]any{"__name": "Development", "region": "us-east-1"}}}, pi.Presets())
	assert.Equal(t, []domain.InstanceResource{{
		Field: "api", Name: "HTTP API", ResourceType: "aws-http-api@0.0.0", Payload: map[string]any{"endpoint": "https://x"},
		Fields: []domain.SchemaField{{Name: "region", Title: "Region"}, {Name: "endpoint", Title: "Endpoint URL"}, {Name: "id"}},
	}}, pi.Resources, "schema properties keep the order the API sends")
	assert.Equal(t, "aws-iam-role@0.0.0", env.Unfulfilled[0].ResourceType)
}

func TestErrorsMapToDomainSentinels(t *testing.T) {
	c, _, _ := server(t, `{"data":{"environment":null},"errors":[{"message":"Environment not found","code":"NOT_FOUND"}]}`, 200)
	_, err := c.GetEnvironment(context.Background(), "nope")
	require.ErrorIs(t, err, domain.ErrNotFound)

	c, _, _ = server(t, ``, 401)
	err = c.DeployEnvironment(context.Background(), "serverless-dev")
	require.ErrorIs(t, err, domain.ErrForbidden)
}

func TestMutationsSendInputsAndSurfaceValidation(t *testing.T) {
	c, last, _ := server(t, `{"data":{"createResource":{"successful":true,"result":{"id":"res-1"}}}}`, 200)
	id, err := c.CreateResource(context.Background(), "aws-iam-role", "My role", map[string]any{"arn": "arn:aws:iam::1:role/x", "token_level": "organization"})
	require.NoError(t, err)
	assert.Equal(t, "res-1", id)
	assert.Equal(t, "aws-iam-role", last.Variables["resourceTypeId"])
	input := last.Variables["input"].(map[string]any)
	assert.Equal(t, "My role", input["name"])
	assert.JSONEq(t, `{"arn":"arn:aws:iam::1:role/x","token_level":"organization"}`, input["payload"].(string), "Map scalars travel as JSON strings")

	c, last, _ = server(t, `{"data":{"createResourceGrant":{"successful":true,"result":{"id":"g"}}}}`, 200)
	require.NoError(t, c.GrantResource(context.Background(), "res-1"))
	assert.Equal(t, map[string]any{"action": "resource:export", "recipientConditions": "*"}, last.Variables["input"])

	c, last, _ = server(t, `{"data":{"setEnvironmentDefault":{"successful":true,"result":{"id":"d"}}}}`, 200)
	require.NoError(t, c.SetEnvironmentDefault(context.Background(), "serverless-dev", "res-1"))
	assert.Equal(t, "serverless-dev", last.Variables["environmentId"])

	c, last, _ = server(t, `{"data":{"createOrganization":{"successful":true,"result":{"id":"rs-x"}}}}`, 200)
	require.NoError(t, c.CreateOrganization(context.Background(), "rs-x", "RocketSled 1"))
	_, hasOrg := last.Variables["organizationId"]
	assert.False(t, hasOrg, "createOrganization takes no organization")

	c, _, _ = server(t, `{"data":{"createServiceAccount":{"successful":true,"result":{"id":"sa","token":"md_new"}}}}`, 200)
	tok, err := c.CreateServiceAccount(context.Background(), "rocketsled")
	require.NoError(t, err)
	assert.Equal(t, "md_new", tok)

	c, _, _ = server(t, `{"data":{"forkEnvironment":{"successful":false,"messages":[{"field":"id","message":"has already been taken","code":"taken"}],"result":null}}}`, 200)
	_, err = c.ForkEnvironment(context.Background(), domain.ForkRequest{ParentEnvironmentID: "serverless-production", ID: "dev", Name: "Dev"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "forkEnvironment failed: id: has already been taken")

	c, last, _ = server(t, `{"data":{"deployEnvironment":{"successful":true,"result":{"id":"serverless-dev"}}}}`, 200)
	require.NoError(t, c.DeployEnvironment(context.Background(), "serverless-dev"))
	assert.Equal(t, "serverless-dev", last.Variables["id"])

	c, last, _ = server(t, `{"data":{"createDeployment":{"successful":true,"result":{"id":"dep"}}}}`, 200)
	require.NoError(t, c.DecommissionInstance(context.Background(), "ruby-hello-db", map[string]any{"size": "small"}, "Removed by ada"))
	assert.Equal(t, "ruby-hello-db", last.Variables["id"])
	assert.Equal(t, map[string]any{"action": "DECOMMISSION", "message": "Removed by ada", "params": `{"size":"small"}`}, last.Variables["input"])

	c, last, _ = server(t, `{"data":{"updateInstance":{"successful":true,"result":{"id":"ruby-prod-web"}}}}`, 200)
	require.NoError(t, c.SetInstanceVersion(context.Background(), "ruby-prod-web", "1.3.0"))
	assert.Equal(t, map[string]any{"version": "1.3.0"}, last.Variables["input"])

	c, last, _ = server(t, `{"data":{"updateEnvironment":{"successful":true,"result":{"id":"ruby-hello"}}}}`, 200)
	require.NoError(t, c.SetEnvironmentDescription(context.Background(), "ruby-hello", "Made in Rocketsled"))
	assert.Equal(t, map[string]any{"description": "Made in Rocketsled"}, last.Variables["input"])
}

func TestCopyInstanceEncodesOverridesAsJSONString(t *testing.T) {
	c, last, _ := server(t, `{"data":{"copyInstance":{"successful":true,"result":{"id":"serverless-dev-api"}}}}`, 200)
	require.NoError(t, c.CopyInstance(context.Background(), "serverless-production-api", "serverless-dev-api", map[string]any{"throttle_rate": 50}))
	input := last.Variables["input"].(map[string]any)
	assert.JSONEq(t, `{"throttle_rate":50}`, input["overrides"].(string))
}

func TestReleasesTrackTheirOwnDeployment(t *testing.T) {
	c, last, _ := server(t, `{"data":{"createDeployment":{"successful":true,"result":{"id":"5f0c"}}}}`, 200)
	id, err := c.StartDeployment(context.Background(), "ruby-hello-web", map[string]any{"image_uri": "x"}, "Release #1")
	require.NoError(t, err)
	assert.Equal(t, "5f0c", id)
	assert.Equal(t, "PROVISION", last.Variables["input"].(map[string]any)["action"])

	c, last, _ = server(t, `{"data":{"deployment":{"id":"5f0c","status":"RUNNING"}}}`, 200)
	status, err := c.DeploymentStatus(context.Background(), "5f0c")
	require.NoError(t, err)
	assert.Equal(t, "RUNNING", status)
	assert.Equal(t, "5f0c", last.Variables["id"])
}

func TestGrantResourceToProjectSkipsCoveredProjects(t *testing.T) {
	cases := []struct {
		name     string
		existing string
		creates  bool
	}{
		{"no grants", `[]`, true},
		{"wildcard", `[{"action":"resource:export","recipientConditions":"*"}]`, false},
		{"lists the project", `[{"action":"resource:export","recipientConditions":"{\"md-project\":[\"node\",\"ruby\"]}"}]`, false},
		{"another project", `[{"action":"resource:export","recipientConditions":"{\"md-project\":[\"node\"]}"}]`, true},
		{"narrower than the project", `[{"action":"resource:export","recipientConditions":"{\"md-project\":[\"ruby\"],\"md-environment\":[\"ruby-prod\"]}"}]`, true},
		{"view only", `[{"action":"resource:view","recipientConditions":"*"}]`, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var calls []capture
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var c capture
				assert.NoError(t, json.NewDecoder(r.Body).Decode(&c))
				calls = append(calls, c)
				if len(calls) == 1 {
					_, _ = w.Write([]byte(`{"data":{"resource":{"id":"platform-main-cluster.cluster","grants":{"items":` + tc.existing + `}}}}`))
					return
				}
				_, _ = w.Write([]byte(`{"data":{"createResourceGrant":{"successful":true,"result":{"id":"g"}}}}`))
			}))
			t.Cleanup(srv.Close)
			c := massdriver.New(srv.URL, "acme", "md_secret", srv.Client())

			require.NoError(t, c.GrantResourceToProject(context.Background(), "platform-main-cluster.cluster", "ruby"))
			if !tc.creates {
				assert.Len(t, calls, 1)
				return
			}
			require.Len(t, calls, 2)
			assert.Equal(t, map[string]any{"action": "resource:export", "recipientConditions": `{"md-project":["ruby"]}`}, calls[1].Variables["input"])
		})
	}
}

func TestEnvironmentDefaultsListsResourceIDs(t *testing.T) {
	c, last, _ := server(t, `{"data":{"environment":{"id":"ruby-template","defaults":{"items":[{"resource":{"id":"platform-main-cluster.cluster"}},{"resource":{"id":"res-role"}}]}}}}`, 200)
	ids, err := c.EnvironmentDefaults(context.Background(), "ruby-template")
	require.NoError(t, err)
	assert.Equal(t, []string{"platform-main-cluster.cluster", "res-role"}, ids)
	assert.Equal(t, "ruby-template", last.Variables["id"])
}

func TestProjectsArePagedAndCreatedFromAnEnvironment(t *testing.T) {
	var calls []capture
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var c capture
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&c))
		calls = append(calls, c)
		if len(calls) == 1 {
			_, _ = w.Write([]byte(`{"data":{"projects":{"cursor":{"next":"p2"},"items":[{"id":"ruby","name":"Ruby","description":"","environments":{"items":[]}}]}}}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":{"projects":{"cursor":{"next":null},"items":[{"id":"shop","name":"shop","description":"[owner x]","environments":{"items":[{"id":"shop-template","name":"Template"}]}}]}}}`))
	}))
	t.Cleanup(srv.Close)
	c := massdriver.New(srv.URL, "acme", "md_secret", srv.Client())

	projects, err := c.ListProjects(context.Background())
	require.NoError(t, err)
	require.Len(t, projects, 2)
	assert.Equal(t, "shop", projects[1].ID)
	assert.Equal(t, "shop-template", projects[1].Environments[0].ID)
	assert.Equal(t, map[string]any{"limit": float64(100), "next": "p2"}, calls[1].Variables["cursor"])

	c2, last, _ := server(t, `{"data":{"createProjectFromEnvironment":{"successful":true,"result":{"id":"shop"}}}}`, 200)
	require.NoError(t, c2.CreateProjectFromEnvironment(context.Background(), "ruby-template", domain.NewProject{ID: "shop", Name: "Shop", Description: "d", EnvironmentID: "template", EnvironmentName: "Template"}))
	assert.Equal(t, "ruby-template", last.Variables["sourceEnvironmentId"])
	assert.Equal(t, map[string]any{
		"id": "shop", "name": "Shop", "description": "d", "copyParams": "CONFIGURED",
		"environment": map[string]any{"id": "template", "name": "Template"},
	}, last.Variables["input"])

	c3, last, _ := server(t, `{"data":{"deleteProject":{"successful":true,"result":{"id":"shop"}}}}`, 200)
	require.NoError(t, c3.DeleteProject(context.Background(), "shop"))
	assert.Equal(t, "shop", last.Variables["id"])
}

func TestProjectEnvironmentsArePaged(t *testing.T) {
	var calls []capture
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var c capture
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&c))
		calls = append(calls, c)
		if len(calls) == 1 {
			_, _ = w.Write([]byte(`{"data":{"project":{"id":"shop","name":"shop","components":[],"links":[],"environments":{"cursor":{"next":"e2"},"items":[{"id":"shop-template"}]}}}}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":{"project":{"environments":{"cursor":{"next":null},"items":[{"id":"shop-pr101"}]}}}}`))
	}))
	t.Cleanup(srv.Close)
	c := massdriver.New(srv.URL, "acme", "md_secret", srv.Client())

	p, err := c.GetProject(context.Background(), "shop")
	require.NoError(t, err)
	require.Len(t, p.Environments, 2, "environments past the first page are listed too")
	assert.Equal(t, "shop-pr101", p.Environments[1].ID)
	assert.Equal(t, map[string]any{"limit": float64(100), "next": "e2"}, calls[1].Variables["cursor"])
}

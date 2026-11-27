package massdriver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/massdriver-cloud/rocketsled/internal/domain"
)

// Factory builds a Client per credential, satisfying domain.PlatformFactory.
func Factory(c domain.Credential) domain.Platform {
	url := c.URL
	if url == "" {
		url = "https://api.massdriver.cloud"
	}
	return New(url, c.OrganizationID, c.APIKey, nil)
}

const projectQuery = `
query RocketsledProject($organizationId: ID!, $id: ID!) {
  project(organizationId: $organizationId, id: $id) {
    id name description
    environments(cursor: { limit: 100 }) { items { id name description createdAt } }
    components { id name ociRepo { name } }
    links { fromComponent { id } toComponent { id } }
  }
}`

type gqlProject struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Description  string `json:"description"`
	Environments struct {
		Items []struct {
			ID          string    `json:"id"`
			Name        string    `json:"name"`
			Description string    `json:"description"`
			CreatedAt   time.Time `json:"createdAt"`
		} `json:"items"`
	} `json:"environments"`
	Components []struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		OciRepo *struct {
			Name string `json:"name"`
		} `json:"ociRepo"`
	} `json:"components"`
	Links []struct {
		FromComponent struct {
			ID string `json:"id"`
		} `json:"fromComponent"`
		ToComponent struct {
			ID string `json:"id"`
		} `json:"toComponent"`
	} `json:"links"`
}

func (c *Client) GetProject(ctx context.Context, projectID string) (*domain.Project, error) {
	var out struct {
		Project *gqlProject `json:"project"`
	}
	if err := c.do(ctx, projectQuery, map[string]any{"id": projectID}, &out); err != nil {
		return nil, err
	}
	if out.Project == nil {
		return nil, &APIError{Code: "NOT_FOUND", Message: "project " + projectID + " not found"}
	}
	p := &domain.Project{
		ID:           out.Project.ID,
		Name:         out.Project.Name,
		Description:  out.Project.Description,
		Components:   []domain.Component{},
		Links:        []domain.Link{},
		Environments: []domain.EnvironmentSummary{},
	}
	for _, e := range out.Project.Environments.Items {
		p.Environments = append(p.Environments, domain.EnvironmentSummary{ID: e.ID, Name: e.Name, Description: e.Description, CreatedAt: e.CreatedAt})
	}
	for _, comp := range out.Project.Components {
		component := domain.Component{Slug: localID(comp.ID, out.Project.ID), Name: comp.Name}
		if comp.OciRepo != nil {
			component.Bundle.Name = comp.OciRepo.Name
		}
		p.Components = append(p.Components, component)
	}
	for _, l := range out.Project.Links {
		p.Links = append(p.Links, domain.Link{From: localID(l.FromComponent.ID, out.Project.ID), To: localID(l.ToComponent.ID, out.Project.ID)})
	}
	return p, nil
}

// mapArg encodes a Go map for Massdriver's Map scalar, which only accepts a
// JSON-encoded string on input.
func mapArg(m map[string]any) string {
	if m == nil {
		m = map[string]any{}
	}
	b, err := json.Marshal(m)
	if err != nil {
		return "{}"
	}
	return string(b)
}

// localID strips a parent identifier prefix: "shop-prod-api" under "shop-prod" is "api".
func localID(full, parent string) string {
	return strings.TrimPrefix(full, parent+"-")
}

const environmentFields = `
    id name description
    project { id }
    instances {
      id name status params paramsSchema resolvedVersion deployedVersion
      component { id name }
      bundle { name }
      resources { field resource { id name payload resourceType { id schema uiSchema } } }
    }
    unfulfilledDependencies { instance { id } field resourceType { id } }`

const environmentQuery = `
query RocketsledEnvironment($organizationId: ID!, $id: ID!) {
  environment(organizationId: $organizationId, id: $id) {` + environmentFields + `
  }
}`

type gqlEnvironment struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Project     struct {
		ID string `json:"id"`
	} `json:"project"`
	Instances []struct {
		ID              string         `json:"id"`
		Name            string         `json:"name"`
		Status          string         `json:"status"`
		Params          map[string]any `json:"params"`
		ParamsSchema    map[string]any `json:"paramsSchema"`
		ResolvedVersion string         `json:"resolvedVersion"`
		DeployedVersion string         `json:"deployedVersion"`
		Component       struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"component"`
		Bundle *struct {
			Name string `json:"name"`
		} `json:"bundle"`
		Resources []struct {
			Field    string `json:"field"`
			Resource *struct {
				ID           string         `json:"id"`
				Name         string         `json:"name"`
				Payload      map[string]any `json:"payload"`
				ResourceType struct {
					ID       string          `json:"id"`
					Schema   json.RawMessage `json:"schema"`
					UISchema json.RawMessage `json:"uiSchema"`
				} `json:"resourceType"`
			} `json:"resource"`
		} `json:"resources"`
	} `json:"instances"`
	UnfulfilledDependencies []struct {
		Instance struct {
			ID string `json:"id"`
		} `json:"instance"`
		Field        string `json:"field"`
		ResourceType struct {
			ID string `json:"id"`
		} `json:"resourceType"`
	} `json:"unfulfilledDependencies"`
}

func (c *Client) GetEnvironment(ctx context.Context, environmentID string) (*domain.PlatformEnvironment, error) {
	var out struct {
		Environment *gqlEnvironment `json:"environment"`
	}
	if err := c.do(ctx, environmentQuery, map[string]any{"id": environmentID}, &out); err != nil {
		return nil, err
	}
	if out.Environment == nil {
		return nil, &APIError{Code: "NOT_FOUND", Message: "environment " + environmentID + " not found"}
	}
	return toEnvironment(out.Environment), nil
}

func toEnvironment(e *gqlEnvironment) *domain.PlatformEnvironment {
	env := &domain.PlatformEnvironment{
		ID:          e.ID,
		Name:        e.Name,
		Description: e.Description,
		ProjectID:   e.Project.ID,
		Instances:   make([]domain.PlatformInstance, 0, len(e.Instances)),
		Unfulfilled: make([]domain.UnfulfilledDependency, 0, len(e.UnfulfilledDependencies)),
	}
	for _, in := range e.Instances {
		pi := domain.PlatformInstance{
			ID:              in.ID,
			ComponentSlug:   localID(in.Component.ID, e.Project.ID),
			ComponentName:   in.Component.Name,
			Name:            in.Name,
			Version:         in.ResolvedVersion,
			DeployedVersion: in.DeployedVersion,
			Status:          in.Status,
			Params:          in.Params,
			ParamsSchema:    in.ParamsSchema,
			Resources:       []domain.InstanceResource{},
		}
		if in.Bundle != nil {
			pi.BundleName = in.Bundle.Name
		}
		for _, r := range in.Resources {
			if r.Resource == nil {
				continue
			}
			pi.Resources = append(pi.Resources, domain.InstanceResource{
				Field: r.Field, Name: r.Resource.Name, ResourceType: r.Resource.ResourceType.ID, Payload: r.Resource.Payload,
				Fields: schemaFields(r.Resource.ResourceType.Schema, r.Resource.ResourceType.UISchema),
			})
		}
		env.Instances = append(env.Instances, pi)
	}
	for _, u := range e.UnfulfilledDependencies {
		env.Unfulfilled = append(env.Unfulfilled, domain.UnfulfilledDependency{
			InstanceID: u.Instance.ID, Field: u.Field, ResourceType: u.ResourceType.ID,
		})
	}
	return env
}

const forkMutation = `
mutation RocketsledFork($organizationId: ID!, $parentId: ID!, $input: ForkEnvironmentInput!) {
  forkEnvironment(organizationId: $organizationId, parentId: $parentId, input: $input) {
    successful
    messages { field message code }
    result {` + environmentFields + `
    }
  }
}`

func (c *Client) ForkEnvironment(ctx context.Context, req domain.ForkRequest) (*domain.PlatformEnvironment, error) {
	var out struct {
		ForkEnvironment payload[gqlEnvironment] `json:"forkEnvironment"`
	}
	vars := map[string]any{
		"parentId": req.ParentEnvironmentID,
		"input": map[string]any{
			"id":                      req.ID,
			"name":                    req.Name,
			"description":             req.Description,
			"copyEnvironmentDefaults": req.CopyEnvironmentDefaults,
			"copySecrets":             req.CopySecrets,
		},
	}
	if err := c.do(ctx, forkMutation, vars, &out); err != nil {
		return nil, err
	}
	if err := out.ForkEnvironment.check("forkEnvironment"); err != nil {
		return nil, err
	}
	if out.ForkEnvironment.Result == nil {
		return nil, errNilResult
	}
	return toEnvironment(out.ForkEnvironment.Result), nil
}

const copyInstanceMutation = `
mutation RocketsledCopyInstance($organizationId: ID!, $sourceId: ID!, $destinationId: ID!, $input: CopyInstanceInput!) {
  copyInstance(organizationId: $organizationId, sourceId: $sourceId, destinationId: $destinationId, input: $input) {
    successful
    messages { field message code }
    result { id }
  }
}`

func (c *Client) CopyInstance(ctx context.Context, sourceID, destinationID string, overrides map[string]any) error {
	var out struct {
		CopyInstance payload[struct{ ID string }] `json:"copyInstance"`
	}
	vars := map[string]any{
		"sourceId":      sourceID,
		"destinationId": destinationID,
		"input":         map[string]any{"overrides": mapArg(overrides)},
	}
	if err := c.do(ctx, copyInstanceMutation, vars, &out); err != nil {
		return err
	}
	return out.CopyInstance.check("copyInstance")
}

func (c *Client) DeployEnvironment(ctx context.Context, environmentID string) error {
	return c.simpleMutation(ctx, "deployEnvironment", environmentID)
}

func (c *Client) DecommissionEnvironment(ctx context.Context, environmentID string) error {
	return c.simpleMutation(ctx, "decommissionEnvironment", environmentID)
}

func (c *Client) DeleteEnvironment(ctx context.Context, environmentID string) error {
	return c.simpleMutation(ctx, "deleteEnvironment", environmentID)
}

// simpleMutation runs a (organizationId, id) -> EnvironmentPayload mutation.
func (c *Client) simpleMutation(ctx context.Context, name, id string) error {
	query := fmt.Sprintf(`
mutation Rocketsled($organizationId: ID!, $id: ID!) {
  %s(organizationId: $organizationId, id: $id) {
    successful
    messages { field message code }
    result { id }
  }
}`, name)
	var out map[string]payload[struct{ ID string }]
	if err := c.do(ctx, query, map[string]any{"id": id}, &out); err != nil {
		return err
	}
	return out[name].check(name)
}

const createResourceMutation = `
mutation RocketsledCreateResource($organizationId: ID!, $resourceTypeId: ID!, $input: CreateResourceInput!) {
  createResource(organizationId: $organizationId, resourceTypeId: $resourceTypeId, input: $input) {
    successful
    messages { field message code }
    result { id }
  }
}`

func (c *Client) CreateResource(ctx context.Context, resourceTypeID, name string, data map[string]any) (string, error) {
	var out struct {
		CreateResource payload[struct {
			ID string `json:"id"`
		}] `json:"createResource"`
	}
	vars := map[string]any{
		"resourceTypeId": resourceTypeID,
		"input":          map[string]any{"name": name, "payload": mapArg(data)},
	}
	if err := c.do(ctx, createResourceMutation, vars, &out); err != nil {
		return "", err
	}
	if err := out.CreateResource.check("createResource"); err != nil {
		return "", err
	}
	if out.CreateResource.Result == nil {
		return "", errNilResult
	}
	return out.CreateResource.Result.ID, nil
}

const grantResourceMutation = `
mutation RocketsledGrantResource($organizationId: ID!, $resourceId: ID!, $input: CreateResourceGrantInput!) {
  createResourceGrant(organizationId: $organizationId, resourceId: $resourceId, input: $input) {
    successful
    messages { field message code }
    result { id }
  }
}`

// GrantResource shares a resource with every environment in the organization.
func (c *Client) GrantResource(ctx context.Context, resourceID string) error {
	var out struct {
		CreateResourceGrant payload[struct{ ID string }] `json:"createResourceGrant"`
	}
	vars := map[string]any{
		"resourceId": resourceID,
		"input":      map[string]any{"action": "resource:export", "recipientConditions": "*"},
	}
	if err := c.do(ctx, grantResourceMutation, vars, &out); err != nil {
		return err
	}
	return out.CreateResourceGrant.check("createResourceGrant")
}

const setDefaultMutation = `
mutation RocketsledSetDefault($organizationId: ID!, $environmentId: ID!, $resourceId: ID!) {
  setEnvironmentDefault(organizationId: $organizationId, environmentId: $environmentId, resourceId: $resourceId) {
    successful
    messages { field message code }
    result { id }
  }
}`

func (c *Client) SetEnvironmentDefault(ctx context.Context, environmentID, resourceID string) error {
	var out struct {
		SetEnvironmentDefault payload[struct{ ID string }] `json:"setEnvironmentDefault"`
	}
	if err := c.do(ctx, setDefaultMutation, map[string]any{"environmentId": environmentID, "resourceId": resourceID}, &out); err != nil {
		return err
	}
	return out.SetEnvironmentDefault.check("setEnvironmentDefault")
}

const createOrganizationMutation = `
mutation RocketsledCreateOrganization($input: CreateOrganizationInput!) {
  createOrganization(input: $input) {
    successful
    messages { field message code }
    result { id name }
  }
}`

func (c *Client) CreateOrganization(ctx context.Context, id, name string) error {
	var out struct {
		CreateOrganization payload[struct{ ID string }] `json:"createOrganization"`
	}
	if err := c.doWithoutOrg(ctx, createOrganizationMutation, map[string]any{"input": map[string]any{"id": id, "name": name}}, &out); err != nil {
		return err
	}
	return out.CreateOrganization.check("createOrganization")
}

const createServiceAccountMutation = `
mutation RocketsledCreateServiceAccount($organizationId: ID!, $input: CreateServiceAccountInput!) {
  createServiceAccount(organizationId: $organizationId, input: $input) {
    successful
    messages { field message code }
    result { id token }
  }
}`

// CreateServiceAccount makes a service account whose default token lasts ten years.
func (c *Client) CreateServiceAccount(ctx context.Context, name string) (string, error) {
	var out struct {
		CreateServiceAccount payload[struct {
			ID    string `json:"id"`
			Token string `json:"token"`
		}] `json:"createServiceAccount"`
	}
	vars := map[string]any{"input": map[string]any{
		"name":                                  name,
		"description":                           "Rocketsled administers this organization.",
		"defaultAccessTokenExpirationInMinutes": 5256000,
	}}
	if err := c.do(ctx, createServiceAccountMutation, vars, &out); err != nil {
		return "", err
	}
	if err := out.CreateServiceAccount.check("createServiceAccount"); err != nil {
		return "", err
	}
	if out.CreateServiceAccount.Result == nil || out.CreateServiceAccount.Result.Token == "" {
		return "", errors.New("createServiceAccount returned no token")
	}
	return out.CreateServiceAccount.Result.Token, nil
}

const viewerQuery = `
query RocketsledViewer($organizationId: ID!) {
  organization(organizationId: $organizationId) { id name }
}`

// Ping verifies credentials and organization access, returning the organization name.
func (c *Client) Ping(ctx context.Context) (string, error) {
	var out struct {
		Organization *struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"organization"`
	}
	if err := c.do(ctx, viewerQuery, nil, &out); err != nil {
		return "", err
	}
	if out.Organization == nil {
		return "", fmt.Errorf("%w: organization %q", domain.ErrNotFound, c.orgID)
	}
	return out.Organization.Name, nil
}

const createDeploymentMutation = `
mutation RocketsledDeployInstance($organizationId: ID!, $id: ID!, $input: CreateDeploymentInput!) {
  createDeployment(organizationId: $organizationId, id: $id, input: $input) {
    successful
    messages { field message code }
    result { id }
  }
}`

// DeployInstance provisions one instance with exactly these params.
func (c *Client) DeployInstance(ctx context.Context, instanceID string, params map[string]any, message string) error {
	return c.createDeployment(ctx, instanceID, "PROVISION", params, message)
}

// DecommissionInstance tears down one instance. Massdriver still validates params, so
// the instance's current ones are sent.
func (c *Client) DecommissionInstance(ctx context.Context, instanceID string, params map[string]any, message string) error {
	return c.createDeployment(ctx, instanceID, "DECOMMISSION", params, message)
}

func (c *Client) createDeployment(ctx context.Context, instanceID, action string, params map[string]any, message string) error {
	var out struct {
		CreateDeployment payload[struct{ ID string }] `json:"createDeployment"`
	}
	vars := map[string]any{
		"id":    instanceID,
		"input": map[string]any{"action": action, "message": message, "params": mapArg(params)},
	}
	if err := c.do(ctx, createDeploymentMutation, vars, &out); err != nil {
		return err
	}
	return out.CreateDeployment.check("createDeployment")
}

const updateEnvironmentMutation = `
mutation RocketsledUpdateEnvironment($organizationId: ID!, $id: ID!, $input: UpdateEnvironmentInput!) {
  updateEnvironment(organizationId: $organizationId, id: $id, input: $input) {
    successful
    messages { field message code }
    result { id }
  }
}`

func (c *Client) SetEnvironmentDescription(ctx context.Context, environmentID, description string) error {
	var out struct {
		UpdateEnvironment payload[struct{ ID string }] `json:"updateEnvironment"`
	}
	vars := map[string]any{"id": environmentID, "input": map[string]any{"description": description}}
	if err := c.do(ctx, updateEnvironmentMutation, vars, &out); err != nil {
		return err
	}
	return out.UpdateEnvironment.check("updateEnvironment")
}

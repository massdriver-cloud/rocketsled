package massdriver

import (
	"context"
	"fmt"
	"strings"

	"github.com/massdriver-cloud/rocketsled/internal/domain"
)

const projectQuery = `
query RocketsledProject($organizationId: ID!, $id: ID!) {
  project(organizationId: $organizationId, id: $id) {
    id name description
    environments(cursor: { limit: 100 }) { items { id name } }
    components { id name ociRepo { name } }
  }
}`

type gqlProject struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Description  string `json:"description"`
	Environments struct {
		Items []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"items"`
	} `json:"environments"`
	Components []struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		OciRepo *struct {
			Name string `json:"name"`
		} `json:"ociRepo"`
	} `json:"components"`
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
		ID:          out.Project.ID,
		Name:        out.Project.Name,
		Description: out.Project.Description,
	}
	for _, e := range out.Project.Environments.Items {
		p.Environments = append(p.Environments, domain.EnvironmentSummary{ID: e.ID, Name: e.Name})
	}
	for _, comp := range out.Project.Components {
		component := domain.Component{Slug: localID(comp.ID, out.Project.ID), Name: comp.Name}
		if comp.OciRepo != nil {
			component.Bundle.Name = comp.OciRepo.Name
		}
		p.Components = append(p.Components, component)
	}
	return p, nil
}

// localID strips a parent identifier prefix: "shop-prod-api" under "shop-prod" is "api".
func localID(full, parent string) string {
	return strings.TrimPrefix(full, parent+"-")
}

const environmentQuery = `
query RocketsledEnvironment($organizationId: ID!, $id: ID!) {
  environment(organizationId: $organizationId, id: $id) {
    id name
    project { id }
    instances {
      id name status params paramsSchema resolvedVersion deployedVersion
      component { id }
      bundle { name }
    }
    unfulfilledDependencies { instance { id } field resourceType { id } }
  }
}`

type gqlEnvironment struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Project struct {
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
			ID string `json:"id"`
		} `json:"component"`
		Bundle *struct {
			Name string `json:"name"`
		} `json:"bundle"`
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

func (c *Client) GetEnvironment(ctx context.Context, environmentID string) (*domain.Environment, error) {
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

func toEnvironment(e *gqlEnvironment) *domain.Environment {
	env := &domain.Environment{
		ID:          e.ID,
		Name:        e.Name,
		ProjectID:   e.Project.ID,
		Instances:   make([]domain.PlatformInstance, 0, len(e.Instances)),
		Unfulfilled: make([]domain.UnfulfilledDependency, 0, len(e.UnfulfilledDependencies)),
	}
	for _, in := range e.Instances {
		pi := domain.PlatformInstance{
			ID:              in.ID,
			ComponentSlug:   localID(in.Component.ID, e.Project.ID),
			Name:            in.Name,
			Version:         in.ResolvedVersion,
			DeployedVersion: in.DeployedVersion,
			Status:          in.Status,
			Params:          in.Params,
			ParamsSchema:    in.ParamsSchema,
		}
		if in.Bundle != nil {
			pi.BundleName = in.Bundle.Name
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
    result {
      id name
      project { id }
      instances {
        id name status params paramsSchema resolvedVersion deployedVersion
        component { id }
        bundle { name }
      }
      unfulfilledDependencies { instance { id } field resourceType { id } }
    }
  }
}`

func (c *Client) ForkEnvironment(ctx context.Context, req domain.ForkRequest) (*domain.Environment, error) {
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
			"copyRemoteReferences":    req.CopyRemoteReferences,
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
		"input":         map[string]any{"overrides": overrides},
	}
	if err := c.do(ctx, copyInstanceMutation, vars, &out); err != nil {
		return err
	}
	return out.CopyInstance.check("copyInstance")
}

const createDeploymentMutation = `
mutation RocketsledCreateDeployment($organizationId: ID!, $id: ID!, $input: CreateDeploymentInput!) {
  createDeployment(organizationId: $organizationId, id: $id, input: $input) {
    successful
    messages { field message code }
    result { id status }
  }
}`

func (c *Client) CreateDeployment(ctx context.Context, instanceID string, params map[string]any, message string) (string, error) {
	var out struct {
		CreateDeployment payload[struct {
			ID string `json:"id"`
		}] `json:"createDeployment"`
	}
	vars := map[string]any{
		"id": instanceID,
		"input": map[string]any{
			"action":  "PROVISION",
			"message": message,
			"params":  params,
		},
	}
	if err := c.do(ctx, createDeploymentMutation, vars, &out); err != nil {
		return "", err
	}
	if err := out.CreateDeployment.check("createDeployment"); err != nil {
		return "", err
	}
	if out.CreateDeployment.Result == nil {
		return "", errNilResult
	}
	return out.CreateDeployment.Result.ID, nil
}

const deployEnvironmentMutation = `
mutation RocketsledDeployEnvironment($organizationId: ID!, $id: ID!) {
  deployEnvironment(organizationId: $organizationId, id: $id) {
    successful
    messages { field message code }
    result { id }
  }
}`

func (c *Client) DeployEnvironment(ctx context.Context, environmentID string) error {
	var out struct {
		DeployEnvironment payload[struct{ ID string }] `json:"deployEnvironment"`
	}
	if err := c.do(ctx, deployEnvironmentMutation, map[string]any{"id": environmentID}, &out); err != nil {
		return err
	}
	return out.DeployEnvironment.check("deployEnvironment")
}

const deploymentQuery = `
query RocketsledDeployment($organizationId: ID!, $id: UUID!) {
  deployment(organizationId: $organizationId, id: $id) {
    id status action message updatedAt
  }
}`

func (c *Client) GetDeployment(ctx context.Context, deploymentID string) (*domain.DeploymentSummary, error) {
	var out struct {
		Deployment *struct {
			ID        string `json:"id"`
			Status    string `json:"status"`
			Action    string `json:"action"`
			Message   string `json:"message"`
			UpdatedAt string `json:"updatedAt"`
		} `json:"deployment"`
	}
	if err := c.do(ctx, deploymentQuery, map[string]any{"id": deploymentID}, &out); err != nil {
		return nil, err
	}
	if out.Deployment == nil {
		return nil, &APIError{Code: "NOT_FOUND", Message: "deployment " + deploymentID + " not found"}
	}
	d := out.Deployment
	return &domain.DeploymentSummary{
		ID: d.ID, Status: domain.DeploymentStatus(d.Status), Action: d.Action, Message: d.Message, UpdatedAt: d.UpdatedAt,
	}, nil
}

const decommissionMutation = `
mutation RocketsledDecommission($organizationId: ID!, $id: ID!) {
  decommissionEnvironment(organizationId: $organizationId, id: $id) {
    successful
    messages { field message code }
    result { id }
  }
}`

func (c *Client) DecommissionEnvironment(ctx context.Context, environmentID string) error {
	var out struct {
		DecommissionEnvironment payload[struct{ ID string }] `json:"decommissionEnvironment"`
	}
	if err := c.do(ctx, decommissionMutation, map[string]any{"id": environmentID}, &out); err != nil {
		return err
	}
	return out.DecommissionEnvironment.check("decommissionEnvironment")
}

const deleteMutation = `
mutation RocketsledDelete($organizationId: ID!, $id: ID!) {
  deleteEnvironment(organizationId: $organizationId, id: $id) {
    successful
    messages { field message code }
    result { id }
  }
}`

func (c *Client) DeleteEnvironment(ctx context.Context, environmentID string) error {
	var out struct {
		DeleteEnvironment payload[struct{ ID string }] `json:"deleteEnvironment"`
	}
	if err := c.do(ctx, deleteMutation, map[string]any{"id": environmentID}, &out); err != nil {
		return err
	}
	return out.DeleteEnvironment.check("deleteEnvironment")
}

const viewerQuery = `
query RocketsledViewer($organizationId: ID!) {
  organization(organizationId: $organizationId) { id name }
  viewer {
    __typename
    ... on AccountViewer { id email }
    ... on ServiceAccountViewer { id name }
  }
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

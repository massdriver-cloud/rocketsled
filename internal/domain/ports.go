package domain

import (
	"context"
	"io"
	"net/http"
)

//go:generate go tool moq -pkg mocks -out mocks/mocks.go -stub . Platform SCM Store Builder

// Platform is everything Rocketsled needs from Massdriver.
type Platform interface {
	// GetProject returns the project's blueprint components and environments.
	GetProject(ctx context.Context, projectID string) (*Project, error)
	// GetEnvironment returns the environment with each instance's params, schema and status.
	GetEnvironment(ctx context.Context, environmentID string) (*Environment, error)
	// ForkEnvironment creates (or converges) an environment seeded from a parent.
	ForkEnvironment(ctx context.Context, req ForkRequest) (*Environment, error)
	// CopyInstance deep-merges overrides onto the source instance's params and writes
	// the result to the destination without deploying.
	CopyInstance(ctx context.Context, sourceID, destinationID string, overrides map[string]any) error
	// CreateDeployment saves params on an instance and provisions it, returning the deployment ID.
	CreateDeployment(ctx context.Context, instanceID string, params map[string]any, message string) (string, error)
	// DeployEnvironment provisions every instance in dependency order.
	DeployEnvironment(ctx context.Context, environmentID string) error
	// GetDeployment returns one deployment's state.
	GetDeployment(ctx context.Context, deploymentID string) (*DeploymentSummary, error)
	// DecommissionEnvironment tears down every instance in reverse dependency order.
	DecommissionEnvironment(ctx context.Context, environmentID string) error
	// DeleteEnvironment removes an environment whose instances are all decommissioned.
	DeleteEnvironment(ctx context.Context, environmentID string) error
}

// Project is a Massdriver project: the parity boundary a stack links to.
type Project struct {
	ID           string               `json:"id"`
	Name         string               `json:"name"`
	Description  string               `json:"description,omitempty"`
	Components   []Component          `json:"components"`
	Environments []EnvironmentSummary `json:"environments"`
}

// Component is one node in a project's blueprint.
type Component struct {
	// Slug is the component's local identifier within the project.
	Slug string `json:"slug"`
	Name string `json:"name"`
	// Bundle is filled from the base environment's instance of this component.
	Bundle Bundle `json:"bundle"`
}

// Bundle carries what Rocketsled needs from a bundle: its presets and params schema.
type Bundle struct {
	Name         string         `json:"name"`
	Version      string         `json:"version"`
	Presets      []Preset       `json:"presets"`
	ParamsSchema map[string]any `json:"-"`
}

// Preset is a named example from the bundle's params.examples.
type Preset struct {
	Name   string         `json:"name"`
	Params map[string]any `json:"params"`
}

// EnvironmentSummary lists an environment on a project.
type EnvironmentSummary struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Environment is a deploy target with one instance per blueprint component.
type Environment struct {
	ID        string             `json:"id"`
	Name      string             `json:"name"`
	ProjectID string             `json:"projectId"`
	Instances []PlatformInstance `json:"instances"`
	// Unfulfilled lists required connection slots nothing fills; a deploy blocks on each.
	Unfulfilled []UnfulfilledDependency `json:"unfulfilled"`
}

// UnfulfilledDependency is a required input no link, reference or default satisfies.
type UnfulfilledDependency struct {
	InstanceID   string `json:"instanceId"`
	Field        string `json:"field"`
	ResourceType string `json:"resourceType"`
}

// PlatformInstance is a Massdriver instance (a component realized in an environment).
type PlatformInstance struct {
	ID              string         `json:"id"`
	ComponentSlug   string         `json:"componentSlug"`
	Name            string         `json:"name"`
	BundleName      string         `json:"bundleName"`
	Version         string         `json:"version"`
	DeployedVersion string         `json:"deployedVersion,omitempty"`
	Status          string         `json:"status"`
	Params          map[string]any `json:"params,omitempty"`
	ParamsSchema    map[string]any `json:"-"`
}

// Presets extracts the bundle's params.examples from the instance's schema.
func (pi PlatformInstance) Presets() []Preset {
	raw, _ := pi.ParamsSchema["examples"].([]any)
	out := make([]Preset, 0, len(raw))
	for _, item := range raw {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name, _ := m[PresetNameKey].(string)
		if name == "" {
			continue
		}
		out = append(out, Preset{Name: name, Params: m})
	}
	return out
}

// DeploymentSummary is the state of a Massdriver deployment.
type DeploymentSummary struct {
	ID        string           `json:"id"`
	Status    DeploymentStatus `json:"status"`
	Action    string           `json:"action"`
	Message   string           `json:"message,omitempty"`
	UpdatedAt string           `json:"updatedAt,omitempty"`
}

// DeploymentStatus mirrors Massdriver's DeploymentStatus enum.
type DeploymentStatus string

// ForkRequest describes an environment fork.
type ForkRequest struct {
	ParentEnvironmentID     string
	ID                      string
	Name                    string
	Description             string
	CopyEnvironmentDefaults bool
	CopySecrets             bool
	CopyRemoteReferences    bool
}

// SCM is a source-control provider: OAuth login, repository access and webhooks.
type SCM interface {
	Provider() Provider
	AuthCodeURL(state, redirectURL string) string
	Exchange(ctx context.Context, code, redirectURL string) (string, error)
	CurrentUser(ctx context.Context, token string) (*User, error)
	ListRepos(ctx context.Context, token string) ([]Repo, error)
	GetRepo(ctx context.Context, token, fullName string) (*Repo, error)
	BranchHead(ctx context.Context, token, fullName, branch string) (string, error)
	CreateWebhook(ctx context.Context, token, fullName, callbackURL, secret string) (string, error)
	DeleteWebhook(ctx context.Context, token, fullName, hookID string) error
	// ParsePush authenticates and decodes a push webhook. It returns (nil, nil) for
	// events that are not pushes.
	ParsePush(r *http.Request, secret string) (*PushEvent, error)
	// CloneURL returns an HTTPS clone URL that embeds the token when the repo needs it.
	CloneURL(repo Repo, token string) string
}

// Builder turns a directory of a repository at a commit into a pushed container image.
type Builder interface {
	Build(ctx context.Context, req BuildRequest) (*BuildResult, error)
}

// BuildRequest describes one image build.
type BuildRequest struct {
	CloneURL string
	SHA      string
	// Path is the build context inside the repository; "" or "." means the root.
	Path string
	// Image is the full reference to build and push, e.g. registry/repo:tag.
	Image string
	// Log receives the combined build output.
	Log io.Writer
}

// BuildResult reports what was pushed.
type BuildResult struct {
	Image  string
	Digest string
}

// Store persists Rocketsled's own state.
type Store interface {
	UpsertUser(ctx context.Context, u *User) error
	GetUser(ctx context.Context, id string) (*User, error)
	SaveCredential(ctx context.Context, userID, token string) error
	GetCredential(ctx context.Context, userID string) (string, error)

	CreateStack(ctx context.Context, s *Stack) error
	GetStack(ctx context.Context, id string) (*Stack, error)
	ListStacks(ctx context.Context) ([]Stack, error)
	FindStacksByRepo(ctx context.Context, provider Provider, fullName string) ([]Stack, error)
	DeleteStack(ctx context.Context, id string) error

	CreateInstance(ctx context.Context, i *Instance) error
	GetInstance(ctx context.Context, id string) (*Instance, error)
	ListInstances(ctx context.Context, stackID string) ([]Instance, error)
	UpdateInstance(ctx context.Context, i *Instance) error
	DeleteInstance(ctx context.Context, id string) error

	CreateDeploy(ctx context.Context, d *Deploy) error
	UpdateDeploy(ctx context.Context, d *Deploy) error
	ListDeploys(ctx context.Context, instanceID string, limit int) ([]Deploy, error)
}

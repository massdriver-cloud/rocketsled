package domain

import (
	"context"
	"io"
	"time"
)

//go:generate go tool moq -pkg mocks -out mocks/mocks.go -stub . Platform SCM Store Installer

// Platform is everything Rocketsled needs from one Massdriver organization. A
// Platform is bound to a Credential; see PlatformFactory.
type Platform interface {
	// Ping verifies the credential and returns the organization's display name.
	Ping(ctx context.Context) (string, error)
	// CreateOrganization makes a new organization. The credential must be a user's.
	CreateOrganization(ctx context.Context, id, name string) error
	// CreateServiceAccount makes an admin service account and returns its token.
	CreateServiceAccount(ctx context.Context, name string) (string, error)
	// GetProject returns the blueprint and environments.
	GetProject(ctx context.Context, projectID string) (*Project, error)
	// GetEnvironment returns an environment with each instance's params, schema, status and resources.
	GetEnvironment(ctx context.Context, environmentID string) (*PlatformEnvironment, error)
	// ForkEnvironment creates (or converges) an environment seeded from a parent.
	ForkEnvironment(ctx context.Context, req ForkRequest) (*PlatformEnvironment, error)
	// CopyInstance deep-merges overrides onto the source's params and writes them to the destination.
	CopyInstance(ctx context.Context, sourceID, destinationID string, overrides map[string]any) error
	// DeployInstance provisions one instance with exactly these params.
	DeployInstance(ctx context.Context, instanceID string, params map[string]any, message string) error
	// DeployEnvironment provisions every instance in dependency order.
	DeployEnvironment(ctx context.Context, environmentID string) error
	// DecommissionEnvironment tears down every instance in reverse dependency order.
	DecommissionEnvironment(ctx context.Context, environmentID string) error
	// DeleteEnvironment removes an environment whose instances are all decommissioned.
	DeleteEnvironment(ctx context.Context, environmentID string) error
	// CreateResource imports a resource of a type (a credential, typically) and returns its ID.
	CreateResource(ctx context.Context, resourceTypeID, name string, payload map[string]any) (string, error)
	// GrantResource shares a resource with every environment so it can be a default.
	GrantResource(ctx context.Context, resourceID string) error
	// SetEnvironmentDefault pins a resource as the environment's default of its type.
	SetEnvironmentDefault(ctx context.Context, environmentID, resourceID string) error
}

// PlatformFactory builds a Platform for a credential.
type PlatformFactory func(Credential) Platform

// Project is a Massdriver project.
type Project struct {
	ID           string               `json:"id"`
	Name         string               `json:"name"`
	Description  string               `json:"description,omitempty"`
	Components   []Component          `json:"components"`
	Links        []Link               `json:"links"`
	Environments []EnvironmentSummary `json:"environments"`
}

// Component is one node in a project's blueprint.
type Component struct {
	Slug string `json:"slug"`
	Name string `json:"name"`
	// Bundle is filled from the base environment's instance of this component.
	Bundle Bundle `json:"bundle"`
}

// Link wires one component's output into another's input.
type Link struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// Bundle carries what Rocketsled needs from a bundle: presets and schema.
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
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
}

// PlatformEnvironment is a Massdriver environment with its instances.
type PlatformEnvironment struct {
	ID          string                  `json:"id"`
	Name        string                  `json:"name"`
	ProjectID   string                  `json:"projectId"`
	Instances   []PlatformInstance      `json:"instances"`
	Unfulfilled []UnfulfilledDependency `json:"unfulfilled"`
}

// UnfulfilledDependency is a required input no link, reference or default satisfies.
type UnfulfilledDependency struct {
	InstanceID   string `json:"instanceId"`
	Field        string `json:"field"`
	ResourceType string `json:"resourceType"`
}

// PlatformInstance is a component realized in an environment.
type PlatformInstance struct {
	ID              string             `json:"id"`
	ComponentSlug   string             `json:"componentSlug"`
	ComponentName   string             `json:"componentName"`
	Name            string             `json:"name"`
	BundleName      string             `json:"bundleName"`
	Version         string             `json:"version"`
	DeployedVersion string             `json:"deployedVersion,omitempty"`
	Status          string             `json:"status"`
	Params          map[string]any     `json:"params,omitempty"`
	ParamsSchema    map[string]any     `json:"-"`
	Resources       []InstanceResource `json:"resources"`
}

// InstanceResource is something an instance produced, with its (masked) payload.
type InstanceResource struct {
	Field        string         `json:"field"`
	Name         string         `json:"name"`
	ResourceType string         `json:"resourceType"`
	Payload      map[string]any `json:"payload"`
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

// ForkRequest describes an environment fork.
type ForkRequest struct {
	ParentEnvironmentID     string
	ID                      string
	Name                    string
	Description             string
	CopyEnvironmentDefaults bool
	CopySecrets             bool
}

// SCM is a sign-in provider.
type SCM interface {
	Provider() Provider
	AuthCodeURL(state, redirectURL string) string
	Exchange(ctx context.Context, code, redirectURL string) (string, error)
	CurrentUser(ctx context.Context, token string) (*User, error)
}

// Installer publishes a catalog into an organization and applies its bootstrap.
type Installer interface {
	Install(ctx context.Context, req InstallRequest) (*InstallResult, error)
}

// InstallRequest is one catalog install.
type InstallRequest struct {
	Catalog    *Catalog
	Credential Credential
	// WorkDir is a private directory for this account's bootstrap working files.
	WorkDir string
	// State is the previous OpenTofu state for the bootstrap, if any.
	State []byte
	// Stable publishes bundles as stable releases; otherwise the development channel.
	Stable bool
	Log    io.Writer
}

// InstallResult reports what the bootstrap created.
type InstallResult struct {
	ProjectID    string
	Environments map[string]string
	Components   map[string]string
	// State is the OpenTofu state after apply, kept with the account.
	State []byte
}

// Store persists Rocketsled's own state.
type Store interface {
	UpsertUser(ctx context.Context, u *User) error
	GetUser(ctx context.Context, id string) (*User, error)

	CreateAccount(ctx context.Context, a *Account, credential Credential) error
	GetAccount(ctx context.Context, id string) (*Account, error)
	GetAccountByOwner(ctx context.Context, userID string) (*Account, error)
	ListAccounts(ctx context.Context, status AccountStatus) ([]Account, error)
	UpdateAccount(ctx context.Context, a *Account) error
	GetAccountCredential(ctx context.Context, accountID string) (Credential, error)
	SaveBootstrapState(ctx context.Context, accountID string, state []byte) error
	GetBootstrapState(ctx context.Context, accountID string) ([]byte, error)

	CreateEnvironment(ctx context.Context, e *Environment) error
	GetEnvironment(ctx context.Context, id string) (*Environment, error)
	ListEnvironments(ctx context.Context, accountID string) ([]Environment, error)
	UpdateEnvironment(ctx context.Context, e *Environment) error
	DeleteEnvironment(ctx context.Context, id string) error

	CreateRun(ctx context.Context, r *Run) error
	UpdateRun(ctx context.Context, r *Run) error
	ListRuns(ctx context.Context, environmentID string, limit int) ([]Run, error)
}

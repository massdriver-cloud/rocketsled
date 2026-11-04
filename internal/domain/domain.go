// Package domain holds Rocketsled's model: an account is a customer with their own
// Massdriver organization; a catalog is a PaaS they install into it; environments
// are deployed copies of the catalog's project configured from bundle presets.
package domain

import (
	"errors"
	"fmt"
	"regexp"
	"time"
)

// Provider identifies a sign-in provider.
type Provider string

const (
	ProviderGitHub Provider = "github"
	ProviderGitLab Provider = "gitlab"
	// ProviderDev is a local sign-in stub with no OAuth application behind it.
	ProviderDev Provider = "dev"
)

// User is someone who signed in.
type User struct {
	ID             string    `json:"id"`
	Provider       Provider  `json:"provider"`
	ProviderUserID string    `json:"providerUserId"`
	Login          string    `json:"login"`
	Name           string    `json:"name"`
	Email          string    `json:"email,omitempty"`
	AvatarURL      string    `json:"avatarUrl,omitempty"`
	CreatedAt      time.Time `json:"createdAt"`
}

// AccountStatus is where a customer is in onboarding.
type AccountStatus string

const (
	// AccountStatusNew has an organization but no catalog yet.
	AccountStatusNew AccountStatus = "new"
	// AccountStatusInstalling is publishing the catalog and bootstrapping the project.
	AccountStatusInstalling AccountStatus = "installing"
	// AccountStatusNeedsCredentials has a project but is missing a cloud credential.
	AccountStatusNeedsCredentials AccountStatus = "needs_credentials" //nolint:gosec // a status name, not a secret
	// AccountStatusReady can create environments.
	AccountStatusReady AccountStatus = "ready"
	// AccountStatusFailed could not install its catalog; the log says why.
	AccountStatusFailed AccountStatus = "failed"
)

// Account is a Rocketsled customer: one person, one Massdriver organization.
type Account struct {
	ID               string        `json:"id"`
	Name             string        `json:"name"`
	OwnerUserID      string        `json:"ownerUserId"`
	OrganizationID   string        `json:"organizationId"`
	OrganizationName string        `json:"organizationName"`
	Status           AccountStatus `json:"status"`
	CatalogID        string        `json:"catalogId,omitempty"`
	// ProjectID and BaseEnvironmentID come from the catalog bootstrap outputs.
	ProjectID         string `json:"projectId,omitempty"`
	BaseEnvironmentID string `json:"baseEnvironmentId,omitempty"`
	// Platforms maps a platform resource type to the resource the customer provided.
	Platforms    map[string]string `json:"platforms"`
	InstallLog   string            `json:"installLog,omitempty"`
	InstallError string            `json:"installError,omitempty"`
	CreatedAt    time.Time         `json:"createdAt"`
	UpdatedAt    time.Time         `json:"updatedAt"`
}

// Credential authenticates Rocketsled to one Massdriver organization.
type Credential struct {
	URL            string
	OrganizationID string
	APIKey         string
}

// Catalog is a PaaS a customer can install: credential types to collect, things to
// publish, and a bootstrap that lays out the first project.
type Catalog struct {
	ID            string         `json:"id"`
	Name          string         `json:"name"`
	Cloud         string         `json:"cloud"`
	Tagline       string         `json:"tagline"`
	Description   string         `json:"description"`
	Platforms     []PlatformType `json:"platforms"`
	ResourceTypes []string       `json:"resourceTypes"`
	Bundles       []string       `json:"bundles"`
	Bootstrap     Bootstrap      `json:"bootstrap"`
	// Dir is the catalog's directory on disk.
	Dir string `json:"-"`
}

// Bootstrap describes what the catalog's bootstrap creates.
type Bootstrap struct {
	Project         string `json:"project"`
	BaseEnvironment string `json:"baseEnvironment"`
	AppComponent    string `json:"appComponent,omitempty"`
	ImageParam      string `json:"imageParam,omitempty"`
}

// PlatformType is a credential resource type the customer fills in by hand.
type PlatformType struct {
	ID           string         `json:"id"`
	Label        string         `json:"label"`
	Description  string         `json:"description"`
	Schema       map[string]any `json:"schema"`
	UIOrder      []string       `json:"uiOrder,omitempty"`
	Instructions []Instruction  `json:"instructions"`
}

// Instruction is a how-to for obtaining a credential.
type Instruction struct {
	Label    string `json:"label"`
	Markdown string `json:"markdown"`
}

// EnvironmentState is Rocketsled's coarse view of an environment's lifecycle.
type EnvironmentState string

const (
	EnvironmentStateDeploying       EnvironmentState = "deploying"
	EnvironmentStateRunning         EnvironmentState = "running"
	EnvironmentStateFailed          EnvironmentState = "failed"
	EnvironmentStateDecommissioning EnvironmentState = "decommissioning"
)

// Environment is a copy of the catalog's project the customer created.
type Environment struct {
	ID            string            `json:"id"`
	AccountID     string            `json:"accountId"`
	Slug          string            `json:"slug"`
	Name          string            `json:"name"`
	EnvironmentID string            `json:"environmentId"`
	Presets       map[string]string `json:"presets"`
	State         EnvironmentState  `json:"state"`
	CreatedBy     string            `json:"createdBy"`
	CreatedAt     time.Time         `json:"createdAt"`
	UpdatedAt     time.Time         `json:"updatedAt"`
}

// RunTrigger records what caused a run.
type RunTrigger string

const (
	TriggerCreate RunTrigger = "create"
	TriggerDeploy RunTrigger = "deploy"
)

// RunStatus is Rocketsled's record of a deploy request.
type RunStatus string

const (
	RunStatusQueued    RunStatus = "queued"
	RunStatusRunning   RunStatus = "running"
	RunStatusSucceeded RunStatus = "succeeded"
	RunStatusFailed    RunStatus = "failed"
)

// Run is a numbered deploy of an environment.
type Run struct {
	ID            string     `json:"id"`
	EnvironmentID string     `json:"environmentId"`
	Number        int        `json:"number"`
	Trigger       RunTrigger `json:"trigger"`
	Status        RunStatus  `json:"status"`
	Message       string     `json:"message,omitempty"`
	RequestedBy   string     `json:"requestedBy,omitempty"`
	CreatedAt     time.Time  `json:"createdAt"`
	UpdatedAt     time.Time  `json:"updatedAt"`
}

// Sentinel errors. Handlers map these to HTTP statuses.
var (
	ErrNotFound  = errors.New("not found")
	ErrForbidden = errors.New("forbidden")
	ErrConflict  = errors.New("conflict")
)

// ValidationError describes a rejected input field.
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

// Invalid builds a ValidationError.
func Invalid(field, format string, args ...any) error {
	return &ValidationError{Field: field, Message: fmt.Sprintf(format, args...)}
}

// Massdriver environment local identifiers are lowercase alphanumeric, 1-20 chars.
var slugPattern = regexp.MustCompile(`^[a-z0-9]{1,20}$`)

// ValidSlug reports whether s can be used as a Massdriver environment ID segment.
func ValidSlug(s string) bool { return slugPattern.MatchString(s) }

// Package domain holds Rocketsled's core model: stacks (a repository linked to a
// Massdriver project), stack instances (a forked Massdriver environment tracking a
// branch) and deploys. It depends on nothing but the ports declared in ports.go.
package domain

import (
	"errors"
	"fmt"
	"regexp"
	"time"
)

// Provider identifies a source-control host.
type Provider string

const (
	ProviderGitHub Provider = "github"
	ProviderGitLab Provider = "gitlab"
	// ProviderDev is a local sign-in stub with no OAuth application behind it.
	ProviderDev Provider = "dev"
)

// User is someone who signed in through an SCM provider.
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

// Repo is a source repository as seen through its provider.
type Repo struct {
	Provider      Provider `json:"provider"`
	ExternalID    string   `json:"externalId"`
	FullName      string   `json:"fullName"`
	URL           string   `json:"url"`
	DefaultBranch string   `json:"defaultBranch"`
	Private       bool     `json:"private"`
}

// Stack links a repository to a Massdriver project. Every instance of the stack is
// an environment forked inside that project.
type Stack struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Repo Repo   `json:"repo"`
	// Path is the directory inside the repository holding the app's Dockerfile.
	Path      string    `json:"path"`
	ProjectID string    `json:"projectId"`
	WebhookID string    `json:"-"`
	CreatedBy string    `json:"createdBy"`
	CreatedAt time.Time `json:"createdAt"`
	// DeployTokenHash authenticates CI callbacks; the plaintext is shown once at creation.
	DeployTokenHash string `json:"-"`
}

// InstanceState is Rocketsled's coarse view of an instance's lifecycle. Live
// per-package status always comes from Massdriver.
type InstanceState string

const (
	InstanceStateLaunching       InstanceState = "launching"
	InstanceStateRunning         InstanceState = "running"
	InstanceStateDecommissioning InstanceState = "decommissioning"
	InstanceStateFailed          InstanceState = "failed"
)

// Instance is one running copy of a stack: a Massdriver environment forked from the
// project's base environment, configured from presets, following a git branch.
type Instance struct {
	ID            string            `json:"id"`
	StackID       string            `json:"stackId"`
	Slug          string            `json:"slug"`
	Name          string            `json:"name"`
	Branch        string            `json:"branch"`
	EnvironmentID string            `json:"environmentId"`
	Presets       map[string]string `json:"presets"`
	State         InstanceState     `json:"state"`
	CreatedBy     string            `json:"createdBy"`
	CreatedAt     time.Time         `json:"createdAt"`
	UpdatedAt     time.Time         `json:"updatedAt"`
}

// DeployTrigger records what caused a deploy.
type DeployTrigger string

const (
	TriggerLaunch  DeployTrigger = "launch"
	TriggerManual  DeployTrigger = "manual"
	TriggerWebhook DeployTrigger = "webhook"
	TriggerCI      DeployTrigger = "ci"
)

// DeployStatus is Rocketsled's record of a deploy request. Massdriver owns the
// authoritative per-package status.
type DeployStatus string

const (
	DeployStatusQueued    DeployStatus = "queued"
	DeployStatusBuilding  DeployStatus = "building"
	DeployStatusRunning   DeployStatus = "running"
	DeployStatusSucceeded DeployStatus = "succeeded"
	DeployStatusFailed    DeployStatus = "failed"
)

// Deploy is a numbered run of an instance.
type Deploy struct {
	ID         string        `json:"id"`
	InstanceID string        `json:"instanceId"`
	Number     int           `json:"number"`
	Trigger    DeployTrigger `json:"trigger"`
	Ref        string        `json:"ref"`
	SHA        string        `json:"sha"`
	ImageTag   string        `json:"imageTag"`
	// Image is the full image reference that was built, when Rocketsled built one.
	Image   string       `json:"image,omitempty"`
	Status  DeployStatus `json:"status"`
	Message string       `json:"message,omitempty"`
	// Log holds build output, capped by the service.
	Log string `json:"log,omitempty"`
	// PlatformDeploymentIDs are the Massdriver deployment IDs this run fanned out to.
	PlatformDeploymentIDs []string  `json:"platformDeploymentIds"`
	RequestedBy           string    `json:"requestedBy,omitempty"`
	CreatedAt             time.Time `json:"createdAt"`
	UpdatedAt             time.Time `json:"updatedAt"`
}

// PushEvent is a normalized SCM push webhook.
type PushEvent struct {
	Provider     Provider
	RepoFullName string
	Branch       string
	SHA          string
	Deleted      bool
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
var instanceSlugPattern = regexp.MustCompile(`^[a-z0-9]{1,20}$`)

// ValidInstanceSlug reports whether s can be used as a Massdriver environment ID segment.
func ValidInstanceSlug(s string) bool { return instanceSlugPattern.MatchString(s) }

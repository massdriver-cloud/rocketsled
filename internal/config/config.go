// Package config loads and validates the rocketsled.yaml configuration file.
package config

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

var projectIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// Config is the fully-resolved server configuration.
type Config struct {
	Server     Server     `yaml:"server"`
	Database   Database   `yaml:"database"`
	Massdriver Massdriver `yaml:"massdriver"`
	Auth       Auth       `yaml:"auth"`
	Builder    Builder    `yaml:"builder"`
}

// Builder configures how Rocketsled turns a commit into a container image. With no
// builder, projects must not set image_registry and images are built elsewhere.
type Builder struct {
	Docker *DockerBuilder `yaml:"docker"`
}

// DockerBuilder shells out to the local git and docker CLIs.
type DockerBuilder struct {
	// Platform is the image platform; Lambda accepts linux/amd64 or linux/arm64.
	Platform string `yaml:"platform"`
	// LoginCommand runs through `sh -c` before each push, e.g. an ECR login.
	LoginCommand string `yaml:"login_command"`
}

type Server struct {
	// Addr is the listen address, e.g. ":8080".
	Addr string `yaml:"addr"`
	// BaseURL is the externally reachable URL, used for OAuth callbacks and webhooks.
	BaseURL string `yaml:"base_url"`
	// SessionSecret signs session cookies and encrypts stored SCM tokens. 32+ random bytes.
	SessionSecret string `yaml:"session_secret"`
	// SessionTTL bounds how long a login lasts.
	SessionTTL time.Duration `yaml:"session_ttl"`
}

type Database struct {
	// Path is the SQLite file. ":memory:" is accepted for tests.
	Path string `yaml:"path"`
}

type Massdriver struct {
	URL            string    `yaml:"url"`
	OrganizationID string    `yaml:"organization_id"`
	APIKey         string    `yaml:"api_key"`
	Projects       []Project `yaml:"projects"`
}

// Project is a Massdriver project an operator has opted into Rocketsled.
// Each stack instance becomes a fork of BaseEnvironment inside this project.
type Project struct {
	ID              string `yaml:"id"`
	BaseEnvironment string `yaml:"base_environment"`
	// AppComponent is the blueprint component whose image tag tracks the linked repo.
	// When empty, Rocketsled picks the first component whose params expose ImageTagParam.
	AppComponent string `yaml:"app_component"`
	// ImageTagParam is the dotted path of the container image tag param on AppComponent.
	ImageTagParam string `yaml:"image_tag_param"`
	// ImageRepositoryParam optionally receives the image repository (registry/name) on each deploy.
	ImageRepositoryParam string `yaml:"image_repository_param"`
	// ImageRepository is the value written to ImageRepositoryParam. Supports {{owner}} and {{repo}}.
	ImageRepository string `yaml:"image_repository"`
	// ImageRegistry, when set, makes Rocketsled build the stack's Dockerfile and push
	// to this repository as <registry>:<sha>. Supports {{owner}}, {{repo}} and {{stack}}.
	ImageRegistry string `yaml:"image_registry"`
	// CopySecrets carries the base environment's instance secrets into each fork.
	CopySecrets bool `yaml:"copy_secrets"`
	// CopyRemoteReferences carries the base environment's remote references into each fork.
	CopyRemoteReferences bool `yaml:"copy_remote_references"`
}

type Auth struct {
	// Admins lists SCM logins (provider:login, or bare login) allowed to link stacks to projects.
	Admins    []string       `yaml:"admins"`
	Providers AuthProviders  `yaml:"providers"`
	Webhooks  WebhookOptions `yaml:"webhooks"`
}

type AuthProviders struct {
	GitHub *OAuthProvider `yaml:"github"`
	GitLab *OAuthProvider `yaml:"gitlab"`
	// Dev signs in a fixed user with no OAuth app. Local development only.
	Dev *DevProvider `yaml:"dev"`
}

// DevProvider configures the local sign-in stub.
type DevProvider struct {
	// Login is the username shown in the UI.
	Login string `yaml:"login"`
	// Repos lists public GitHub repositories (owner/name) offered for linking.
	Repos []string `yaml:"repos"`
}

type OAuthProvider struct {
	ClientID     string `yaml:"client_id"`
	ClientSecret string `yaml:"client_secret"`
	// BaseURL overrides the provider host for self-managed installs (GitHub Enterprise, self-hosted GitLab).
	BaseURL string `yaml:"base_url"`
}

type WebhookOptions struct {
	// Secret is shared with GitHub/GitLab so push events can be authenticated.
	Secret string `yaml:"secret"`
}

// Load reads a YAML file, expands ${VAR} references from the process environment,
// applies defaults and validates the result.
func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path) //nolint:gosec // operator-supplied path
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	return Parse(raw)
}

// Parse decodes YAML bytes. See Load.
func Parse(raw []byte) (*Config, error) {
	expanded := os.ExpandEnv(string(raw))
	cfg := Default()
	if err := yaml.Unmarshal([]byte(expanded), cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	cfg.applyDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// Default returns a config with every optional field populated.
func Default() *Config {
	return &Config{
		Server: Server{
			Addr:       ":8080",
			BaseURL:    "http://localhost:8080",
			SessionTTL: 7 * 24 * time.Hour,
		},
		Database:   Database{Path: "rocketsled.db"},
		Massdriver: Massdriver{URL: "https://api.massdriver.cloud"},
	}
}

func (c *Config) applyDefaults() {
	for i := range c.Massdriver.Projects {
		p := &c.Massdriver.Projects[i]
		if p.ImageTagParam == "" {
			p.ImageTagParam = "image.tag"
		}
	}
	if c.Builder.Docker != nil && c.Builder.Docker.Platform == "" {
		c.Builder.Docker.Platform = "linux/amd64"
	}
	c.Server.BaseURL = strings.TrimRight(c.Server.BaseURL, "/")
	c.Massdriver.URL = strings.TrimRight(c.Massdriver.URL, "/")
}

// Validate reports every configuration problem at once.
func (c *Config) Validate() error {
	var errs []error
	if len(c.Server.SessionSecret) < 32 {
		errs = append(errs, errors.New("server.session_secret must be at least 32 characters"))
	}
	if c.Massdriver.OrganizationID == "" {
		errs = append(errs, errors.New("massdriver.organization_id is required"))
	}
	if c.Massdriver.APIKey == "" {
		errs = append(errs, errors.New("massdriver.api_key is required"))
	}
	if len(c.Massdriver.Projects) == 0 {
		errs = append(errs, errors.New("massdriver.projects must list at least one project"))
	}
	seen := map[string]bool{}
	for i, p := range c.Massdriver.Projects {
		switch {
		case p.ID == "":
			errs = append(errs, fmt.Errorf("massdriver.projects[%d].id is required", i))
		case !projectIDPattern.MatchString(p.ID):
			errs = append(errs, fmt.Errorf("massdriver.projects[%d].id %q is not a valid project identifier", i, p.ID))
		case seen[p.ID]:
			errs = append(errs, fmt.Errorf("massdriver.projects[%d].id %q is listed twice", i, p.ID))
		}
		seen[p.ID] = true
		if p.BaseEnvironment == "" {
			errs = append(errs, fmt.Errorf("massdriver.projects[%d].base_environment is required", i))
		}
		if p.ImageRepositoryParam != "" && p.ImageRepository == "" {
			errs = append(errs, fmt.Errorf("massdriver.projects[%d].image_repository is required when image_repository_param is set", i))
		}
		if p.ImageRegistry != "" && c.Builder.Docker == nil {
			errs = append(errs, fmt.Errorf("massdriver.projects[%d].image_registry needs builder.docker to be configured", i))
		}
	}
	if c.Auth.Providers.GitHub == nil && c.Auth.Providers.GitLab == nil && c.Auth.Providers.Dev == nil {
		errs = append(errs, errors.New("auth.providers must configure github, gitlab, or dev"))
	}
	if c.Auth.Providers.Dev != nil && strings.HasPrefix(c.Server.BaseURL, "https://") {
		errs = append(errs, errors.New("auth.providers.dev is for local use; remove it when serving over https"))
	}
	for name, p := range map[string]*OAuthProvider{"github": c.Auth.Providers.GitHub, "gitlab": c.Auth.Providers.GitLab} {
		if p == nil {
			continue
		}
		if p.ClientID == "" || p.ClientSecret == "" {
			errs = append(errs, fmt.Errorf("auth.providers.%s needs client_id and client_secret", name))
		}
	}
	if c.Auth.Webhooks.Secret == "" {
		errs = append(errs, errors.New("auth.webhooks.secret is required"))
	}
	return errors.Join(errs...)
}

// Project finds an opted-in project by ID.
func (c *Config) Project(id string) (Project, bool) {
	for _, p := range c.Massdriver.Projects {
		if p.ID == id {
			return p, true
		}
	}
	return Project{}, false
}

// IsAdmin reports whether an SCM login may link repositories to projects.
// Entries may be "login" (any provider) or "provider:login".
func (c *Config) IsAdmin(provider, login string) bool {
	for _, entry := range c.Auth.Admins {
		if entry == login || entry == provider+":"+login {
			return true
		}
	}
	return false
}

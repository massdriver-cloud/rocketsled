// Package config loads and validates rocketsled.yaml.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/massdriver-cloud/rocketsled/internal/masscfg"
)

// Config is the fully-resolved server configuration.
type Config struct {
	Server     Server     `yaml:"server"`
	Database   Database   `yaml:"database"`
	DataDir    string     `yaml:"data_dir"`
	Massdriver Massdriver `yaml:"massdriver"`
	Catalogs   Catalogs   `yaml:"catalogs"`
	Tools      Tools      `yaml:"tools"`
	Auth       Auth       `yaml:"auth"`
	// Skin is a directory with skin.json; relative paths resolve against the config file.
	Skin string `yaml:"skin"`
	// PaaS describes what customers can make in tenant mode.
	PaaS *PaaS `yaml:"paas"`
}

// PaaS is a tenant-mode server's offer: runtimes forked from templates, add-ons
// deployed only when picked, and params no preset can know.
type PaaS struct {
	Runtimes []Runtime `yaml:"runtimes"`
	// Pinned params apply over every preset, keyed by component. String values may
	// use {{environment_id}}, {{app}} and {{runtime}}.
	Pinned map[string]map[string]any `yaml:"pinned"`
	Addons Addons                    `yaml:"addons"`
	// Plans label presets with a price and a sentence, keyed by component, then preset.
	Plans      map[string]map[string]Plan `yaml:"plans"`
	Highlights map[string][]string        `yaml:"highlights"`
}

// Runtime is a template environment customers fork.
type Runtime struct {
	ID           string `yaml:"id"`
	Name         string `yaml:"name"`
	Summary      string `yaml:"summary"`
	Project      string `yaml:"project"`
	Template     string `yaml:"template"`
	AppComponent string `yaml:"app_component"`
	ImageParam   string `yaml:"image_param"`
}

// Addon is an optional component of a template.
type Addon struct {
	ID      string
	Title   string `yaml:"title"`
	Summary string `yaml:"summary"`
}

// Addons keeps the order the config lists them in, which is the order customers see.
type Addons []Addon

func (a *Addons) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind != yaml.MappingNode {
		return errors.New("paas.addons must be a mapping of component to title and summary")
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		var addon Addon
		if err := n.Content[i+1].Decode(&addon); err != nil {
			return err
		}
		addon.ID = n.Content[i].Value
		*a = append(*a, addon)
	}
	return nil
}

// Plan is what a preset costs and gets, in a customer's words.
type Plan struct {
	Price   string `yaml:"price"`
	Summary string `yaml:"summary"`
}

type Server struct {
	Addr          string        `yaml:"addr"`
	BaseURL       string        `yaml:"base_url"`
	SessionSecret string        `yaml:"session_secret"`
	SessionTTL    time.Duration `yaml:"session_ttl"`
}

type Database struct {
	Path string `yaml:"path"`
}

// Massdriver says where new accounts' organizations come from.
type Massdriver struct {
	URL string `yaml:"url"`
	// AccountMode is "shared" (every account borrows one organization; development)
	// or "organization" (an organization per account).
	AccountMode string `yaml:"account_mode"`
	// OrganizationID and APIKey are the shared organization's credential. When
	// empty they are read from the mass CLI profile named by CredentialPath/Profile
	// (or MASSDRIVER_CREDENTIAL_PATH and MASSDRIVER_PROFILE).
	OrganizationID string `yaml:"organization_id"`
	APIKey         string `yaml:"api_key"`
	CredentialPath string `yaml:"credential_path"`
	Profile        string `yaml:"profile"`
	// OrgCreatorToken is a personal access token allowed to create organizations
	// (organization mode). Falls back to APIKey.
	OrgCreatorToken string `yaml:"org_creator_token"`
}

type Catalogs struct {
	Dir string `yaml:"dir"`
	// ReleaseChannel is "stable" or "development" for published bundles.
	ReleaseChannel string `yaml:"release_channel"`
}

type Tools struct {
	Mass string `yaml:"mass"`
	Tofu string `yaml:"tofu"`
}

type Auth struct {
	Providers AuthProviders `yaml:"providers"`
}

type AuthProviders struct {
	GitHub *OAuthProvider `yaml:"github"`
	GitLab *OAuthProvider `yaml:"gitlab"`
	// Dev signs in a fixed user with no OAuth app. Local development only.
	Dev *DevProvider `yaml:"dev"`
	// Password signs in anyone who knows the shared password, under the name they type.
	Password *PasswordProvider `yaml:"password"`
}

type PasswordProvider struct {
	Password string `yaml:"password"`
}

type OAuthProvider struct {
	ClientID     string `yaml:"client_id"`
	ClientSecret string `yaml:"client_secret"`
	BaseURL      string `yaml:"base_url"`
}

type DevProvider struct {
	Login string `yaml:"login"`
}

// Load reads a YAML file, expands ${VAR} references, applies defaults, resolves
// the Massdriver credential and validates the result.
func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	cfg, err := Parse(raw)
	if err != nil {
		return nil, err
	}
	if cfg.Skin != "" && !filepath.IsAbs(cfg.Skin) {
		cfg.Skin = filepath.Join(filepath.Dir(path), cfg.Skin)
	}
	return cfg, nil
}

// Parse decodes YAML bytes. See Load.
func Parse(raw []byte) (*Config, error) {
	cfg := Default()
	if err := yaml.Unmarshal([]byte(expandEnv(string(raw))), cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	cfg.applyDefaults()
	if err := cfg.resolveCredential(); err != nil {
		return nil, err
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

var envRef = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// expandEnv replaces only ${NAME}, so prices like "$7/mo" survive.
func expandEnv(s string) string {
	return envRef.ReplaceAllStringFunc(s, func(m string) string { return os.Getenv(m[2 : len(m)-1]) })
}

// Default returns a config with every optional field populated.
func Default() *Config {
	return &Config{
		Server:     Server{Addr: ":8080", BaseURL: "http://localhost:8080", SessionTTL: 7 * 24 * time.Hour},
		Database:   Database{Path: "data/rocketsled.db"},
		DataDir:    "data",
		Massdriver: Massdriver{URL: masscfg.DefaultURL, AccountMode: "shared"},
		Catalogs:   Catalogs{Dir: "catalogs", ReleaseChannel: "stable"},
		Tools:      Tools{Mass: "mass", Tofu: "tofu"},
	}
}

func (c *Config) applyDefaults() {
	c.Server.BaseURL = strings.TrimRight(c.Server.BaseURL, "/")
	c.Massdriver.URL = strings.TrimRight(c.Massdriver.URL, "/")
}

// resolveCredential fills the shared credential from the mass CLI profile when
// it was not given directly.
func (c *Config) resolveCredential() error {
	m := &c.Massdriver
	if m.APIKey != "" && m.OrganizationID != "" {
		return nil
	}
	path, profile := masscfg.Resolve()
	if m.CredentialPath != "" {
		path = m.CredentialPath
	}
	if m.Profile != "" {
		profile = m.Profile
	}
	p, err := masscfg.Load(path, profile)
	if err != nil {
		return fmt.Errorf("massdriver credential: set massdriver.organization_id and api_key, or a mass profile: %w", err)
	}
	if m.OrganizationID == "" {
		m.OrganizationID = p.OrganizationID
	}
	if m.APIKey == "" {
		m.APIKey = p.APIKey
	}
	if p.URL != "" && m.URL == masscfg.DefaultURL {
		m.URL = p.URL
	}
	return nil
}

// Validate reports every configuration problem at once.
func (c *Config) Validate() error {
	var errs []error
	if len(c.Server.SessionSecret) < 32 {
		errs = append(errs, errors.New("server.session_secret must be at least 32 characters"))
	}
	switch c.Massdriver.AccountMode {
	case "shared", "organization":
	case "tenant":
		errs = append(errs, c.PaaS.validate()...)
	default:
		errs = append(errs, fmt.Errorf("massdriver.account_mode must be shared, organization or tenant, got %q", c.Massdriver.AccountMode))
	}
	if c.Massdriver.OrganizationID == "" || c.Massdriver.APIKey == "" {
		errs = append(errs, errors.New("massdriver.organization_id and massdriver.api_key are required"))
	}
	switch c.Catalogs.ReleaseChannel {
	case "stable", "development":
	default:
		errs = append(errs, fmt.Errorf("catalogs.release_channel must be stable or development, got %q", c.Catalogs.ReleaseChannel))
	}
	if c.Auth.Providers.GitHub == nil && c.Auth.Providers.GitLab == nil && c.Auth.Providers.Dev == nil && c.Auth.Providers.Password == nil {
		errs = append(errs, errors.New("auth.providers must configure github, gitlab, password or dev"))
	}
	if p := c.Auth.Providers.Password; p != nil && len(p.Password) < 8 {
		errs = append(errs, errors.New("auth.providers.password.password must be at least 8 characters"))
	}
	if c.Auth.Providers.Dev != nil && strings.HasPrefix(c.Server.BaseURL, "https://") {
		errs = append(errs, errors.New("auth.providers.dev is for local use; remove it when serving over https"))
	}
	for name, p := range map[string]*OAuthProvider{"github": c.Auth.Providers.GitHub, "gitlab": c.Auth.Providers.GitLab} {
		if p != nil && (p.ClientID == "" || p.ClientSecret == "") {
			errs = append(errs, fmt.Errorf("auth.providers.%s needs client_id and client_secret", name))
		}
	}
	return errors.Join(errs...)
}

func (p *PaaS) validate() []error {
	if p == nil || len(p.Runtimes) == 0 {
		return []error{errors.New("tenant mode needs paas.runtimes")}
	}
	var errs []error
	seen := map[string]bool{}
	for i, r := range p.Runtimes {
		if r.ID == "" || r.Project == "" || r.Template == "" {
			errs = append(errs, fmt.Errorf("paas.runtimes[%d] needs id, project and template", i))
		}
		if seen[r.ID] {
			errs = append(errs, fmt.Errorf("paas.runtimes: %q is listed twice", r.ID))
		}
		seen[r.ID] = true
	}
	return errs
}

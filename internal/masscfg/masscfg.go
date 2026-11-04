// Package masscfg reads credentials from the mass CLI's configuration file so
// Rocketsled can run with the same profile a developer already uses.
package masscfg

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	// PathEnv names the config file; it defaults to the mass CLI's own location.
	PathEnv = "MASSDRIVER_CREDENTIAL_PATH"
	// ProfileEnv names the profile inside the file.
	ProfileEnv = "MASSDRIVER_PROFILE"

	DefaultProfile = "default"
	DefaultURL     = "https://api.massdriver.cloud"
)

// Profile is one entry under `profiles:` in the mass CLI config.
type Profile struct {
	Name           string
	OrganizationID string
	APIKey         string
	URL            string
}

type file struct {
	Version  int `yaml:"version"`
	Profiles map[string]struct {
		OrganizationID string `yaml:"organization_id"`
		APIKey         string `yaml:"api_key"`
		URL            string `yaml:"url"`
	} `yaml:"profiles"`
}

// DefaultPath is where the mass CLI keeps its config.
func DefaultPath() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "massdriver", "config.yaml")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "massdriver/config.yaml"
	}
	return filepath.Join(home, ".config", "massdriver", "config.yaml")
}

// Resolve returns the path and profile name from the environment, with defaults.
func Resolve() (path, profile string) {
	path = os.Getenv(PathEnv)
	if path == "" {
		path = DefaultPath()
	}
	profile = os.Getenv(ProfileEnv)
	if profile == "" {
		profile = DefaultProfile
	}
	return expandHome(path), profile
}

func expandHome(p string) string {
	if strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[2:])
		}
	}
	return p
}

// Load reads one profile. The token is returned but never logged by callers.
func Load(path, profile string) (*Profile, error) {
	raw, err := os.ReadFile(expandHome(path))
	if err != nil {
		return nil, fmt.Errorf("read mass config: %w", err)
	}
	var f file
	if err := yaml.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("parse mass config: %w", err)
	}
	p, ok := f.Profiles[profile]
	if !ok {
		names := make([]string, 0, len(f.Profiles))
		for n := range f.Profiles {
			names = append(names, n)
		}
		return nil, fmt.Errorf("profile %q not found in %s (have: %s)", profile, path, strings.Join(names, ", "))
	}
	if p.OrganizationID == "" || p.APIKey == "" {
		return nil, errors.New("profile " + profile + " needs organization_id and api_key")
	}
	url := strings.TrimRight(p.URL, "/")
	if url == "" {
		url = DefaultURL
	}
	return &Profile{Name: profile, OrganizationID: p.OrganizationID, APIKey: p.APIKey, URL: url}, nil
}

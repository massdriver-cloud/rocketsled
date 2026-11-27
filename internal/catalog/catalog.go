// Package catalog loads Rocketsled catalogs from disk. A catalog directory holds
// catalog.yaml plus the platforms/, resource-types/, bundles/ and bootstrap/
// directories the mass CLI and OpenTofu consume.
package catalog

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"gopkg.in/yaml.v3"

	"github.com/massdriver-cloud/rocketsled/internal/domain"
)

type manifest struct {
	ID            string   `yaml:"id"`
	Name          string   `yaml:"name"`
	Cloud         string   `yaml:"cloud"`
	Tagline       string   `yaml:"tagline"`
	Description   string   `yaml:"description"`
	Platforms     []string `yaml:"platforms"`
	ResourceTypes []string `yaml:"resource_types"`
	Bundles       []string `yaml:"bundles"`
	Paths         struct {
		Platforms     string `yaml:"platforms"`
		ResourceTypes string `yaml:"resource_types"`
		Bundles       string `yaml:"bundles"`
	} `yaml:"paths"`
	Bootstrap struct {
		Project         string `yaml:"project"`
		BaseEnvironment string `yaml:"base_environment"`
		AppComponent    string `yaml:"app_component"`
		ImageParam      string `yaml:"image_param"`
	} `yaml:"bootstrap"`
}

type platformFile struct {
	Name   string         `yaml:"name"`
	Label  string         `yaml:"label"`
	Schema map[string]any `yaml:"schema"`
	UI     struct {
		Instructions []struct {
			Label string `yaml:"label"`
			Path  string `yaml:"path"`
		} `yaml:"instructions"`
		Schema map[string]any `yaml:"schema"`
	} `yaml:"ui"`
}

// LoadAll reads every catalog under each of dirs, sorted by name. A catalog ID
// found in two places is an error.
func LoadAll(dirs ...string) ([]domain.Catalog, error) {
	var out []domain.Catalog
	where := map[string]string{}
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return nil, fmt.Errorf("read catalogs dir: %w", err)
		}
		for _, e := range entries {
			path := filepath.Join(dir, e.Name())
			if info, err := os.Stat(path); err != nil || !info.IsDir() {
				continue
			}
			if _, err := os.Stat(filepath.Join(path, "catalog.yaml")); err != nil {
				continue
			}
			c, err := Load(path)
			if err != nil {
				return nil, fmt.Errorf("catalog %s: %w", path, err)
			}
			if first, ok := where[c.ID]; ok {
				return nil, fmt.Errorf("catalog %q is in both %s and %s", c.ID, first, path)
			}
			where[c.ID] = path
			out = append(out, *c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Load reads one catalog directory.
func Load(dir string) (*domain.Catalog, error) {
	raw, err := os.ReadFile(filepath.Join(dir, "catalog.yaml"))
	if err != nil {
		return nil, err
	}
	var m manifest
	if err := yaml.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("parse catalog.yaml: %w", err)
	}
	var errs []error
	if m.ID == "" || m.Name == "" {
		errs = append(errs, errors.New("catalog.yaml needs id and name"))
	}
	if m.Bootstrap.Project == "" || m.Bootstrap.BaseEnvironment == "" {
		errs = append(errs, errors.New("catalog.yaml needs bootstrap.project and bootstrap.base_environment"))
	}
	if _, err := os.Stat(filepath.Join(dir, "bootstrap")); err != nil {
		errs = append(errs, errors.New("bootstrap/ directory is missing"))
	}
	paths := domain.CatalogPaths{
		Platforms:     resolvePath(dir, m.Paths.Platforms, "platforms"),
		ResourceTypes: resolvePath(dir, m.Paths.ResourceTypes, "resource-types"),
		Bundles:       resolvePath(dir, m.Paths.Bundles, "bundles"),
	}
	for _, p := range []struct {
		key, set, path string
		used           bool
	}{
		{"paths.platforms", m.Paths.Platforms, paths.Platforms, len(m.Platforms) > 0},
		{"paths.resource_types", m.Paths.ResourceTypes, paths.ResourceTypes, len(m.ResourceTypes) > 0},
		{"paths.bundles", m.Paths.Bundles, paths.Bundles, len(m.Bundles) > 0},
	} {
		if p.set == "" {
			continue
		}
		if info, err := os.Stat(p.path); err != nil || !info.IsDir() {
			errs = append(errs, fmt.Errorf("%s: %s is not a directory", p.key, p.path))
		}
	}
	for _, rt := range m.ResourceTypes {
		if _, err := os.Stat(filepath.Join(paths.ResourceTypes, rt, "massdriver.yaml")); err != nil {
			errs = append(errs, fmt.Errorf("resource type %q has no massdriver.yaml in %s", rt, paths.ResourceTypes))
		}
	}
	for _, b := range m.Bundles {
		if _, err := os.Stat(filepath.Join(paths.Bundles, b, "massdriver.yaml")); err != nil {
			errs = append(errs, fmt.Errorf("bundle %q has no massdriver.yaml in %s", b, paths.Bundles))
		}
	}
	c := &domain.Catalog{
		ID:            m.ID,
		Name:          m.Name,
		Cloud:         m.Cloud,
		Tagline:       m.Tagline,
		Description:   m.Description,
		ResourceTypes: m.ResourceTypes,
		Bundles:       m.Bundles,
		Platforms:     []domain.PlatformType{},
		Bootstrap: domain.Bootstrap{
			Project:         m.Bootstrap.Project,
			BaseEnvironment: m.Bootstrap.BaseEnvironment,
			AppComponent:    m.Bootstrap.AppComponent,
			ImageParam:      m.Bootstrap.ImageParam,
		},
		Dir:   dir,
		Paths: paths,
	}
	for _, p := range m.Platforms {
		pt, err := loadPlatform(filepath.Join(paths.Platforms, p))
		if err != nil {
			errs = append(errs, fmt.Errorf("platform %q: %w", p, err))
			continue
		}
		c.Platforms = append(c.Platforms, *pt)
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return c, nil
}

// resolvePath places a catalog.yaml path relative to the catalog directory.
// resolvePath follows symlinks in dir first, so ".." climbs from where the
// catalog really is, as a shell would.
func resolvePath(dir, set, fallback string) string {
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		dir = resolved
	}
	if set == "" {
		return filepath.Join(dir, fallback)
	}
	if filepath.IsAbs(set) {
		return filepath.Clean(set)
	}
	return filepath.Join(dir, set)
}

func loadPlatform(dir string) (*domain.PlatformType, error) {
	raw, err := os.ReadFile(filepath.Join(dir, "massdriver.yaml"))
	if err != nil {
		return nil, err
	}
	var pf platformFile
	if err := yaml.Unmarshal(raw, &pf); err != nil {
		return nil, fmt.Errorf("parse massdriver.yaml: %w", err)
	}
	if pf.Name == "" || pf.Schema == nil {
		return nil, errors.New("massdriver.yaml needs name and schema")
	}
	pt := &domain.PlatformType{
		ID:           pf.Name,
		Label:        pf.Label,
		Schema:       pf.Schema,
		Instructions: []domain.Instruction{},
	}
	if pt.Label == "" {
		pt.Label = pf.Name
	}
	if d, ok := pf.Schema["description"].(string); ok {
		pt.Description = d
	}
	if order, ok := pf.UI.Schema["ui:order"].([]any); ok {
		for _, o := range order {
			if s, ok := o.(string); ok {
				pt.UIOrder = append(pt.UIOrder, s)
			}
		}
	}
	for _, in := range pf.UI.Instructions {
		body, err := os.ReadFile(filepath.Join(dir, filepath.Clean(in.Path)))
		if err != nil {
			return nil, fmt.Errorf("instruction %q: %w", in.Label, err)
		}
		pt.Instructions = append(pt.Instructions, domain.Instruction{Label: in.Label, Markdown: string(body)})
	}
	return pt, nil
}

// Package skin loads a PaaS's look from a directory: skin.json, a logo and CSS.
package skin

import (
	"encoding/json"
	"fmt"
	"html"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Skin is a PaaS's name, copy and design tokens.
type Skin struct {
	Name        string                       `json:"name"`
	Tagline     string                       `json:"tagline,omitempty"`
	Logo        string                       `json:"logo,omitempty"`
	Favicon     string                       `json:"favicon,omitempty"`
	Layout      string                       `json:"layout,omitempty"`
	ColorScheme string                       `json:"colorScheme,omitempty"`
	Tokens      map[string]map[string]string `json:"tokens,omitempty"`
	Copy        map[string]string            `json:"copy,omitempty"`
	CSS         string                       `json:"css,omitempty"`
	// Files serves the skin directory's logo, favicon and CSS.
	Files fs.FS `json:"-"`
}

// Load reads dir/skin.json.
func Load(dir string) (*Skin, error) {
	raw, err := os.ReadFile(filepath.Join(dir, "skin.json"))
	if err != nil {
		return nil, fmt.Errorf("skin: %w", err)
	}
	var s Skin
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, fmt.Errorf("skin %s: %w", dir, err)
	}
	if s.Name == "" {
		return nil, fmt.Errorf("skin %s: name is required", dir)
	}
	s.Files = os.DirFS(dir)
	for _, f := range []string{s.Logo, s.Favicon, s.CSS} {
		if f == "" {
			continue
		}
		if _, err := fs.Stat(s.Files, f); err != nil {
			return nil, fmt.Errorf("skin %s: %w", dir, err)
		}
	}
	return &s, nil
}

// URL is where the server publishes one of the skin's files.
func URL(name string) string {
	if name == "" {
		return ""
	}
	return "/skin/" + name
}

// Head is markup for the page's <head>, so the skin paints on first load
// instead of flashing the default look.
func (s *Skin) Head() string {
	var b strings.Builder
	fmt.Fprintf(&b, "<title>%s</title>\n", html.EscapeString(s.Name))
	if s.Favicon != "" {
		fmt.Fprintf(&b, `<link rel="icon" href="%s" />`+"\n", html.EscapeString(URL(s.Favicon)))
	}
	b.WriteString("<style>\n")
	light := s.Tokens["light"]
	scheme := s.ColorScheme
	if scheme == "" {
		scheme = "light"
	}
	fmt.Fprintf(&b, ":root{color-scheme:%s;%s}\n", cssValue(scheme), declarations(light))
	if dark, ok := s.Tokens["dark"]; ok && strings.Contains(scheme, "dark") {
		merged := map[string]string{}
		maps.Copy(merged, light)
		maps.Copy(merged, dark)
		fmt.Fprintf(&b, "@media (prefers-color-scheme: dark){:root{%s}}\n", declarations(merged))
	}
	b.WriteString("</style>\n")
	if s.CSS != "" {
		fmt.Fprintf(&b, `<link rel="stylesheet" href="%s" />`+"\n", html.EscapeString(URL(s.CSS)))
	}
	return b.String()
}

func declarations(tokens map[string]string) string {
	keys := make([]string, 0, len(tokens))
	for k := range tokens {
		if strings.HasPrefix(k, "--") {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		fmt.Fprintf(&b, "%s:%s;", k, cssValue(tokens[k]))
	}
	return b.String()
}

// cssValue keeps a token from closing the style element or the rule.
func cssValue(v string) string {
	return strings.NewReplacer("<", "", "{", "", "}", "", ";", "").Replace(v)
}

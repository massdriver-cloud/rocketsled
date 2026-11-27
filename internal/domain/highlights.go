package domain

import (
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// maxFallbackHighlights caps the fields shown for a resource type nobody configured.
const maxFallbackHighlights = 5

// Highlight is one field of a resource payload worth showing, with its label.
type Highlight struct {
	Field string `json:"field"`
	Title string `json:"title"`
	Value string `json:"value"`
}

// SchemaField is a top-level property of a resource type's schema, in schema order.
type SchemaField struct {
	Name  string
	Title string
}

var (
	secretField = regexp.MustCompile(`(?i)secret|password|passwd|token|credential|private|dsn|connection_string|(^|_)key$|(^|_)key_|api_?key|access_key`)
	// camelKey catches sshKey and signingKey, which the snake_case rules miss.
	camelKey = regexp.MustCompile(`[a-z0-9]Key$`)
	// userinfo spots credentials in a connection string url.Parse can't read,
	// one whose password has a # or / in it, say.
	userinfo = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9+.-]*://[^/@\s]*:[^@\s]*@`)
)

// PickHighlights chooses what to show of a resource: the configured fields in
// their configured order, or else the first scalar fields in schema order.
// Fields that look secret are never shown.
func PickHighlights(r InstanceResource, configured []string) []Highlight {
	titles := make(map[string]string, len(r.Fields))
	for _, f := range r.Fields {
		titles[f.Name] = f.Title
	}
	names := configured
	limit := len(configured)
	if configured == nil {
		names = fallbackOrder(r)
		limit = maxFallbackHighlights
	}
	out := []Highlight{}
	for _, name := range names {
		if len(out) == limit {
			break
		}
		value, ok := scalar(r.Payload[name])
		if !ok || secretField.MatchString(name) || camelKey.MatchString(name) || carriesPassword(value) || slices.ContainsFunc(out, func(h Highlight) bool { return h.Field == name }) {
			continue
		}
		title := titles[name]
		if title == "" {
			title = humanize(name)
		}
		out = append(out, Highlight{Field: name, Title: title, Value: value})
	}
	return out
}

// fallbackOrder lists the payload's keys in schema order; keys the schema
// doesn't know follow alphabetically, so the order is stable.
func fallbackOrder(r InstanceResource) []string {
	names := make([]string, 0, len(r.Payload))
	seen := make(map[string]bool, len(r.Fields))
	for _, f := range r.Fields {
		if _, ok := r.Payload[f.Name]; ok && !seen[f.Name] {
			names = append(names, f.Name)
			seen[f.Name] = true
		}
	}
	var rest []string
	for k := range r.Payload {
		if !seen[k] {
			rest = append(rest, k)
		}
	}
	slices.Sort(rest)
	return append(names, rest...)
}

// carriesPassword spots a connection string with credentials in it, whatever its field is called.
func carriesPassword(value string) bool {
	if userinfo.MatchString(value) {
		return true
	}
	u, err := url.Parse(value)
	if err != nil || u.User == nil {
		return false
	}
	_, ok := u.User.Password()
	return ok
}

func scalar(v any) (string, bool) {
	switch t := v.(type) {
	case string:
		return t, t != ""
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64), true
	case bool, int, int64:
		return fmt.Sprint(t), true
	}
	return "", false
}

var acronyms = map[string]bool{"api": true, "arn": true, "cidr": true, "dns": true, "id": true, "ip": true, "tls": true, "url": true, "uri": true}

func humanize(field string) string {
	words := strings.Fields(strings.ReplaceAll(field, "_", " "))
	for i, w := range words {
		switch {
		case acronyms[w]:
			words[i] = strings.ToUpper(w)
		case i == 0:
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}
	return strings.Join(words, " ")
}

// resourceTypeName drops the version Massdriver appends to resource type IDs.
func resourceTypeName(id string) string {
	name, _, _ := strings.Cut(id, "@")
	return name
}

// highlight fills each resource's highlights from the configured fields per resource type.
func highlight(env *PlatformEnvironment, configured map[string][]string) {
	for i := range env.Instances {
		for j := range env.Instances[i].Resources {
			r := &env.Instances[i].Resources[j]
			r.Highlights = PickHighlights(*r, configured[resourceTypeName(r.ResourceType)])
		}
	}
}

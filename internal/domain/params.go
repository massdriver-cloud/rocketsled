package domain

import (
	"maps"
	"strings"
)

// PresetNameKey is the key bundles use to label an example in params.examples.
const PresetNameKey = "__name"

// MergeParams deep-merges overlay into base, returning a new map. Nested maps merge
// recursively; every other value in overlay replaces the base value.
func MergeParams(base, overlay map[string]any) map[string]any {
	out := make(map[string]any, len(base)+len(overlay))
	maps.Copy(out, base)
	for k, v := range overlay {
		if k == PresetNameKey {
			continue
		}
		ov, overlayIsMap := v.(map[string]any)
		bv, baseIsMap := out[k].(map[string]any)
		if overlayIsMap && baseIsMap {
			out[k] = MergeParams(bv, ov)
			continue
		}
		out[k] = v
	}
	return out
}

// SetParam writes value at a dotted path, creating intermediate maps. It returns a
// new top-level map and never mutates params.
func SetParam(params map[string]any, path string, value any) map[string]any {
	out := make(map[string]any, len(params)+1)
	maps.Copy(out, params)
	key, rest, more := strings.Cut(path, ".")
	if !more {
		out[key] = value
		return out
	}
	child, _ := out[key].(map[string]any)
	out[key] = SetParam(child, rest, value)
	return out
}

// GetParam reads a dotted path.
func GetParam(params map[string]any, path string) (any, bool) {
	key, rest, more := strings.Cut(path, ".")
	v, ok := params[key]
	if !ok {
		return nil, false
	}
	if !more {
		return v, true
	}
	child, isMap := v.(map[string]any)
	if !isMap {
		return nil, false
	}
	return GetParam(child, rest)
}

// SchemaHasPath reports whether a JSON Schema declares a property at a dotted path.
func SchemaHasPath(schema map[string]any, path string) bool {
	props, _ := schema["properties"].(map[string]any)
	key, rest, more := strings.Cut(path, ".")
	prop, ok := props[key].(map[string]any)
	if !ok {
		return false
	}
	if !more {
		return true
	}
	return SchemaHasPath(prop, rest)
}

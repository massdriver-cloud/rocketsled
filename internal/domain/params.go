package domain

import "maps"

// PresetNameKey is the key bundles use to label an example in params.examples.
const PresetNameKey = "__name"

// MergeParams deep-merges overlay into base, returning a new map. Nested maps merge
// recursively; every other value in overlay replaces the base value. The preset
// label is dropped.
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

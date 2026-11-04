package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/massdriver-cloud/rocketsled/internal/domain"
)

func TestMergeParamsDeepMergesAndDropsPresetName(t *testing.T) {
	base := map[string]any{
		"image":   map[string]any{"repository": "ghcr.io/acme/shop", "tag": "old"},
		"scaling": map[string]any{"min": 1, "max": 2},
		"keep":    true,
	}
	preset := map[string]any{
		"__name":  "Production",
		"scaling": map[string]any{"min": 3},
		"extra":   "x",
	}

	got := domain.MergeParams(base, preset)

	assert.Equal(t, map[string]any{
		"image":   map[string]any{"repository": "ghcr.io/acme/shop", "tag": "old"},
		"scaling": map[string]any{"min": 3, "max": 2},
		"keep":    true,
		"extra":   "x",
	}, got)
	assert.Equal(t, 1, base["scaling"].(map[string]any)["min"], "base must not be mutated")
}

func TestSetParamCreatesIntermediateMaps(t *testing.T) {
	got := domain.SetParam(map[string]any{"a": 1}, "image.tag", "abc123")
	assert.Equal(t, map[string]any{"a": 1, "image": map[string]any{"tag": "abc123"}}, got)

	v, ok := domain.GetParam(got, "image.tag")
	assert.True(t, ok)
	assert.Equal(t, "abc123", v)

	_, ok = domain.GetParam(got, "image.tag.deeper")
	assert.False(t, ok)
}

func TestSchemaHasPath(t *testing.T) {
	schema := map[string]any{
		"properties": map[string]any{
			"image": map[string]any{
				"type":       "object",
				"properties": map[string]any{"tag": map[string]any{"type": "string"}},
			},
		},
	}
	assert.True(t, domain.SchemaHasPath(schema, "image.tag"))
	assert.False(t, domain.SchemaHasPath(schema, "image.digest"))
	assert.False(t, domain.SchemaHasPath(schema, "replicas"))
}

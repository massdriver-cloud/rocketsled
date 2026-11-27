package massdriver //nolint:testpackage // schemaFields is unexported

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/massdriver-cloud/rocketsled/internal/domain"
)

func TestSchemaFields(t *testing.T) {
	schema := `{"properties":{"id":{"title":"ID"},"region":{"title":"Region"},"url":{"title":"URL"},"arn":{}}}`
	cases := []struct {
		name     string
		schema   string
		uiSchema string
		want     []string
	}{
		{"schema order", schema, `{}`, []string{"id", "region", "url", "arn"}},
		{"ui:order first, the rest where * sits", schema, `{"ui:order":["url","*","id"]}`, []string{"url", "region", "arn", "id"}},
		{"ui:order without *", schema, `{"ui:order":["arn"]}`, []string{"arn", "id", "region", "url"}},
		{"Map sent as a JSON string", mustQuote(schema), mustQuote(`{"ui:order":["url"]}`), []string{"url", "id", "region", "arn"}},
		{"no properties", `{"type":"object"}`, `{}`, nil},
		{"missing", ``, ``, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got []string
			for _, f := range schemaFields(json.RawMessage(tc.schema), json.RawMessage(tc.uiSchema)) {
				got = append(got, f.Name)
			}
			assert.Equal(t, tc.want, got)
		})
	}
	assert.Equal(t, domain.SchemaField{Name: "url", Title: "URL"}, schemaFields(json.RawMessage(schema), nil)[2])
}

func mustQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

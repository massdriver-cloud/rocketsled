package massdriver

import (
	"bytes"
	"encoding/json"
	"slices"

	"github.com/massdriver-cloud/rocketsled/internal/domain"
)

// schemaFields lists a resource type's top-level properties with their titles,
// ordered by the UI schema's ui:order, else as the schema's JSON lists them.
// A Go map would lose that order, so the properties are read token by token.
func schemaFields(schema, uiSchema json.RawMessage) []domain.SchemaField {
	var root struct {
		Properties json.RawMessage `json:"properties"`
	}
	if json.Unmarshal(unquote(schema), &root) != nil || len(root.Properties) == 0 {
		return nil
	}
	fields := orderedProperties(root.Properties)
	var ui struct {
		Order []string `json:"ui:order"`
	}
	if json.Unmarshal(unquote(uiSchema), &ui) != nil || len(ui.Order) == 0 {
		return fields
	}
	rest := slices.Index(ui.Order, "*")
	if rest < 0 {
		rest = len(ui.Order)
	}
	rank := func(name string) int {
		if i := slices.Index(ui.Order, name); i >= 0 {
			return i
		}
		return rest
	}
	slices.SortStableFunc(fields, func(a, b domain.SchemaField) int { return rank(a.Name) - rank(b.Name) })
	return fields
}

func orderedProperties(raw json.RawMessage) []domain.SchemaField {
	dec := json.NewDecoder(bytes.NewReader(raw))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return nil
	}
	var out []domain.SchemaField
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return out
		}
		name, _ := tok.(string)
		var value json.RawMessage
		if err := dec.Decode(&value); err != nil {
			return out
		}
		var prop struct {
			Title string `json:"title"`
		}
		_ = json.Unmarshal(value, &prop)
		out = append(out, domain.SchemaField{Name: name, Title: prop.Title})
	}
	return out
}

// unquote accepts a Map scalar sent either as a JSON object or as a JSON-encoded string.
func unquote(raw json.RawMessage) json.RawMessage {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return json.RawMessage(s)
	}
	return raw
}

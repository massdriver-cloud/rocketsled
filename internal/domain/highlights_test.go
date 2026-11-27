package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/massdriver-cloud/rocketsled/internal/domain"
)

func TestPickHighlights(t *testing.T) {
	postgres := domain.InstanceResource{
		ResourceType: "aws-postgres",
		Payload: map[string]any{
			"arn": "arn:aws:rds:x", "database": "app", "hostname": "db.internal", "password": "hunter2",
			"port": float64(5432), "username": "app", "tls": true, "endpoints": map[string]any{"reader": "r"}, "empty": "",
		},
		Fields: []domain.SchemaField{
			{Name: "hostname", Title: "Hostname"},
			{Name: "port", Title: "Port"},
			{Name: "username"},
			{Name: "password", Title: "Password"},
			{Name: "endpoints"},
			{Name: "database", Title: "Database"},
		},
	}
	cases := []struct {
		name       string
		resource   domain.InstanceResource
		configured []string
		want       []domain.Highlight
	}{
		{
			name: "configured fields in configured order, missing and repeated ones skipped", resource: postgres,
			configured: []string{"port", "nope", "hostname", "port", "database"},
			want:       []domain.Highlight{{"port", "Port", "5432"}, {"hostname", "Hostname", "db.internal"}, {"database", "Database", "app"}},
		},
		{
			name: "configured secrets and objects are never shown", resource: postgres,
			configured: []string{"password", "endpoints", "empty", "username"},
			want:       []domain.Highlight{{"username", "Username", "app"}},
		},
		{
			name: "configured empty list shows nothing", resource: postgres, configured: []string{},
			want: []domain.Highlight{},
		},
		{
			name: "fallback follows the schema, then the rest alphabetically, up to five", resource: postgres,
			want: []domain.Highlight{
				{"hostname", "Hostname", "db.internal"},
				{"port", "Port", "5432"},
				{"username", "Username", "app"},
				{"database", "Database", "app"},
				{"arn", "ARN", "arn:aws:rds:x"},
			},
		},
		{
			name:     "fallback without a schema is alphabetical and titles come from the field name",
			resource: domain.InstanceResource{Payload: map[string]any{"url": "https://x", "client_id": "c", "api_key": "k"}},
			want:     []domain.Highlight{{"client_id", "Client ID", "c"}, {"url", "URL", "https://x"}},
		},
		{
			name: "no payload", resource: domain.InstanceResource{}, want: []domain.Highlight{},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, domain.PickHighlights(tc.resource, tc.configured))
		})
	}
}

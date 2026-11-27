package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/massdriver-cloud/rocketsled/internal/domain"
)

func TestRenderInstructions(t *testing.T) {
	vars := domain.InstructionVars{OrganizationID: "rs-misty-river-42", Issuer: "api.massdriver.cloud"}
	tests := []struct {
		name string
		in   string
		vars domain.InstructionVars
		want string
	}{
		{
			name: "nothing to render",
			in:   "# Connect AWS\n\nCreate a role.",
			vars: vars,
			want: "# Connect AWS\n\nCreate a role.",
		},
		{
			name: "placeholders",
			in:   "Audience `massdriver:{{organization_id}}` from https://{{issuer}}.",
			vars: vars,
			want: "Audience `massdriver:rs-misty-river-42` from https://api.massdriver.cloud.",
		},
		{
			name: "the upstream ORG_ID token in prose and JSON",
			in:   "Enter `massdriver:ORG_ID`.\n\"api.massdriver.cloud:sub\": \"org:ORG_ID\"",
			vars: vars,
			want: "Enter `massdriver:rs-misty-river-42`.\n\"api.massdriver.cloud:sub\": \"org:rs-misty-river-42\"",
		},
		{
			name: "a shell assignment and references get the value",
			in:   "```bash\nORG_ID=$(mass whoami -o json | jq -r .organization.id)\necho \"$ORG_ID\" ${ORG_ID}\n  export ORG_ID=x\n```",
			vars: vars,
			want: "```bash\nORG_ID=rs-misty-river-42\necho \"rs-misty-river-42\" rs-misty-river-42\n  export ORG_ID=rs-misty-river-42\n```",
		},
		{
			name: "references still work when the step that set ORG_ID is skipped",
			in:   "<!-- rocketsled:skip -->\nORG_ID=$(mass whoami)\n<!-- /rocketsled:skip -->\naws iam create-open-id-connect-provider --client-id-list \"massdriver:$ORG_ID\"",
			vars: vars,
			want: "aws iam create-open-id-connect-provider --client-id-list \"massdriver:rs-misty-river-42\"",
		},
		{
			name: "only the assigned value is replaced, not the rest of the line",
			in:   "export ORG_ID=$(mass whoami) && echo $ORG_ID\nORG_ID=x aws sts get-caller-identity",
			vars: vars,
			want: "export ORG_ID=rs-misty-river-42 && echo rs-misty-river-42\nORG_ID=rs-misty-river-42 aws sts get-caller-identity",
		},
		{
			name: "longer names that contain ORG_ID are left alone",
			in:   "MY_ORG_ID and ORG_IDS",
			vars: vars,
			want: "MY_ORG_ID and ORG_IDS",
		},
		{
			name: "skip blocks are removed, several and across lines",
			in:   "Intro.\n<!-- rocketsled:skip -->\n## 1. Sign in to mass\n\n`mass login`\n<!-- /rocketsled:skip -->\n## 2. Create the role\n<!--rocketsled:skip-->Run `mass whoami`.<!-- /rocketsled:skip -->Done.",
			vars: vars,
			want: "Intro.\n## 2. Create the role\nDone.",
		},
		{
			name: "an unclosed skip block is kept",
			in:   "<!-- rocketsled:skip -->\nstill here",
			vars: vars,
			want: "<!-- rocketsled:skip -->\nstill here",
		},
		{
			name: "no organization leaves ORG_ID for the reader",
			in:   "Use massdriver:ORG_ID on {{issuer}}.",
			vars: domain.InstructionVars{Issuer: "api.massdriver.cloud"},
			want: "Use massdriver:ORG_ID on api.massdriver.cloud.",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, domain.RenderInstructions(tt.in, tt.vars))
		})
	}
}

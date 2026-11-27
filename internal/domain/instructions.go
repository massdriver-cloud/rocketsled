package domain

import (
	"net/url"
	"regexp"
	"strings"
)

// DefaultMassdriverURL is the API a credential without a URL talks to.
const DefaultMassdriverURL = "https://api.massdriver.cloud"

// InstructionVars are the account's values a credential's instructions may need.
type InstructionVars struct {
	OrganizationID string
	// Issuer is the host that signs Massdriver's identity tokens.
	Issuer string
}

// instructionVarsFor reads the values from the account and its credential.
func instructionVarsFor(account *Account, cred Credential) InstructionVars {
	raw := cred.URL
	if raw == "" {
		raw = DefaultMassdriverURL
	}
	issuer := raw
	if u, err := url.Parse(raw); err == nil && u.Host != "" {
		issuer = u.Host
	}
	org := account.OrganizationID
	if org == "" {
		org = cred.OrganizationID
	}
	return InstructionVars{OrganizationID: org, Issuer: issuer}
}

var (
	skipBlock = regexp.MustCompile(`(?s)<!--\s*rocketsled:skip\s*-->.*?<!--\s*/rocketsled:skip\s*-->\n?`)
	// Upstream instructions set ORG_ID from `mass whoami`, which a Rocketsled
	// customer doesn't have. The value is written in everywhere, the assignment
	// included, so the step that sets it can be skipped without leaving a shell
	// reference empty.
	orgIDAssignment = regexp.MustCompile(`\bORG_ID=(\$\([^)]*\)|"[^"]*"|'[^']*'|[^\s;&|]+)`)
	orgIDToken      = regexp.MustCompile(`\$\{ORG_ID\}|\$ORG_ID\b|\bORG_ID\b(=)?`)
)

// RenderInstructions fills an instruction's placeholders with the account's
// values and drops the steps only a Massdriver user can follow.
func RenderInstructions(markdown string, vars InstructionVars) string {
	out := skipBlock.ReplaceAllString(markdown, "")
	out = strings.NewReplacer("{{organization_id}}", vars.OrganizationID, "{{issuer}}", vars.Issuer).Replace(out)
	if vars.OrganizationID == "" {
		return out
	}
	out = orgIDAssignment.ReplaceAllLiteralString(out, "ORG_ID="+vars.OrganizationID)
	return orgIDToken.ReplaceAllStringFunc(out, func(m string) string {
		if strings.HasSuffix(m, "=") {
			return m
		}
		return vars.OrganizationID
	})
}

// renderPlatformType returns a copy of the type with its instructions rendered;
// the catalog's own copy is shared by every account.
func renderPlatformType(pt PlatformType, vars InstructionVars) PlatformType {
	rendered := make([]Instruction, len(pt.Instructions))
	for i, in := range pt.Instructions {
		rendered[i] = Instruction{Label: in.Label, Markdown: RenderInstructions(in.Markdown, vars)}
	}
	pt.Instructions = rendered
	return pt
}

func renderCatalog(c *Catalog, vars InstructionVars) *Catalog {
	out := *c
	out.Platforms = make([]PlatformType, len(c.Platforms))
	for i, pt := range c.Platforms {
		out.Platforms[i] = renderPlatformType(pt, vars)
	}
	return &out
}

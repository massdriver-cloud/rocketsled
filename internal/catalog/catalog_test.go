package catalog_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/massdriver-cloud/rocketsled/internal/catalog"
)

func TestLoadsTheShippedCatalog(t *testing.T) {
	cats, err := catalog.LoadAll(filepath.Join("..", "..", "catalogs"))
	require.NoError(t, err)
	require.Len(t, cats, 1)

	c := cats[0]
	assert.Equal(t, "aws-serverless", c.ID)
	assert.Equal(t, "AWS Serverless", c.Name)
	assert.Equal(t, "serverless", c.Bootstrap.Project)
	assert.Equal(t, "production", c.Bootstrap.BaseEnvironment)
	assert.Equal(t, []string{"shared-vpc", "dynamodb-table", "http-api", "lambda-function"}, c.Bundles)
	require.Len(t, c.Platforms, 1)
	p := c.Platforms[0]
	assert.Equal(t, "aws-iam-role", p.ID)
	assert.Equal(t, "AWS IAM Role", p.Label)
	assert.Equal(t, []string{"external_id", "arn", "*"}, p.UIOrder)
	assert.ElementsMatch(t, []any{"arn", "external_id"}, p.Schema["required"])
	require.Len(t, p.Instructions, 2)
	assert.Equal(t, "AWS CLI", p.Instructions[0].Label)
	assert.Contains(t, p.Instructions[0].Markdown, "aws")
}

func TestLoadsAnOIDCPlatform(t *testing.T) {
	dir := t.TempDir()
	write := func(rel, body string) {
		t.Helper()
		require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(dir, rel)), 0o750))
		require.NoError(t, os.WriteFile(filepath.Join(dir, rel), []byte(body), 0o600))
	}
	write("catalog.yaml", "id: oidc\nname: OIDC\nplatforms: [aws-iam-role]\nbootstrap: {project: p, base_environment: main}\n")
	write("bootstrap/main.tf", "")
	write("platforms/aws-iam-role/massdriver.yaml", `
name: aws-iam-role
label: AWS IAM Role
ui:
  instructions:
    - label: AWS Console
      path: ./instructions/AWS Console.md
  schema:
    ui:order: [arn, token_level, "*"]
schema:
  required: [arn, token_level]
  properties:
    arn: {title: Role ARN, type: string}
    token_level:
      title: Token level
      type: string
      default: organization
      oneOf:
        - {const: organization, title: Organization}
        - {const: project, title: Project}
`)
	write("platforms/aws-iam-role/instructions/AWS Console.md", "Audience `massdriver:ORG_ID`.")

	c, err := catalog.Load(dir)
	require.NoError(t, err)
	p := c.Platforms[0]
	assert.Equal(t, []string{"arn", "token_level", "*"}, p.UIOrder)
	assert.ElementsMatch(t, []any{"arn", "token_level"}, p.Schema["required"])
	props, ok := p.Schema["properties"].(map[string]any)
	require.True(t, ok)
	level, ok := props["token_level"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "organization", level["default"])
	assert.Len(t, level["oneOf"], 2)
	assert.Equal(t, "Audience `massdriver:ORG_ID`.", p.Instructions[0].Markdown, "instructions load as written; they are rendered per account")
}

func TestLoadReportsMissingPieces(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "catalog.yaml"), []byte(`
id: broken
name: Broken
bundles: [nope]
resource_types: [missing]
platforms: [ghost]
bootstrap: {project: p}
`), 0o600))

	_, err := catalog.Load(dir)
	require.Error(t, err)
	for _, want := range []string{"bootstrap.base_environment", "bootstrap/ directory", `bundle "nope"`, `resource type "missing"`, `platform "ghost"`} {
		assert.Contains(t, err.Error(), want)
	}
}

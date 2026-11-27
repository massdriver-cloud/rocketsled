package catalog_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/massdriver-cloud/rocketsled/internal/catalog"
	"github.com/massdriver-cloud/rocketsled/internal/domain"
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

func write(t *testing.T, path, body string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
}

// sharedTree lays out a bundle repository with a catalog at examples/paas whose
// platforms, resource types and bundles live at the repository root.
func sharedTree(t *testing.T) (root, catalogDir string) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	catalogDir = filepath.Join(root, "examples", "paas")
	write(t, filepath.Join(catalogDir, "catalog.yaml"), `
id: paas
name: PaaS
platforms: [aws-iam-role]
resource_types: [aws-vpc]
bundles: [vpc]
paths:
  platforms: ../../platforms
  resource_types: ../../resource-types
  bundles: `+filepath.Join(root, "bundles")+`
bootstrap: {project: p, base_environment: main}
`)
	write(t, filepath.Join(catalogDir, "bootstrap", "main.tf"), "")
	write(t, filepath.Join(root, "platforms", "aws-iam-role", "massdriver.yaml"), "name: aws-iam-role\nschema: {type: object}\n")
	write(t, filepath.Join(root, "resource-types", "aws-vpc", "massdriver.yaml"), "name: aws-vpc\n")
	write(t, filepath.Join(root, "bundles", "vpc", "massdriver.yaml"), "name: vpc\n")
	return root, catalogDir
}

func TestLoadResolvesPathsOutsideTheCatalog(t *testing.T) {
	root, dir := sharedTree(t)

	c, err := catalog.Load(dir)
	require.NoError(t, err)
	assert.Equal(t, domain.CatalogPaths{
		Platforms:     filepath.Join(root, "platforms"),
		ResourceTypes: filepath.Join(root, "resource-types"),
		Bundles:       filepath.Join(root, "bundles"),
	}, c.Paths)
	require.Len(t, c.Platforms, 1)
	assert.Equal(t, "aws-iam-role", c.Platforms[0].ID)
}

func TestLoadClimbsFromWhereASymlinkedCatalogReallyIs(t *testing.T) {
	root, dir := sharedTree(t)
	elsewhere := t.TempDir()
	link := filepath.Join(elsewhere, "linked")
	require.NoError(t, os.Symlink(filepath.Dir(dir), link))

	c, err := catalog.Load(filepath.Join(link, "paas"))
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(root, "platforms"), c.Paths.Platforms, "../../platforms from the real directory, not the link")

	all, err := catalog.LoadAll(link)
	require.NoError(t, err)
	require.Len(t, all, 1, "a catalog directory reached through a symlink is loaded")
}

func TestLoadDefaultsPathsToSubdirectories(t *testing.T) {
	c, err := catalog.Load(filepath.Join("..", "..", "catalogs", "aws-serverless"))
	require.NoError(t, err)
	assert.Equal(t, domain.CatalogPaths{
		Platforms:     filepath.Join(c.Dir, "platforms"),
		ResourceTypes: filepath.Join(c.Dir, "resource-types"),
		Bundles:       filepath.Join(c.Dir, "bundles"),
	}, c.Paths)
}

func TestLoadRejectsPathsThatAreNotDirectories(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	write(t, filepath.Join(dir, "catalog.yaml"), `
id: c
name: C
paths: {bundles: ../nowhere, resource_types: catalog.yaml}
bootstrap: {project: p, base_environment: main}
`)
	write(t, filepath.Join(dir, "bootstrap", "main.tf"), "")

	_, err = catalog.Load(dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "paths.bundles: "+filepath.Join(dir, "../nowhere")+" is not a directory")
	assert.Contains(t, err.Error(), "paths.resource_types: "+filepath.Join(dir, "catalog.yaml")+" is not a directory")
}

func TestLoadAllReadsEveryDirAndRejectsDuplicateIDs(t *testing.T) {
	_, dir := sharedTree(t)
	shipped := filepath.Join("..", "..", "catalogs")

	cats, err := catalog.LoadAll(shipped, filepath.Dir(dir))
	require.NoError(t, err)
	ids := make([]string, 0, len(cats))
	for _, c := range cats {
		ids = append(ids, c.ID)
	}
	assert.ElementsMatch(t, []string{"aws-serverless", "paas"}, ids)

	_, err = catalog.LoadAll(shipped, filepath.Dir(dir), shipped)
	require.ErrorContains(t, err, `catalog "aws-serverless" is in both`)
}

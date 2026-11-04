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

package installer_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/massdriver-cloud/rocketsled/internal/catalog"
	"github.com/massdriver-cloud/rocketsled/internal/domain"
	"github.com/massdriver-cloud/rocketsled/internal/installer"
)

type fake struct {
	calls  []string
	envs   [][]string
	failOn string
}

func (f *fake) run(_ context.Context, dir string, env []string, out io.Writer, name string, args ...string) error {
	line := name + " " + strings.Join(args, " ")
	f.calls = append(f.calls, filepath.Base(dir)+": "+line)
	f.envs = append(f.envs, env)
	if f.failOn != "" && strings.Contains(line, f.failOn) {
		_, _ = io.WriteString(out, "boom\n")
		return errors.New("exit status 1")
	}
	switch {
	case strings.HasPrefix(line, "tofu apply"):
		_ = os.WriteFile(filepath.Join(dir, "terraform.tfstate"), []byte(`{"version":4}`), 0o600)
	case strings.HasPrefix(line, "tofu output"):
		_, _ = io.WriteString(out, `{"project":{"value":"serverless"},"environments":{"value":{"production":"serverless-production"}},"components":{"value":{"app":"serverless-app"}}}`)
	}
	return nil
}

func shipped(t *testing.T) *domain.Catalog {
	t.Helper()
	c, err := catalog.Load(filepath.Join("..", "..", "catalogs", "aws-serverless"))
	require.NoError(t, err)
	return c
}

func TestInstallPublishesThenBootstraps(t *testing.T) {
	f := &fake{}
	inst := &installer.Installer{Mass: "mass", Tofu: "tofu", Run: f.run}
	var log bytes.Buffer
	work := t.TempDir()

	res, err := inst.Install(context.Background(), domain.InstallRequest{
		Catalog:    shipped(t),
		Credential: domain.Credential{OrganizationID: "rs-test", APIKey: "md_secret", URL: "https://api.massdriver.cloud"},
		WorkDir:    work,
		Stable:     true,
		Log:        &log,
	})
	require.NoError(t, err)

	assert.Equal(t, "serverless", res.ProjectID)
	assert.Equal(t, map[string]string{"production": "serverless-production"}, res.Environments)
	assert.JSONEq(t, `{"version":4}`, string(res.State))

	joined := strings.Join(f.calls, "\n")
	assert.Contains(t, joined, "aws-iam-role: mass repository create aws-iam-role -t resource-type")
	assert.Contains(t, joined, "aws-iam-role: mass resource-type publish .")
	assert.Contains(t, joined, "aws-vpc: mass resource-type publish .")
	assert.Contains(t, joined, "shared-vpc: mass bundle publish --bundle-directory .")
	assert.NotContains(t, joined, "--development")
	assert.Contains(t, joined, "bootstrap: tofu init -input=false -no-color")
	assert.Contains(t, joined, "bootstrap: tofu apply -auto-approve -input=false -no-color")
	assert.Less(t, strings.Index(joined, "resource-type publish"), strings.Index(joined, "bundle publish"), "resource types before bundles")
	assert.Less(t, strings.Index(joined, "bundle publish"), strings.Index(joined, "tofu init"), "bundles before bootstrap")

	assert.Contains(t, f.envs[0], "MASSDRIVER_ORGANIZATION_ID=rs-test")
	assert.Contains(t, f.envs[0], "MASSDRIVER_API_KEY=md_secret")
	assert.Contains(t, log.String(), "==> publishing AWS Serverless into rs-test")
	assert.NotContains(t, log.String(), "md_secret")
	_, err = os.Stat(filepath.Join(work, "bootstrap", "main.tf"))
	require.NoError(t, err, "bootstrap files are copied into the work dir")
}

func TestInstallUsesDevelopmentChannelAndRestoresState(t *testing.T) {
	f := &fake{}
	inst := &installer.Installer{Mass: "mass", Tofu: "tofu", Run: f.run}
	work := t.TempDir()

	_, err := inst.Install(context.Background(), domain.InstallRequest{
		Catalog: shipped(t), Credential: domain.Credential{OrganizationID: "rs-test", APIKey: "k"},
		WorkDir: work, State: []byte(`{"version":4,"serial":7}`),
	})
	require.NoError(t, err)
	assert.Contains(t, strings.Join(f.calls, "\n"), "mass bundle publish --bundle-directory . --development")
}

func TestInstallFailureKeepsStateAndLog(t *testing.T) {
	f := &fake{failOn: "tofu apply"}
	inst := &installer.Installer{Mass: "mass", Tofu: "tofu", Run: f.run}
	var log bytes.Buffer

	res, err := inst.Install(context.Background(), domain.InstallRequest{
		Catalog: shipped(t), Credential: domain.Credential{OrganizationID: "rs-test", APIKey: "k"}, WorkDir: t.TempDir(), Log: &log,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tofu apply")
	assert.NotNil(t, res)
	assert.Contains(t, log.String(), "boom")
}

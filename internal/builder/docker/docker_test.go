package docker_test

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

	"github.com/massdriver-cloud/rocketsled/internal/builder/docker"
	"github.com/massdriver-cloud/rocketsled/internal/domain"
)

// fakeRunner records commands and materializes a Dockerfile on checkout so the
// context check passes.
type fakeRunner struct {
	calls   []string
	failOn  string
	path    string
	digests string
}

func (f *fakeRunner) run(_ context.Context, dir string, out io.Writer, name string, args ...string) error {
	line := name + " " + strings.Join(args, " ")
	f.calls = append(f.calls, line)
	if f.failOn != "" && strings.HasPrefix(line, f.failOn) {
		_, _ = io.WriteString(out, "boom with token ghp_secret\n")
		return errors.New("exit status 1")
	}
	if name == "git" && args[0] == "checkout" {
		ctxDir := filepath.Join(dir, f.path)
		_ = os.MkdirAll(ctxDir, 0o755)
		_ = os.WriteFile(filepath.Join(ctxDir, "Dockerfile"), []byte("FROM scratch"), 0o644)
	}
	if name == "docker" && args[0] == "inspect" {
		_, _ = io.WriteString(out, f.digests)
	}
	return nil
}

func TestBuildRunsFetchBuildLoginPush(t *testing.T) {
	fr := &fakeRunner{path: "example/hello-world", digests: "reg/app@sha256:abc\n"}
	b := &docker.Builder{Platform: "linux/amd64", LoginCommand: "aws ecr get-login-password | docker login", Run: fr.run}
	var log bytes.Buffer

	res, err := b.Build(context.Background(), domain.BuildRequest{
		CloneURL: "https://x-access-token:ghp_secret@github.com/acme/shop.git",
		SHA:      "abcdef1",
		Path:     "example/hello-world",
		Image:    "reg/app:abcdef1",
		Log:      &log,
	})
	require.NoError(t, err)
	assert.Equal(t, "reg/app:abcdef1", res.Image)
	assert.Equal(t, "sha256:abc", res.Digest)

	joined := strings.Join(fr.calls, "\n")
	assert.Contains(t, joined, "git fetch --quiet --depth 1 origin abcdef1")
	assert.Contains(t, joined, "git checkout --quiet FETCH_HEAD")
	assert.Contains(t, joined, "docker build --platform linux/amd64 --provenance=false --sbom=false --build-arg VERSION=abcdef1 -t reg/app:abcdef1 ")
	assert.Contains(t, joined, "/example/hello-world")
	assert.Contains(t, joined, "sh -c aws ecr get-login-password | docker login")
	assert.Contains(t, joined, "docker push reg/app:abcdef1")
	assert.Contains(t, log.String(), "fetching https://github.com/acme/shop.git")
	assert.NotContains(t, log.String(), "ghp_secret", "tokens never reach the log")
}

func TestBuildFailuresAreReportedAndRedacted(t *testing.T) {
	fr := &fakeRunner{failOn: "docker build"}
	b := &docker.Builder{Platform: "linux/arm64", Run: fr.run}
	var log bytes.Buffer

	_, err := b.Build(context.Background(), domain.BuildRequest{
		CloneURL: "https://oauth2:ghp_secret@gitlab.com/acme/shop.git", SHA: "1", Image: "reg/app:1", Log: &log,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "docker build")
	assert.Contains(t, log.String(), "boom with token ***")
}

func TestBuildRejectsMissingDockerfileAndEscapes(t *testing.T) {
	fr := &fakeRunner{path: "somewhere-else"}
	b := &docker.Builder{Run: fr.run}

	_, err := b.Build(context.Background(), domain.BuildRequest{CloneURL: "https://h/r.git", SHA: "1", Image: "i:1", Path: "app"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `path "app" is not a directory`)

	_, err = b.Build(context.Background(), domain.BuildRequest{CloneURL: "https://h/r.git", SHA: "1", Image: "i:1", Path: "../etc"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "escapes")
}

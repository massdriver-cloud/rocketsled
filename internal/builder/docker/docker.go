// Package docker builds images with the local git and docker CLIs. It is the
// simplest thing that turns "push to branch" into "image in the registry".
package docker

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/massdriver-cloud/rocketsled/internal/domain"
)

// Runner executes a command in dir, streaming combined output to out.
type Runner func(ctx context.Context, dir string, out io.Writer, name string, args ...string) error

// Builder shells out to git and docker.
type Builder struct {
	// Platform is passed to docker build; Lambda wants linux/amd64 or linux/arm64.
	Platform string
	// LoginCommand, when set, runs through `sh -c` before the push (registry login).
	LoginCommand string
	// Run overrides command execution; tests use it.
	Run Runner
}

var _ domain.Builder = (*Builder)(nil)

// New returns a Builder that runs real commands.
func New(platform, loginCommand string) *Builder {
	if platform == "" {
		platform = "linux/amd64"
	}
	return &Builder{Platform: platform, LoginCommand: loginCommand, Run: execRunner}
}

func (b *Builder) Build(ctx context.Context, req domain.BuildRequest) (*domain.BuildResult, error) {
	if req.CloneURL == "" || req.SHA == "" || req.Image == "" {
		return nil, errors.New("docker builder: clone URL, sha and image are required")
	}
	out := req.Log
	if out == nil {
		out = io.Discard
	}
	out = redact(out, req.CloneURL)

	dir, err := os.MkdirTemp("", "rocketsled-build-")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(dir) }()

	fmt.Fprintf(out, "==> fetching %s at %s\n", redactURL(req.CloneURL), req.SHA)
	if err := b.fetch(ctx, dir, out, req); err != nil {
		return nil, err
	}

	contextDir, err := contextPath(dir, req.Path)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(filepath.Join(contextDir, "Dockerfile")); err != nil {
		return nil, fmt.Errorf("no Dockerfile in %q", displayPath(req.Path))
	}

	fmt.Fprintf(out, "==> docker build %s (%s)\n", req.Image, b.Platform)
	buildArgs := []string{
		"build", "--platform", b.Platform, "--provenance=false", "--sbom=false",
		"--build-arg", "VERSION=" + req.SHA, "-t", req.Image, contextDir,
	}
	if err := b.Run(ctx, dir, out, "docker", buildArgs...); err != nil {
		return nil, fmt.Errorf("docker build: %w", err)
	}

	if b.LoginCommand != "" {
		fmt.Fprintln(out, "==> registry login")
		if err := b.Run(ctx, dir, out, "sh", "-c", b.LoginCommand); err != nil {
			return nil, fmt.Errorf("registry login: %w", err)
		}
	}

	fmt.Fprintf(out, "==> docker push %s\n", req.Image)
	if err := b.Run(ctx, dir, out, "docker", "push", req.Image); err != nil {
		return nil, fmt.Errorf("docker push: %w", err)
	}

	var digest bytes.Buffer
	_ = b.Run(ctx, dir, &digest, "docker", "inspect", "--format", "{{index .RepoDigests 0}}", req.Image)
	res := &domain.BuildResult{Image: req.Image}
	if _, d, ok := strings.Cut(strings.TrimSpace(digest.String()), "@"); ok {
		res.Digest = d
	}
	fmt.Fprintf(out, "==> pushed %s %s\n", res.Image, res.Digest)
	return res, nil
}

// fetch checks out exactly one commit. Fetching by sha is cheapest; hosts that
// refuse it fall back to a shallow clone of recent history.
func (b *Builder) fetch(ctx context.Context, dir string, out io.Writer, req domain.BuildRequest) error {
	steps := [][]string{
		{"git", "init", "--quiet"},
		{"git", "remote", "add", "origin", req.CloneURL},
		{"git", "fetch", "--quiet", "--depth", "1", "origin", req.SHA},
		{"git", "checkout", "--quiet", "FETCH_HEAD"},
	}
	for _, step := range steps {
		if err := b.Run(ctx, dir, out, step[0], step[1:]...); err != nil {
			if step[1] != "fetch" {
				return fmt.Errorf("%s: %w", strings.Join(step[:2], " "), err)
			}
			fmt.Fprintln(out, "==> fetch by sha refused; fetching recent history")
			if err := b.Run(ctx, dir, out, "git", "fetch", "--quiet", "--depth", "100", "origin"); err != nil {
				return fmt.Errorf("git fetch: %w", err)
			}
			if err := b.Run(ctx, dir, out, "git", "checkout", "--quiet", req.SHA); err != nil {
				return fmt.Errorf("git checkout %s: %w", req.SHA, err)
			}
			return nil
		}
	}
	return nil
}

func contextPath(root, rel string) (string, error) {
	full := filepath.Join(root, filepath.FromSlash(rel))
	if rel == "" || rel == "." {
		return root, nil
	}
	if r, err := filepath.Rel(root, full); err != nil || strings.HasPrefix(r, "..") {
		return "", fmt.Errorf("path %q escapes the repository", rel)
	}
	if st, err := os.Stat(full); err != nil || !st.IsDir() {
		return "", fmt.Errorf("path %q is not a directory in the repository", rel)
	}
	return full, nil
}

func displayPath(p string) string {
	if p == "" {
		return "."
	}
	return p
}

func execRunner(ctx context.Context, dir string, out io.Writer, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...) //nolint:gosec // git/docker with operator-controlled arguments is the point
	cmd.Dir = dir
	cmd.Stdout = out
	cmd.Stderr = out
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "DOCKER_CLI_HINTS=false")
	return cmd.Run()
}

// redact wraps out so the clone token never lands in a log.
func redact(out io.Writer, cloneURL string) io.Writer {
	u, err := url.Parse(cloneURL)
	if err != nil || u.User == nil {
		return out
	}
	secret, _ := u.User.Password()
	if secret == "" {
		secret = u.User.Username()
	}
	if secret == "" {
		return out
	}
	return &redactWriter{w: out, secret: []byte(secret)}
}

type redactWriter struct {
	w      io.Writer
	secret []byte
}

func (r *redactWriter) Write(p []byte) (int, error) {
	_, err := r.w.Write(bytes.ReplaceAll(p, r.secret, []byte("***")))
	return len(p), err
}

func redactURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	u.User = nil
	return u.String()
}

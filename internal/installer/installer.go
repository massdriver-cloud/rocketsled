// Package installer publishes a catalog into an organization with the mass CLI and
// applies its bootstrap with OpenTofu. Both are shelled out; the organization's
// credential travels through MASSDRIVER_* environment variables.
package installer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/massdriver-cloud/rocketsled/internal/domain"
)

// Runner executes a command in dir with env appended, streaming output to out.
type Runner func(ctx context.Context, dir string, env []string, out io.Writer, name string, args ...string) error

// Installer shells out to mass and tofu.
type Installer struct {
	Mass string
	Tofu string
	Run  Runner
}

var _ domain.Installer = (*Installer)(nil)

// New returns an Installer that runs real commands.
func New(massBin, tofuBin string) *Installer {
	if massBin == "" {
		massBin = "mass"
	}
	if tofuBin == "" {
		tofuBin = "tofu"
	}
	return &Installer{Mass: massBin, Tofu: tofuBin, Run: execRunner}
}

func (i *Installer) Install(ctx context.Context, req domain.InstallRequest) (*domain.InstallResult, error) {
	if req.Catalog == nil || req.Credential.APIKey == "" || req.Credential.OrganizationID == "" {
		return nil, errors.New("installer: catalog and credential are required")
	}
	out := req.Log
	if out == nil {
		out = io.Discard
	}
	env := credentialEnv(req.Credential)
	cat := req.Catalog
	say := func(format string, args ...any) { fmt.Fprintf(out, "==> "+format+"\n", args...) }

	say("publishing %s into %s", cat.Name, req.Credential.OrganizationID)
	for _, p := range cat.Platforms {
		if err := i.publishResourceType(ctx, env, out, filepath.Join(cat.Dir, "platforms", p.ID), p.ID); err != nil {
			return nil, err
		}
	}
	for _, rt := range cat.ResourceTypes {
		if err := i.publishResourceType(ctx, env, out, filepath.Join(cat.Dir, "resource-types", rt), rt); err != nil {
			return nil, err
		}
	}
	for _, b := range cat.Bundles {
		if err := i.publishBundle(ctx, env, out, filepath.Join(cat.Dir, "bundles", b), b, req.Stable); err != nil {
			return nil, err
		}
	}

	say("bootstrapping the %s project", cat.Bootstrap.Project)
	workDir := filepath.Join(req.WorkDir, "bootstrap")
	if err := prepareWorkDir(cat.Dir, workDir, req.State); err != nil {
		return nil, err
	}
	if err := i.Run(ctx, workDir, env, out, i.Tofu, "init", "-input=false", "-no-color"); err != nil {
		return nil, fmt.Errorf("tofu init: %w", err)
	}
	applyErr := i.Run(ctx, workDir, env, out, i.Tofu, "apply", "-auto-approve", "-input=false", "-no-color")
	state, _ := os.ReadFile(filepath.Join(workDir, "terraform.tfstate"))
	if applyErr != nil {
		return &domain.InstallResult{State: state}, fmt.Errorf("tofu apply: %w", applyErr)
	}
	var outputs bytes.Buffer
	if err := i.Run(ctx, workDir, env, &outputs, i.Tofu, "output", "-json", "-no-color"); err != nil {
		return &domain.InstallResult{State: state}, fmt.Errorf("tofu output: %w", err)
	}
	res, err := parseOutputs(outputs.Bytes())
	if err != nil {
		return &domain.InstallResult{State: state}, err
	}
	res.State = state
	say("done: project %s, environments %v", res.ProjectID, keys(res.Environments))
	return res, nil
}

func (i *Installer) publishResourceType(ctx context.Context, env []string, out io.Writer, dir, name string) error {
	fmt.Fprintf(out, "--> resource type %s\n", name)
	// An existing repository makes create fail; that is fine.
	_ = i.Run(ctx, dir, env, out, i.Mass, "repository", "create", name, "-t", "resource-type")
	if err := i.Run(ctx, dir, env, out, i.Mass, "resource-type", "publish", "."); err != nil {
		return fmt.Errorf("publish resource type %s: %w", name, err)
	}
	return nil
}

func (i *Installer) publishBundle(ctx context.Context, env []string, out io.Writer, dir, name string, stable bool) error {
	fmt.Fprintf(out, "--> bundle %s\n", name)
	_ = i.Run(ctx, dir, env, out, i.Mass, "repository", "create", name, "-t", "bundle")
	args := []string{"bundle", "publish", "--bundle-directory", "."}
	if !stable {
		args = append(args, "--development")
	}
	if err := i.Run(ctx, dir, env, out, i.Mass, args...); err != nil {
		return fmt.Errorf("publish bundle %s: %w", name, err)
	}
	return nil
}

// credentialEnv sets every spelling the mass CLI, the SDK and the provider read.
func credentialEnv(c domain.Credential) []string {
	env := []string{
		"MASSDRIVER_ORGANIZATION_ID=" + c.OrganizationID,
		"MASSDRIVER_ORG_ID=" + c.OrganizationID,
		"MASSDRIVER_API_KEY=" + c.APIKey,
		"MASSDRIVER_PROFILE=",
		"TF_IN_AUTOMATION=1",
	}
	if c.URL != "" {
		env = append(env, "MASSDRIVER_URL="+c.URL)
	}
	return env
}

// prepareWorkDir copies the catalog's bootstrap into a private directory and
// restores previous state so re-installs converge instead of duplicating.
func prepareWorkDir(catalogDir, workDir string, state []byte) error {
	if err := os.MkdirAll(workDir, 0o750); err != nil {
		return err
	}
	src := filepath.Join(catalogDir, "bootstrap")
	entries, err := os.ReadDir(src)
	if err != nil {
		return fmt.Errorf("read bootstrap: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".tf") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(src, e.Name()))
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(workDir, e.Name()), data, 0o600); err != nil {
			return err
		}
	}
	if len(state) > 0 {
		if err := os.WriteFile(filepath.Join(workDir, "terraform.tfstate"), state, 0o600); err != nil {
			return err
		}
	}
	return nil
}

type tofuOutput struct {
	Value json.RawMessage `json:"value"`
}

func parseOutputs(raw []byte) (*domain.InstallResult, error) {
	var outs map[string]tofuOutput
	if err := json.Unmarshal(raw, &outs); err != nil {
		return nil, fmt.Errorf("decode tofu outputs: %w", err)
	}
	res := &domain.InstallResult{Environments: map[string]string{}, Components: map[string]string{}}
	if o, ok := outs["project"]; ok {
		_ = json.Unmarshal(o.Value, &res.ProjectID)
	}
	if o, ok := outs["environments"]; ok {
		_ = json.Unmarshal(o.Value, &res.Environments)
	}
	if o, ok := outs["components"]; ok {
		_ = json.Unmarshal(o.Value, &res.Components)
	}
	if res.ProjectID == "" {
		return nil, errors.New("bootstrap produced no project output")
	}
	return res, nil
}

func keys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func execRunner(ctx context.Context, dir string, env []string, out io.Writer, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...) //nolint:gosec // mass/tofu with Rocketsled-built arguments is the point
	cmd.Dir = dir
	cmd.Stdout = out
	cmd.Stderr = out
	cmd.Env = append(os.Environ(), env...)
	return cmd.Run()
}

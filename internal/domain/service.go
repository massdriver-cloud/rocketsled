package domain

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ProjectLink is a Massdriver project an operator has opted into Rocketsled,
// with the rules for turning it into stack instances.
type ProjectLink struct {
	ID                   string `json:"id"`
	BaseEnvironment      string `json:"baseEnvironment"`
	AppComponent         string `json:"appComponent,omitempty"`
	ImageTagParam        string `json:"imageTagParam"`
	ImageRepositoryParam string `json:"imageRepositoryParam,omitempty"`
	ImageRepository      string `json:"imageRepository,omitempty"`
	// ImageRegistry, when set, makes Rocketsled build and push the app image to this
	// repository before deploying. Supports {{owner}}, {{repo}} and {{stack}}.
	ImageRegistry        string `json:"imageRegistry,omitempty"`
	CopySecrets          bool   `json:"copySecrets"`
	CopyRemoteReferences bool   `json:"copyRemoteReferences"`
}

// LinkableProject joins operator configuration with the live Massdriver project.
type LinkableProject struct {
	Link         ProjectLink `json:"link"`
	Project      *Project    `json:"project"`
	AppComponent string      `json:"appComponent"`
}

// Options wires a Service.
type Options struct {
	Platform      Platform
	Store         Store
	SCMs          []SCM
	Projects      []ProjectLink
	BaseURL       string
	WebhookSecret string
	// Builder is optional; without it images are expected to be built elsewhere.
	Builder Builder
	// RunAsync runs launch and deploy pipelines; it defaults to a goroutine.
	RunAsync func(func())
	Logger   *slog.Logger
	Now      func() time.Time
	NewID    func() string
}

// Service implements Rocketsled's use cases.
type Service struct {
	platform      Platform
	store         Store
	scms          map[Provider]SCM
	projects      []ProjectLink
	baseURL       string
	webhookSecret string
	builder       Builder
	runAsync      func(func())
	log           *slog.Logger
	now           func() time.Time
	newID         func() string
}

// NewService validates options and builds a Service.
func NewService(o Options) (*Service, error) {
	if o.Platform == nil || o.Store == nil {
		return nil, errors.New("domain: Platform and Store are required")
	}
	if len(o.SCMs) == 0 {
		return nil, errors.New("domain: at least one SCM provider is required")
	}
	s := &Service{
		platform:      o.Platform,
		store:         o.Store,
		scms:          make(map[Provider]SCM, len(o.SCMs)),
		projects:      o.Projects,
		baseURL:       strings.TrimRight(o.BaseURL, "/"),
		webhookSecret: o.WebhookSecret,
		builder:       o.Builder,
		runAsync:      o.RunAsync,
		log:           o.Logger,
		now:           o.Now,
		newID:         o.NewID,
	}
	for _, scm := range o.SCMs {
		s.scms[scm.Provider()] = scm
	}
	if s.log == nil {
		s.log = slog.Default()
	}
	if s.now == nil {
		s.now = time.Now
	}
	if s.newID == nil {
		s.newID = func() string { return uuid.Must(uuid.NewV7()).String() }
	}
	if s.runAsync == nil {
		s.runAsync = func(f func()) {
			go func() {
				defer func() {
					if r := recover(); r != nil {
						s.log.Error("background pipeline panicked", "panic", r)
					}
				}()
				f()
			}()
		}
	}
	return s, nil
}

// Providers lists configured SCM providers in a stable order.
func (s *Service) Providers() []Provider {
	out := make([]Provider, 0, len(s.scms))
	for p := range s.scms {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// SCM returns the provider by name.
func (s *Service) SCM(p Provider) (SCM, error) {
	scm, ok := s.scms[p]
	if !ok {
		return nil, fmt.Errorf("%w: provider %q is not configured", ErrNotFound, p)
	}
	return scm, nil
}

// ProjectLink returns the operator configuration for a project.
func (s *Service) ProjectLink(id string) (ProjectLink, bool) {
	for _, p := range s.projects {
		if p.ID == id {
			return p, true
		}
	}
	return ProjectLink{}, false
}

// ListProjects returns every opted-in project with its live blueprint.
func (s *Service) ListProjects(ctx context.Context) ([]LinkableProject, error) {
	out := make([]LinkableProject, 0, len(s.projects))
	for _, link := range s.projects {
		lp, err := s.linkableProject(ctx, link)
		if err != nil {
			return nil, err
		}
		out = append(out, *lp)
	}
	return out, nil
}

// GetProject returns one opted-in project with its live blueprint.
func (s *Service) GetProject(ctx context.Context, id string) (*LinkableProject, error) {
	link, ok := s.ProjectLink(id)
	if !ok {
		return nil, fmt.Errorf("%w: project %q is not available to Rocketsled", ErrNotFound, id)
	}
	return s.linkableProject(ctx, link)
}

func (s *Service) linkableProject(ctx context.Context, link ProjectLink) (*LinkableProject, error) {
	project, app, err := s.blueprint(ctx, link)
	if err != nil {
		return nil, err
	}
	return &LinkableProject{Link: link, Project: project, AppComponent: app}, nil
}

// blueprint loads the project and enriches each component with the bundle presets
// and schema exposed by the base environment's instance of that component.
func (s *Service) blueprint(ctx context.Context, link ProjectLink) (*Project, string, error) {
	project, err := s.platform.GetProject(ctx, link.ID)
	if err != nil {
		return nil, "", fmt.Errorf("load project %q: %w", link.ID, err)
	}
	base, err := s.platform.GetEnvironment(ctx, qualifiedEnvironmentID(link.ID, link.BaseEnvironment))
	if err != nil {
		return nil, "", fmt.Errorf("load base environment %q of %q: %w", link.BaseEnvironment, link.ID, err)
	}
	bySlug := make(map[string]PlatformInstance, len(base.Instances))
	for _, pi := range base.Instances {
		bySlug[pi.ComponentSlug] = pi
	}
	for i := range project.Components {
		c := &project.Components[i]
		pi, ok := bySlug[c.Slug]
		if !ok {
			continue
		}
		c.Bundle = Bundle{
			Name:         pi.BundleName,
			Version:      pi.Version,
			Presets:      pi.Presets(),
			ParamsSchema: pi.ParamsSchema,
		}
	}
	app, err := resolveAppComponent(project, link)
	if err != nil {
		return nil, "", fmt.Errorf("%w: %s", ErrConflict, err.Error())
	}
	return project, app, nil
}

// resolveAppComponent picks the component whose image tag follows the repository.
func resolveAppComponent(project *Project, link ProjectLink) (string, error) {
	if link.AppComponent != "" {
		for _, c := range project.Components {
			if c.Slug == link.AppComponent {
				return c.Slug, nil
			}
		}
		return "", fmt.Errorf("project %q has no component %q", project.ID, link.AppComponent)
	}
	for _, c := range project.Components {
		if SchemaHasPath(c.Bundle.ParamsSchema, link.ImageTagParam) {
			return c.Slug, nil
		}
	}
	return "", fmt.Errorf("project %q has no component with a %q param; set app_component", project.ID, link.ImageTagParam)
}

// ListRepos lists repositories the user can link.
func (s *Service) ListRepos(ctx context.Context, user *User) ([]Repo, error) {
	scm, err := s.SCM(user.Provider)
	if err != nil {
		return nil, err
	}
	token, err := s.store.GetCredential(ctx, user.ID)
	if err != nil {
		return nil, err
	}
	return scm.ListRepos(ctx, token)
}

// CreateStackInput links a repository to a project.
type CreateStackInput struct {
	Name         string `json:"name"`
	RepoFullName string `json:"repoFullName"`
	ProjectID    string `json:"projectId"`
	// Path is the directory holding the Dockerfile; empty means the repository root.
	Path string `json:"path"`
}

// CreateStack links the user's repository to an opted-in project and registers a
// push webhook. The returned token authenticates CI deploy callbacks and is not
// stored in plaintext.
func (s *Service) CreateStack(ctx context.Context, user *User, in CreateStackInput) (*Stack, string, error) {
	in.Name = strings.TrimSpace(in.Name)
	if in.RepoFullName == "" {
		return nil, "", Invalid("repoFullName", "choose a repository")
	}
	path, err := CleanRepoPath(in.Path)
	if err != nil {
		return nil, "", err
	}
	link, ok := s.ProjectLink(in.ProjectID)
	if !ok {
		return nil, "", Invalid("projectId", "project %q is not available to Rocketsled", in.ProjectID)
	}
	if _, _, err := s.blueprint(ctx, link); err != nil {
		return nil, "", err
	}

	scm, err := s.SCM(user.Provider)
	if err != nil {
		return nil, "", err
	}
	token, err := s.store.GetCredential(ctx, user.ID)
	if err != nil {
		return nil, "", err
	}
	repo, err := scm.GetRepo(ctx, token, in.RepoFullName)
	if err != nil {
		return nil, "", Invalid("repoFullName", "repository %q is not accessible: %s", in.RepoFullName, err.Error())
	}
	if in.Name == "" {
		in.Name = repoShortName(repo.FullName)
	}

	deployToken, tokenHash := newDeployToken()
	stack := &Stack{
		ID:              s.newID(),
		Name:            in.Name,
		Repo:            *repo,
		Path:            path,
		ProjectID:       link.ID,
		CreatedBy:       user.ID,
		CreatedAt:       s.now(),
		DeployTokenHash: tokenHash,
	}
	hookID, err := scm.CreateWebhook(ctx, token, repo.FullName, s.baseURL+"/hooks/"+string(scm.Provider()), s.webhookSecret)
	if err != nil {
		s.log.Warn("webhook registration failed; pushes will not auto-deploy", "repo", repo.FullName, "err", err)
	}
	stack.WebhookID = hookID
	if err := s.store.CreateStack(ctx, stack); err != nil {
		return nil, "", err
	}
	return stack, deployToken, nil
}

func repoShortName(fullName string) string {
	if i := strings.LastIndex(fullName, "/"); i >= 0 {
		return fullName[i+1:]
	}
	return fullName
}

func newDeployToken() (plaintext, hash string) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		panic("crypto/rand unavailable: " + err.Error())
	}
	plaintext = "rsd_" + hex.EncodeToString(buf)
	return plaintext, hashToken(plaintext)
}

func hashToken(t string) string {
	sum := sha256.Sum256([]byte(t))
	return hex.EncodeToString(sum[:])
}

// ListStacks returns every stack.
func (s *Service) ListStacks(ctx context.Context) ([]Stack, error) {
	return s.store.ListStacks(ctx)
}

// GetStack returns one stack.
func (s *Service) GetStack(ctx context.Context, id string) (*Stack, error) {
	return s.store.GetStack(ctx, id)
}

// DeleteStack removes a stack that has no instances and drops its webhook.
func (s *Service) DeleteStack(ctx context.Context, user *User, id string) error {
	stack, err := s.store.GetStack(ctx, id)
	if err != nil {
		return err
	}
	instances, err := s.store.ListInstances(ctx, id)
	if err != nil {
		return err
	}
	if len(instances) > 0 {
		return fmt.Errorf("%w: destroy the stack's %d instance(s) first", ErrConflict, len(instances))
	}
	if stack.WebhookID != "" {
		if scm, scmErr := s.SCM(stack.Repo.Provider); scmErr == nil {
			token, tokErr := s.store.GetCredential(ctx, user.ID)
			if tokErr == nil {
				if err := scm.DeleteWebhook(ctx, token, stack.Repo.FullName, stack.WebhookID); err != nil {
					s.log.Warn("webhook removal failed", "repo", stack.Repo.FullName, "err", err)
				}
			}
		}
	}
	return s.store.DeleteStack(ctx, id)
}

// CreateInstanceInput launches a new instance of a stack.
type CreateInstanceInput struct {
	Slug    string            `json:"slug"`
	Name    string            `json:"name"`
	Branch  string            `json:"branch"`
	Presets map[string]string `json:"presets"`
}

// CreateInstance forks the project's base environment, applies the chosen preset
// to each component, points the app component at the branch head and deploys.
func (s *Service) CreateInstance(ctx context.Context, user *User, stackID string, in CreateInstanceInput) (*Instance, error) {
	stack, err := s.store.GetStack(ctx, stackID)
	if err != nil {
		return nil, err
	}
	link, ok := s.ProjectLink(stack.ProjectID)
	if !ok {
		return nil, fmt.Errorf("%w: project %q is no longer available to Rocketsled", ErrConflict, stack.ProjectID)
	}
	if !ValidInstanceSlug(in.Slug) {
		return nil, Invalid("slug", "use 1-20 lowercase letters or digits")
	}
	if in.Branch == "" {
		in.Branch = stack.Repo.DefaultBranch
	}
	if strings.TrimSpace(in.Name) == "" {
		in.Name = in.Slug
	}
	project, appSlug, err := s.blueprint(ctx, link)
	if err != nil {
		return nil, err
	}
	presets, err := resolvePresets(project, in.Presets)
	if err != nil {
		return nil, err
	}
	existing, err := s.store.ListInstances(ctx, stackID)
	if err != nil {
		return nil, err
	}
	for _, e := range existing {
		if e.Slug == in.Slug {
			return nil, fmt.Errorf("%w: instance %q already exists", ErrConflict, in.Slug)
		}
	}

	scm, err := s.SCM(user.Provider)
	if err != nil {
		return nil, err
	}
	token, err := s.store.GetCredential(ctx, user.ID)
	if err != nil {
		return nil, err
	}
	sha, err := scm.BranchHead(ctx, token, stack.Repo.FullName, in.Branch)
	if err != nil {
		return nil, Invalid("branch", "branch %q not found in %s", in.Branch, stack.Repo.FullName)
	}

	baseEnvID := qualifiedEnvironmentID(link.ID, link.BaseEnvironment)
	env, err := s.platform.ForkEnvironment(ctx, ForkRequest{
		ParentEnvironmentID:     baseEnvID,
		ID:                      in.Slug,
		Name:                    in.Name,
		Description:             fmt.Sprintf("Rocketsled instance of %s (%s)", stack.Repo.FullName, in.Branch),
		CopyEnvironmentDefaults: true,
		CopySecrets:             link.CopySecrets,
		CopyRemoteReferences:    link.CopyRemoteReferences,
	})
	if err != nil {
		return nil, fmt.Errorf("fork environment: %w", err)
	}
	if env == nil {
		return nil, errors.New("fork environment: platform returned no environment")
	}

	now := s.now()
	inst := &Instance{
		ID:            s.newID(),
		StackID:       stack.ID,
		Slug:          in.Slug,
		Name:          in.Name,
		Branch:        in.Branch,
		EnvironmentID: env.ID,
		Presets:       in.Presets,
		State:         InstanceStateLaunching,
		CreatedBy:     user.ID,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if inst.Presets == nil {
		inst.Presets = map[string]string{}
	}
	if err := s.store.CreateInstance(ctx, inst); err != nil {
		return nil, err
	}

	build := s.buildPlan(stack, link, sha)
	deploy := &Deploy{
		ID:          s.newID(),
		InstanceID:  inst.ID,
		Number:      1,
		Trigger:     TriggerLaunch,
		Ref:         in.Branch,
		SHA:         sha,
		ImageTag:    build.tag,
		Image:       build.image,
		Status:      build.initialStatus(),
		RequestedBy: user.ID,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := s.store.CreateDeploy(ctx, deploy); err != nil {
		return nil, err
	}
	if build.needed {
		build.cloneURL = scm.CloneURL(stack.Repo, token)
	}
	run := *deploy
	s.runAsync(func() {
		bg := context.Background()
		if err := s.completeLaunch(bg, env, baseEnvID, stack, link, appSlug, presets, &run, build); err != nil {
			s.log.Warn("launch failed", "instance", inst.ID, "err", err)
			run.Status = DeployStatusFailed
			run.Message = err.Error()
			run.UpdatedAt = s.now()
			_ = s.store.UpdateDeploy(bg, &run)
			if current, getErr := s.store.GetInstance(bg, inst.ID); getErr == nil {
				current.State = InstanceStateFailed
				current.UpdatedAt = s.now()
				_ = s.store.UpdateInstance(bg, current)
			}
		}
	})
	return inst, nil
}

// completeLaunch builds the image when needed, copies each chosen preset (and the
// app image) onto the forked instances, then deploys the environment.
func (s *Service) completeLaunch(ctx context.Context, env *Environment, baseEnvID string, stack *Stack, link ProjectLink, appSlug string, presets map[string]Preset, run *Deploy, build buildPlan) error {
	if err := s.buildImage(ctx, run, build, stack); err != nil {
		return err
	}
	for _, pi := range env.Instances {
		overrides := map[string]any{}
		changed := false
		if preset, ok := presets[pi.ComponentSlug]; ok {
			overrides = MergeParams(overrides, preset.Params)
			changed = true
		}
		if pi.ComponentSlug == appSlug {
			overrides = s.imageParams(overrides, stack, link, run.ImageTag)
			changed = true
		}
		if !changed {
			continue
		}
		sourceID := baseEnvID + "-" + pi.ComponentSlug
		if err := s.platform.CopyInstance(ctx, sourceID, pi.ID, overrides); err != nil {
			return fmt.Errorf("configure %s: %w", pi.ComponentSlug, err)
		}
	}
	if err := s.platform.DeployEnvironment(ctx, env.ID); err != nil {
		return fmt.Errorf("deploy environment: %w", err)
	}
	run.Status = DeployStatusRunning
	run.UpdatedAt = s.now()
	return s.store.UpdateDeploy(ctx, run)
}

// buildPlan decides whether Rocketsled builds the image for a commit.
type buildPlan struct {
	needed   bool
	image    string
	tag      string
	cloneURL string
	path     string
}

func (b buildPlan) initialStatus() DeployStatus {
	if b.needed {
		return DeployStatusBuilding
	}
	return DeployStatusQueued
}

func (s *Service) buildPlan(stack *Stack, link ProjectLink, sha string) buildPlan {
	registry := s.imageRegistry(stack, link)
	if s.builder == nil || registry == "" {
		return buildPlan{tag: sha}
	}
	return buildPlan{needed: true, image: registry + ":" + sha, tag: sha, path: stack.Path}
}

func (s *Service) imageRegistry(stack *Stack, link ProjectLink) string {
	if link.ImageRegistry == "" {
		return ""
	}
	owner, name, _ := strings.Cut(stack.Repo.FullName, "/")
	return strings.NewReplacer(
		"{{owner}}", strings.ToLower(owner),
		"{{repo}}", strings.ToLower(name),
		"{{stack}}", slugify(stack.Name),
	).Replace(link.ImageRegistry)
}

// buildImage runs the builder for a run and records its log.
func (s *Service) buildImage(ctx context.Context, run *Deploy, build buildPlan, stack *Stack) error {
	if !build.needed {
		return nil
	}
	logBuf := &tailBuffer{limit: maxBuildLog}
	res, err := s.builder.Build(ctx, BuildRequest{
		CloneURL: build.cloneURL,
		SHA:      run.SHA,
		Path:     build.path,
		Image:    build.image,
		Log:      logBuf,
	})
	run.Log = logBuf.String()
	if err != nil {
		return fmt.Errorf("build %s at %s: %w", stack.Repo.FullName, shortSHA(run.SHA), err)
	}
	if res != nil && res.Image != "" {
		run.Image = res.Image
	}
	run.Status = DeployStatusQueued
	run.UpdatedAt = s.now()
	return s.store.UpdateDeploy(ctx, run)
}

func (s *Service) imageParams(params map[string]any, stack *Stack, link ProjectLink, tag string) map[string]any {
	params = SetParam(params, link.ImageTagParam, tag)
	if link.ImageRepositoryParam != "" {
		owner, name, _ := strings.Cut(stack.Repo.FullName, "/")
		repo := strings.NewReplacer("{{owner}}", owner, "{{repo}}", name, "{{full_name}}", stack.Repo.FullName).Replace(link.ImageRepository)
		params = SetParam(params, link.ImageRepositoryParam, repo)
	}
	return params
}

// resolvePresets validates the chosen preset names against the blueprint.
func resolvePresets(project *Project, chosen map[string]string) (map[string]Preset, error) {
	out := make(map[string]Preset, len(chosen))
	for slug, name := range chosen {
		if name == "" {
			continue
		}
		var component *Component
		for i := range project.Components {
			if project.Components[i].Slug == slug {
				component = &project.Components[i]
				break
			}
		}
		if component == nil {
			return nil, Invalid("presets."+slug, "project has no component %q", slug)
		}
		found := false
		for _, p := range component.Bundle.Presets {
			if p.Name == name {
				out[slug] = p
				found = true
				break
			}
		}
		if !found {
			return nil, Invalid("presets."+slug, "bundle %s has no preset %q", component.Bundle.Name, name)
		}
	}
	return out, nil
}

// qualifiedEnvironmentID accepts either a local slug or a full <project>-<slug> ID.
func qualifiedEnvironmentID(projectID, env string) string {
	if strings.HasPrefix(env, projectID+"-") {
		return env
	}
	return projectID + "-" + env
}

// ListInstances lists a stack's instances.
func (s *Service) ListInstances(ctx context.Context, stackID string) ([]Instance, error) {
	if _, err := s.store.GetStack(ctx, stackID); err != nil {
		return nil, err
	}
	return s.store.ListInstances(ctx, stackID)
}

// GetInstance returns one instance record.
func (s *Service) GetInstance(ctx context.Context, id string) (*Instance, error) {
	return s.store.GetInstance(ctx, id)
}

// DeployInput requests a deploy of an instance.
type DeployInput struct {
	Trigger  DeployTrigger `json:"-"`
	Ref      string        `json:"ref"`
	SHA      string        `json:"sha"`
	ImageTag string        `json:"imageTag"`
	Message  string        `json:"message"`
	// Actor is the user or system requesting the deploy.
	Actor *User `json:"-"`
}

// Deploy points the app component at a commit and provisions it.
func (s *Service) Deploy(ctx context.Context, instanceID string, in DeployInput) (*Deploy, error) {
	inst, err := s.store.GetInstance(ctx, instanceID)
	if err != nil {
		return nil, err
	}
	if inst.State == InstanceStateDecommissioning {
		return nil, fmt.Errorf("%w: instance is being decommissioned", ErrConflict)
	}
	stack, err := s.store.GetStack(ctx, inst.StackID)
	if err != nil {
		return nil, err
	}
	link, ok := s.ProjectLink(stack.ProjectID)
	if !ok {
		return nil, fmt.Errorf("%w: project %q is no longer available to Rocketsled", ErrConflict, stack.ProjectID)
	}
	if in.Trigger == "" {
		in.Trigger = TriggerManual
	}
	if in.Ref == "" {
		in.Ref = inst.Branch
	}
	if in.SHA == "" {
		if in.Actor == nil {
			return nil, Invalid("sha", "a commit sha is required")
		}
		scm, scmErr := s.SCM(in.Actor.Provider)
		if scmErr != nil {
			return nil, scmErr
		}
		token, tokErr := s.store.GetCredential(ctx, in.Actor.ID)
		if tokErr != nil {
			return nil, tokErr
		}
		in.SHA, err = scm.BranchHead(ctx, token, stack.Repo.FullName, in.Ref)
		if err != nil {
			return nil, Invalid("ref", "%q not found in %s", in.Ref, stack.Repo.FullName)
		}
	}
	if in.ImageTag == "" {
		in.ImageTag = in.SHA
	}

	_, appSlug, err := s.blueprint(ctx, link)
	if err != nil {
		return nil, err
	}
	env, err := s.platform.GetEnvironment(ctx, inst.EnvironmentID)
	if err != nil {
		return nil, fmt.Errorf("load environment: %w", err)
	}
	var app *PlatformInstance
	for i := range env.Instances {
		if env.Instances[i].ComponentSlug == appSlug {
			app = &env.Instances[i]
		}
	}
	if app == nil {
		return nil, fmt.Errorf("%w: environment %s has no %q instance", ErrConflict, env.ID, appSlug)
	}
	if app.Params == nil {
		return nil, fmt.Errorf("%w: %s has no configuration yet; launch has not finished", ErrConflict, app.ID)
	}

	previous, err := s.store.ListDeploys(ctx, inst.ID, 1)
	if err != nil {
		return nil, err
	}
	build := s.buildPlan(stack, link, in.SHA)
	if in.ImageTag != "" && in.ImageTag != in.SHA {
		// A caller-supplied tag means the image already exists; do not rebuild it.
		build = buildPlan{tag: in.ImageTag}
	}
	if build.needed {
		token, tokErr := s.buildCredential(ctx, in.Actor, stack)
		if tokErr != nil {
			return nil, tokErr
		}
		scm, scmErr := s.SCM(stack.Repo.Provider)
		if scmErr != nil {
			return nil, scmErr
		}
		build.cloneURL = scm.CloneURL(stack.Repo, token)
	}
	now := s.now()
	deploy := &Deploy{
		ID:         s.newID(),
		InstanceID: inst.ID,
		Number:     1,
		Trigger:    in.Trigger,
		Ref:        in.Ref,
		SHA:        in.SHA,
		ImageTag:   build.tag,
		Image:      build.image,
		Status:     build.initialStatus(),
		Message:    in.Message,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if len(previous) > 0 {
		deploy.Number = previous[0].Number + 1
	}
	if in.Actor != nil {
		deploy.RequestedBy = in.Actor.ID
	}
	if err := s.store.CreateDeploy(ctx, deploy); err != nil {
		return nil, err
	}

	message := in.Message
	if message == "" {
		message = fmt.Sprintf("rocketsled run #%d: %s@%s", deploy.Number, in.Ref, shortSHA(in.SHA))
	}
	params := s.imageParams(app.Params, stack, link, build.tag)
	appID := app.ID
	run := *deploy
	s.runAsync(func() {
		bg := context.Background()
		err := s.buildImage(bg, &run, build, stack)
		if err == nil {
			var deploymentID string
			deploymentID, err = s.platform.CreateDeployment(bg, appID, params, message)
			if err != nil {
				err = fmt.Errorf("deploy %s: %w", appSlug, err)
			} else {
				run.PlatformDeploymentIDs = []string{deploymentID}
				run.Status = DeployStatusRunning
			}
		}
		if err != nil {
			s.log.Warn("deploy failed", "instance", inst.ID, "run", run.Number, "err", err)
			run.Status = DeployStatusFailed
			run.Message = err.Error()
		}
		run.UpdatedAt = s.now()
		_ = s.store.UpdateDeploy(bg, &run)
	})
	return deploy, nil
}

// buildCredential picks the SCM token used to clone for a build: the acting user's,
// or the stack owner's for webhook and CI triggered runs.
func (s *Service) buildCredential(ctx context.Context, actor *User, stack *Stack) (string, error) {
	userID := stack.CreatedBy
	if actor != nil {
		userID = actor.ID
	}
	token, err := s.store.GetCredential(ctx, userID)
	if err != nil {
		return "", fmt.Errorf("%w: no stored credential for %s to clone %s", ErrConflict, userID, stack.Repo.FullName)
	}
	return token, nil
}

func shortSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

// HandlePush deploys every instance following the pushed branch.
func (s *Service) HandlePush(ctx context.Context, ev PushEvent) ([]Deploy, error) {
	if ev.Deleted || ev.Branch == "" {
		return nil, nil
	}
	stacks, err := s.store.FindStacksByRepo(ctx, ev.Provider, ev.RepoFullName)
	if err != nil {
		return nil, err
	}
	var deploys []Deploy
	var errs []error
	for _, stack := range stacks {
		instances, listErr := s.store.ListInstances(ctx, stack.ID)
		if listErr != nil {
			errs = append(errs, listErr)
			continue
		}
		for _, inst := range instances {
			if inst.Branch != ev.Branch {
				continue
			}
			d, deployErr := s.Deploy(ctx, inst.ID, DeployInput{Trigger: TriggerWebhook, Ref: ev.Branch, SHA: ev.SHA})
			if deployErr != nil {
				errs = append(errs, fmt.Errorf("%s/%s: %w", stack.Name, inst.Slug, deployErr))
				continue
			}
			deploys = append(deploys, *d)
		}
	}
	return deploys, errors.Join(errs...)
}

// DeployFromCI deploys the stack's instances following ref, authenticated by the
// stack's deploy token. It lets a pipeline that just pushed an image trigger the roll-out.
func (s *Service) DeployFromCI(ctx context.Context, stackID, token string, in DeployInput) ([]Deploy, error) {
	stack, err := s.store.GetStack(ctx, stackID)
	if err != nil {
		return nil, err
	}
	if subtle.ConstantTimeCompare([]byte(hashToken(token)), []byte(stack.DeployTokenHash)) != 1 {
		return nil, fmt.Errorf("%w: invalid deploy token", ErrForbidden)
	}
	if in.SHA == "" {
		return nil, Invalid("sha", "a commit sha is required")
	}
	if in.Ref == "" {
		return nil, Invalid("ref", "a branch is required")
	}
	instances, err := s.store.ListInstances(ctx, stack.ID)
	if err != nil {
		return nil, err
	}
	in.Trigger = TriggerCI
	var deploys []Deploy
	var errs []error
	for _, inst := range instances {
		if inst.Branch != in.Ref {
			continue
		}
		d, deployErr := s.Deploy(ctx, inst.ID, in)
		if deployErr != nil {
			errs = append(errs, deployErr)
			continue
		}
		deploys = append(deploys, *d)
	}
	return deploys, errors.Join(errs...)
}

// InstanceView is an instance with live platform state and recent runs.
type InstanceView struct {
	Instance    Instance     `json:"instance"`
	Environment *Environment `json:"environment,omitempty"`
	Deploys     []Deploy     `json:"deploys"`
	// Gone is true when a decommissioned instance has been fully removed.
	Gone bool `json:"gone"`
}

// InstanceStatus returns live state, refreshing recorded run statuses from the
// platform. A decommissioning instance whose packages are all gone is deleted.
func (s *Service) InstanceStatus(ctx context.Context, id string) (*InstanceView, error) {
	inst, err := s.store.GetInstance(ctx, id)
	if err != nil {
		return nil, err
	}
	view := &InstanceView{Instance: *inst, Deploys: []Deploy{}}
	env, err := s.platform.GetEnvironment(ctx, inst.EnvironmentID)
	if err != nil {
		if inst.State == InstanceStateDecommissioning && errors.Is(err, ErrNotFound) {
			view.Gone = true
			_ = s.store.DeleteInstance(ctx, inst.ID)
			return view, nil
		}
		return nil, fmt.Errorf("load environment: %w", err)
	}
	for i := range env.Instances {
		env.Instances[i].Params = nil
	}
	if env.Unfulfilled == nil {
		env.Unfulfilled = []UnfulfilledDependency{}
	}
	view.Environment = env

	deploys, err := s.store.ListDeploys(ctx, inst.ID, 25)
	if err != nil {
		return nil, err
	}
	for i := range deploys {
		if refreshed := s.refreshDeploy(ctx, &deploys[i], env); refreshed {
			_ = s.store.UpdateDeploy(ctx, &deploys[i])
		}
	}
	view.Deploys = deploys

	if state := deriveState(inst.State, env); state != inst.State {
		inst.State = state
		inst.UpdatedAt = s.now()
		if err := s.store.UpdateInstance(ctx, inst); err != nil {
			return nil, err
		}
		view.Instance = *inst
	}
	if inst.State == InstanceStateDecommissioning && allDecommissioned(env) {
		if err := s.platform.DeleteEnvironment(ctx, env.ID); err == nil {
			_ = s.store.DeleteInstance(ctx, inst.ID)
			view.Gone = true
		} else {
			s.log.Warn("environment delete deferred", "environment", env.ID, "err", err)
		}
	}
	return view, nil
}

func (s *Service) refreshDeploy(ctx context.Context, d *Deploy, env *Environment) bool {
	if d.Status != DeployStatusQueued && d.Status != DeployStatusRunning {
		return false
	}
	var agg DeployStatus
	if len(d.PlatformDeploymentIDs) == 0 {
		if d.Trigger != TriggerLaunch || d.Status == DeployStatusQueued {
			// Still in the pipeline: nothing on the platform to read yet.
			return false
		}
		// Environment-wide runs have no deployment IDs; read the instance statuses.
		agg = environmentRunStatus(env)
	} else {
		agg = DeployStatusSucceeded
		for _, id := range d.PlatformDeploymentIDs {
			dep, err := s.platform.GetDeployment(ctx, id)
			if err != nil {
				return false
			}
			switch ClassifyDeployment(dep.Status) {
			case DeployStatusFailed:
				agg = DeployStatusFailed
			case DeployStatusQueued, DeployStatusBuilding, DeployStatusRunning:
				if agg != DeployStatusFailed {
					agg = DeployStatusRunning
				}
			case DeployStatusSucceeded:
			}
		}
	}
	if agg == d.Status {
		return false
	}
	d.Status = agg
	d.UpdatedAt = s.now()
	return true
}

func environmentRunStatus(env *Environment) DeployStatus {
	if len(env.Instances) == 0 {
		return DeployStatusRunning
	}
	for _, pi := range env.Instances {
		if strings.EqualFold(pi.Status, "FAILED") {
			return DeployStatusFailed
		}
	}
	for _, pi := range env.Instances {
		if !strings.EqualFold(pi.Status, "PROVISIONED") {
			return DeployStatusRunning
		}
	}
	return DeployStatusSucceeded
}

// ClassifyDeployment buckets a Massdriver DeploymentStatus into Rocketsled's.
func ClassifyDeployment(status DeploymentStatus) DeployStatus {
	switch strings.ToUpper(string(status)) {
	case "FAILED", "ABORTED", "REJECTED":
		return DeployStatusFailed
	case "COMPLETED":
		return DeployStatusSucceeded
	case "PROPOSED", "APPROVED", "PENDING":
		return DeployStatusQueued
	default:
		return DeployStatusRunning
	}
}

func deriveState(current InstanceState, env *Environment) InstanceState {
	if current == InstanceStateDecommissioning || len(env.Instances) == 0 {
		return current
	}
	anyFailed, allProvisioned := false, true
	for _, pi := range env.Instances {
		switch strings.ToUpper(pi.Status) {
		case "FAILED":
			anyFailed = true
		case "PROVISIONED":
		default:
			allProvisioned = false
		}
	}
	switch {
	case anyFailed:
		return InstanceStateFailed
	case allProvisioned:
		return InstanceStateRunning
	default:
		return InstanceStateLaunching
	}
}

func allDecommissioned(env *Environment) bool {
	for _, pi := range env.Instances {
		switch strings.ToUpper(pi.Status) {
		case "DECOMMISSIONED", "INITIALIZED":
		default:
			return false
		}
	}
	return true
}

// DestroyInstance decommissions the environment. The record is removed once every
// package is gone (see InstanceStatus).
func (s *Service) DestroyInstance(ctx context.Context, id string) (*Instance, error) {
	inst, err := s.store.GetInstance(ctx, id)
	if err != nil {
		return nil, err
	}
	if inst.State == InstanceStateDecommissioning {
		return inst, nil
	}
	if err := s.platform.DecommissionEnvironment(ctx, inst.EnvironmentID); err != nil {
		return nil, fmt.Errorf("decommission environment: %w", err)
	}
	inst.State = InstanceStateDecommissioning
	inst.UpdatedAt = s.now()
	if err := s.store.UpdateInstance(ctx, inst); err != nil {
		return nil, err
	}
	return inst, nil
}

// SignIn upserts the user returned by an SCM provider and stores their token.
func (s *Service) SignIn(ctx context.Context, provider Provider, code, redirectURL string) (*User, error) {
	scm, err := s.SCM(provider)
	if err != nil {
		return nil, err
	}
	token, err := scm.Exchange(ctx, code, redirectURL)
	if err != nil {
		return nil, fmt.Errorf("exchange code: %w", err)
	}
	user, err := scm.CurrentUser(ctx, token)
	if err != nil {
		return nil, fmt.Errorf("fetch user: %w", err)
	}
	user.ID = string(provider) + ":" + user.ProviderUserID
	user.Provider = provider
	if user.CreatedAt.IsZero() {
		user.CreatedAt = s.now()
	}
	if err := s.store.UpsertUser(ctx, user); err != nil {
		return nil, err
	}
	if err := s.store.SaveCredential(ctx, user.ID, token); err != nil {
		return nil, err
	}
	return user, nil
}

// GetUser loads a user by ID.
func (s *Service) GetUser(ctx context.Context, id string) (*User, error) {
	return s.store.GetUser(ctx, id)
}

// ListProjectLinks returns the operator's project configuration.
func (s *Service) ListProjectLinks() []ProjectLink {
	return append([]ProjectLink(nil), s.projects...)
}

// maxBuildLog caps the build output stored per run.
const maxBuildLog = 64 << 10

// tailBuffer keeps the last limit bytes written to it.
type tailBuffer struct {
	buf   []byte
	limit int
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.buf = append(t.buf, p...)
	if len(t.buf) > t.limit {
		t.buf = append([]byte("…\n"), t.buf[len(t.buf)-t.limit:]...)
	}
	return len(p), nil
}

func (t *tailBuffer) String() string { return string(t.buf) }

// CleanRepoPath normalizes a directory inside a repository. It rejects escapes.
func CleanRepoPath(p string) (string, error) {
	p = strings.Trim(strings.TrimSpace(p), "/")
	if p == "" || p == "." {
		return "", nil
	}
	clean := path.Clean(p)
	if clean == ".." || strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "/") {
		return "", Invalid("path", "must stay inside the repository")
	}
	return clean, nil
}

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

func slugify(s string) string {
	return strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(s), "-"), "-")
}

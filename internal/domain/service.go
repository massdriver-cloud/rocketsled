package domain

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// AccountMode decides where a new account's organization comes from.
type AccountMode string

const (
	// AccountModeShared binds every account to one configured organization. Development only.
	AccountModeShared AccountMode = "shared"
	// AccountModeOrganization creates an organization per account.
	AccountModeOrganization AccountMode = "organization"
)

// Options wires a Service.
type Options struct {
	Platforms PlatformFactory
	Store     Store
	SCMs      []SCM
	Installer Installer
	Catalogs  []Catalog
	// Offered, when set, are the catalog IDs new accounts may install; the
	// rest of Catalogs are loaded only for accounts that already installed them.
	Offered []string
	Mode    AccountMode
	// SharedCredential is used by every account in AccountModeShared.
	SharedCredential Credential
	// OrgCreator is a user credential allowed to create organizations (AccountModeOrganization).
	OrgCreator Credential
	// DataDir holds per-account working directories.
	DataDir string
	// StableReleases publishes catalog bundles as stable releases.
	StableReleases bool
	// PaaS is the offer in tenant mode.
	PaaS *PaaS
	// Password enables password sign-in.
	Password string
	// Open enables sign-up with just a name; InviteCode, when set, gates it.
	Open       bool
	InviteCode string
	RunAsync   func(func())
	Logger     *slog.Logger
	Now        func() time.Time
	NewID      func() string
	NewOrgID   func() string
}

// Service implements Rocketsled's use cases.
type Service struct {
	platforms  PlatformFactory
	store      Store
	scms       map[Provider]SCM
	installer  Installer
	catalogs   []Catalog
	offered    []string
	mode       AccountMode
	shared     Credential
	orgCreator Credential
	dataDir    string
	stable     bool
	runAsync   func(func())
	log        *slog.Logger
	now        func() time.Time
	newID      func() string
	newOrgID   func() string
	paas       *PaaS
	password   string
	open       bool
	inviteCode string
	offers     offerCache
	reaped     reapLog
	inFlight   sync.Map
}

// NewService validates options and builds a Service.
func NewService(o Options) (*Service, error) {
	if o.Platforms == nil || o.Store == nil || o.Installer == nil {
		return nil, errors.New("domain: Platforms, Store and Installer are required")
	}
	if len(o.SCMs) == 0 && o.Password == "" && !o.Open {
		return nil, errors.New("domain: at least one sign-in provider is required")
	}
	switch o.Mode {
	case AccountModeShared:
		if o.SharedCredential.APIKey == "" || o.SharedCredential.OrganizationID == "" {
			return nil, errors.New("domain: shared mode needs a shared credential")
		}
	case AccountModeOrganization:
		if o.OrgCreator.APIKey == "" {
			return nil, errors.New("domain: organization mode needs an organization-creator credential")
		}
	case AccountModeTenant:
		if o.SharedCredential.APIKey == "" || o.SharedCredential.OrganizationID == "" || o.PaaS == nil {
			return nil, errors.New("domain: tenant mode needs a credential and a PaaS")
		}
	default:
		return nil, fmt.Errorf("domain: unknown account mode %q", o.Mode)
	}
	s := &Service{
		platforms:  o.Platforms,
		store:      o.Store,
		scms:       make(map[Provider]SCM, len(o.SCMs)),
		installer:  o.Installer,
		catalogs:   o.Catalogs,
		offered:    o.Offered,
		mode:       o.Mode,
		shared:     o.SharedCredential,
		orgCreator: o.OrgCreator,
		dataDir:    o.DataDir,
		stable:     o.StableReleases,
		runAsync:   o.RunAsync,
		log:        o.Logger,
		now:        o.Now,
		newID:      o.NewID,
		newOrgID:   o.NewOrgID,
		paas:       o.PaaS,
		password:   o.Password,
		open:       o.Open,
		inviteCode: o.InviteCode,
		reaped:     reapLog{at: map[string]time.Time{}},
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
	if s.newOrgID == nil {
		s.newOrgID = NewOrganizationID
	}
	if s.runAsync == nil {
		s.runAsync = func(f func()) {
			go func() {
				defer func() {
					if r := recover(); r != nil {
						s.log.Error("background work panicked", "panic", r)
					}
				}()
				f()
			}()
		}
	}
	return s, nil
}

// --- sign in -----------------------------------------------------------------

// Providers lists configured sign-in providers in a stable order.
func (s *Service) Providers() []Provider {
	out := make([]Provider, 0, len(s.scms))
	for p := range s.scms {
		out = append(out, p)
	}
	if s.password != "" {
		out = append(out, ProviderPassword)
	}
	if s.open {
		out = append(out, ProviderOpen)
	}
	slices.Sort(out)
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

// SignIn upserts the user a provider returns for an OAuth code.
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
	return user, nil
}

// GetUser loads a user by ID.
func (s *Service) GetUser(ctx context.Context, id string) (*User, error) {
	u, err := s.store.GetUser(ctx, id)
	if err != nil && (s.open || s.password != "") && errors.Is(err, ErrNotFound) {
		if rebuilt, ok := s.sessionUser(id); ok {
			rebuilt.CreatedAt = s.now()
			if err := s.store.UpsertUser(ctx, rebuilt); err != nil {
				return nil, err
			}
			return rebuilt, nil
		}
	}
	return u, err
}

// --- accounts ----------------------------------------------------------------

// AccountFor returns the user's account, or ErrNotFound before signup completes.
func (s *Service) AccountFor(ctx context.Context, user *User) (*Account, error) {
	return s.store.GetAccountByOwner(ctx, user.ID)
}

// CreateAccount signs the user up: in shared mode the account borrows the
// configured organization; otherwise a new organization and an admin service
// account are created for it.
func (s *Service) CreateAccount(ctx context.Context, user *User, name string) (*Account, error) {
	if existing, err := s.store.GetAccountByOwner(ctx, user.ID); err == nil {
		return existing, nil
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = user.Name
	}
	if name == "" {
		name = user.Login
	}
	now := s.now()
	account := &Account{
		ID:          s.newID(),
		Name:        name,
		OwnerUserID: user.ID,
		Status:      AccountStatusNew,
		Platforms:   map[string]string{},
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	var cred Credential
	switch s.mode {
	case AccountModeShared:
		cred = s.shared
	case AccountModeTenant:
		cred = s.shared
		account.Status = AccountStatusReady
	case AccountModeOrganization:
		var err error
		cred, err = s.provisionOrganization(ctx)
		if err != nil {
			return nil, err
		}
	}
	account.OrganizationID = cred.OrganizationID
	if orgName, err := s.platforms(cred).Ping(ctx); err == nil {
		account.OrganizationName = orgName
	} else {
		s.log.Warn("organization ping failed", "organization", cred.OrganizationID, "err", err)
		account.OrganizationName = cred.OrganizationID
	}
	if err := s.store.CreateAccount(ctx, account, cred); err != nil {
		return nil, err
	}
	return account, nil
}

func (s *Service) provisionOrganization(ctx context.Context) (Credential, error) {
	id := s.newOrgID()
	name := "RocketSled " + uuid.NewString()
	creator := s.platforms(Credential{URL: s.orgCreator.URL, APIKey: s.orgCreator.APIKey})
	if err := creator.CreateOrganization(ctx, id, name); err != nil {
		return Credential{}, fmt.Errorf("create organization %s: %w", id, err)
	}
	inOrg := s.platforms(Credential{URL: s.orgCreator.URL, OrganizationID: id, APIKey: s.orgCreator.APIKey})
	token, err := inOrg.CreateServiceAccount(ctx, "rocketsled")
	if err != nil {
		return Credential{}, fmt.Errorf("create service account in %s: %w", id, err)
	}
	return Credential{URL: s.orgCreator.URL, OrganizationID: id, APIKey: token}, nil
}

// platformFor builds a Platform bound to the account's credential.
func (s *Service) platformFor(ctx context.Context, account *Account) (Platform, Credential, error) {
	cred, err := s.store.GetAccountCredential(ctx, account.ID)
	if err != nil {
		return nil, Credential{}, err
	}
	return s.platforms(cred), cred, nil
}

// --- catalogs ----------------------------------------------------------------

// ListCatalogs returns the catalogs a customer can install.
func (s *Service) ListCatalogs() []Catalog {
	out := []Catalog{}
	for _, c := range s.catalogs {
		if s.isOffered(c.ID) {
			out = append(out, c)
		}
	}
	return out
}

func (s *Service) isOffered(catalogID string) bool {
	return len(s.offered) == 0 || slices.Contains(s.offered, catalogID)
}

// GetCatalog finds a catalog by ID.
func (s *Service) GetCatalog(id string) (*Catalog, error) {
	for i := range s.catalogs {
		if s.catalogs[i].ID == id {
			c := s.catalogs[i]
			return &c, nil
		}
	}
	return nil, fmt.Errorf("%w: catalog %q", ErrNotFound, id)
}

// InstallCatalog publishes the catalog into the account's organization and runs
// its bootstrap. It returns as soon as the install is queued; the account's
// status and install log track progress.
func (s *Service) InstallCatalog(ctx context.Context, account *Account, catalogID string) (*Account, error) {
	switch account.Status {
	case AccountStatusNew, AccountStatusFailed:
	case AccountStatusInstalling:
		return nil, fmt.Errorf("%w: an install is already running", ErrConflict)
	case AccountStatusNeedsCredentials, AccountStatusReady:
		return nil, fmt.Errorf("%w: a catalog is already installed", ErrConflict)
	}
	catalog, err := s.GetCatalog(catalogID)
	if err == nil && !s.isOffered(catalogID) {
		err = fmt.Errorf("%w: catalog %q", ErrNotFound, catalogID)
	}
	if err != nil {
		return nil, Invalid("catalogId", "%s", err.Error())
	}
	_, cred, err := s.platformFor(ctx, account)
	if err != nil {
		return nil, err
	}
	state, _ := s.store.GetBootstrapState(ctx, account.ID)

	account.Status = AccountStatusInstalling
	account.CatalogID = catalog.ID
	account.InstallError = ""
	account.InstallLog = ""
	account.UpdatedAt = s.now()
	if err := s.store.UpdateAccount(ctx, account); err != nil {
		return nil, err
	}

	accountID := account.ID
	s.runAsync(func() {
		bg := context.Background()
		logBuf := &tailBuffer{limit: maxLog}
		progress := &progressWriter{buf: logBuf, flush: func(text string) {
			// Keep the install page live: persist the log as it grows.
			if current, err := s.store.GetAccount(bg, accountID); err == nil {
				current.InstallLog = text
				_ = s.store.UpdateAccount(bg, current)
			}
		}, now: s.now}
		res, installErr := s.installer.Install(bg, InstallRequest{
			Catalog:    catalog,
			Credential: cred,
			WorkDir:    filepath.Join(s.dataDir, "accounts", accountID),
			State:      state,
			Stable:     s.stable,
			Log:        progress,
		})
		current, err := s.store.GetAccount(bg, accountID)
		if err != nil {
			s.log.Error("install finished for a missing account", "account", accountID, "err", err)
			return
		}
		current.InstallLog = logBuf.String()
		current.UpdatedAt = s.now()
		if res != nil && len(res.State) > 0 {
			if err := s.store.SaveBootstrapState(bg, accountID, res.State); err != nil {
				s.log.Error("save bootstrap state", "account", accountID, "err", err)
			}
		}
		if installErr != nil {
			s.log.Warn("catalog install failed", "account", accountID, "catalog", catalog.ID, "err", installErr)
			current.Status = AccountStatusFailed
			current.InstallError = installErr.Error()
			_ = s.store.UpdateAccount(bg, current)
			return
		}
		current.ProjectID = res.ProjectID
		if current.ProjectID == "" {
			current.ProjectID = catalog.Bootstrap.Project
		}
		current.BaseEnvironmentID = res.Environments[catalog.Bootstrap.BaseEnvironment]
		if current.BaseEnvironmentID == "" {
			current.BaseEnvironmentID = current.ProjectID + "-" + catalog.Bootstrap.BaseEnvironment
		}
		current.Status = s.statusAfterCredentials(current, catalog)
		_ = s.store.UpdateAccount(bg, current)
	})
	return account, nil
}

// RecoverInterruptedInstalls marks accounts left "installing" by a previous
// process as failed so their owners can retry. Call it once at startup.
func (s *Service) RecoverInterruptedInstalls(ctx context.Context) (int, error) {
	accounts, err := s.store.ListAccounts(ctx, AccountStatusInstalling)
	if err != nil {
		return 0, err
	}
	for i := range accounts {
		a := &accounts[i]
		a.Status = AccountStatusFailed
		a.InstallError = "the install was interrupted by a server restart; retry to continue"
		a.UpdatedAt = s.now()
		if err := s.store.UpdateAccount(ctx, a); err != nil {
			return 0, err
		}
	}
	return len(accounts), nil
}

func (s *Service) statusAfterCredentials(account *Account, catalog *Catalog) AccountStatus {
	for _, p := range catalog.Platforms {
		if account.Platforms[p.ID] == "" {
			return AccountStatusNeedsCredentials
		}
	}
	return AccountStatusReady
}

// PlatformRequirement is one credential type the account must provide.
type PlatformRequirement struct {
	Type       PlatformType `json:"type"`
	ResourceID string       `json:"resourceId,omitempty"`
	Satisfied  bool         `json:"satisfied"`
}

// PlatformRequirements lists the credentials the installed catalog needs, with
// instructions written for this account.
func (s *Service) PlatformRequirements(ctx context.Context, account *Account) ([]PlatformRequirement, error) {
	if account.CatalogID == "" {
		return []PlatformRequirement{}, nil
	}
	catalog, err := s.GetCatalog(account.CatalogID)
	if err != nil {
		return nil, err
	}
	_, cred, err := s.platformFor(ctx, account)
	if err != nil {
		return nil, err
	}
	vars := instructionVarsFor(account, cred)
	out := make([]PlatformRequirement, 0, len(catalog.Platforms))
	for _, p := range catalog.Platforms {
		id := account.Platforms[p.ID]
		out = append(out, PlatformRequirement{Type: renderPlatformType(p, vars), ResourceID: id, Satisfied: id != ""})
	}
	return out, nil
}

// ProvidePlatform stores a credential the customer typed in as a resource in their
// organization and pins it as the default on every environment.
func (s *Service) ProvidePlatform(ctx context.Context, account *Account, typeID, name string, payload map[string]any) (*Account, error) {
	if account.CatalogID == "" {
		return nil, fmt.Errorf("%w: install a catalog first", ErrConflict)
	}
	catalog, err := s.GetCatalog(account.CatalogID)
	if err != nil {
		return nil, err
	}
	var ptype *PlatformType
	for i := range catalog.Platforms {
		if catalog.Platforms[i].ID == typeID {
			ptype = &catalog.Platforms[i]
		}
	}
	if ptype == nil {
		return nil, Invalid("type", "the %s catalog does not use %q", catalog.Name, typeID)
	}
	payload = withSchemaDefaults(ptype.Schema, payload)
	if err := validateAgainstSchema(ptype.Schema, payload); err != nil {
		return nil, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = ptype.Label
	}
	platform, _, err := s.platformFor(ctx, account)
	if err != nil {
		return nil, err
	}
	resourceID, err := platform.CreateResource(ctx, ptype.ID, name, payload)
	if err != nil {
		return nil, fmt.Errorf("store credential: %w", err)
	}
	if err := platform.GrantResource(ctx, resourceID); err != nil {
		return nil, fmt.Errorf("share credential with environments: %w", err)
	}
	if account.Platforms == nil {
		account.Platforms = map[string]string{}
	}
	account.Platforms[ptype.ID] = resourceID
	if account.Status != AccountStatusInstalling && account.Status != AccountStatusFailed && account.ProjectID != "" {
		account.Status = s.statusAfterCredentials(account, catalog)
	}
	account.UpdatedAt = s.now()
	if err := s.store.UpdateAccount(ctx, account); err != nil {
		return nil, err
	}
	if err := s.pinDefaults(ctx, platform, account, []string{resourceID}); err != nil {
		s.log.Warn("pinning environment defaults", "account", account.ID, "err", err)
	}
	return account, nil
}

// pinDefaults sets resources as defaults on the base environment and every
// environment the account created.
func (s *Service) pinDefaults(ctx context.Context, platform Platform, account *Account, resourceIDs []string) error {
	envIDs := []string{}
	if account.BaseEnvironmentID != "" {
		envIDs = append(envIDs, account.BaseEnvironmentID)
	}
	envs, err := s.store.ListEnvironments(ctx, account.ID)
	if err != nil {
		return err
	}
	for _, e := range envs {
		envIDs = append(envIDs, e.EnvironmentID)
	}
	var errs []error
	for _, envID := range envIDs {
		for _, rid := range resourceIDs {
			if err := platform.SetEnvironmentDefault(ctx, envID, rid); err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", envID, err))
			}
		}
	}
	return errors.Join(errs...)
}

// validateAgainstSchema checks required keys and basic types of a flat JSON Schema.
func validateAgainstSchema(schema, payload map[string]any) error {
	props, _ := schema["properties"].(map[string]any)
	required, _ := schema["required"].([]any)
	for _, r := range required {
		key, _ := r.(string)
		v, ok := payload[key]
		if !ok || v == nil || v == "" {
			title := key
			if p, ok := props[key].(map[string]any); ok {
				if t, ok := p["title"].(string); ok && t != "" {
					title = t
				}
			}
			return Invalid(key, "%s is required", title)
		}
	}
	for key, v := range payload {
		prop, ok := props[key].(map[string]any)
		if !ok {
			if len(props) > 0 {
				return Invalid(key, "unexpected field")
			}
			continue
		}
		if v == nil || v == "" {
			continue
		}
		// YAML schemas and JSON payloads decode numbers to different Go types.
		allowed, titles := choices(prop)
		if len(allowed) > 0 && !slices.ContainsFunc(allowed, func(a any) bool { return fmt.Sprint(a) == fmt.Sprint(v) }) {
			return Invalid(key, "choose one of %s", strings.Join(titles, ", "))
		}
	}
	return nil
}

// choices lists the values a oneOf of consts or an enum allows, with their labels.
func choices(prop map[string]any) ([]any, []string) {
	var allowed []any
	var titles []string
	if oneOf, ok := prop["oneOf"].([]any); ok {
		for _, o := range oneOf {
			m, ok := o.(map[string]any)
			if !ok {
				return nil, nil
			}
			c, ok := m["const"]
			if !ok {
				return nil, nil
			}
			title, _ := m["title"].(string)
			if title == "" {
				title = fmt.Sprint(c)
			}
			allowed, titles = append(allowed, c), append(titles, title)
		}
		return allowed, titles
	}
	if enum, ok := prop["enum"].([]any); ok {
		for _, e := range enum {
			allowed, titles = append(allowed, e), append(titles, fmt.Sprint(e))
		}
	}
	return allowed, titles
}

// withSchemaDefaults fills the schema's defaults for fields the payload leaves out.
func withSchemaDefaults(schema, payload map[string]any) map[string]any {
	props, _ := schema["properties"].(map[string]any)
	out := make(map[string]any, len(payload)+len(props))
	maps.Copy(out, payload)
	for key, p := range props {
		prop, ok := p.(map[string]any)
		if !ok {
			continue
		}
		def, hasDefault := prop["default"]
		if v, present := out[key]; hasDefault && (!present || v == nil || v == "") {
			out[key] = def
		}
	}
	return out
}

// --- project -----------------------------------------------------------------

// ProjectView is the installed project with presets and the account's environments.
type ProjectView struct {
	Project      *Project      `json:"project"`
	Catalog      *Catalog      `json:"catalog"`
	Environments []Environment `json:"environments"`
}

// Project loads the account's project with presets read from the base environment.
func (s *Service) Project(ctx context.Context, account *Account) (*ProjectView, error) {
	if account.ProjectID == "" {
		return nil, fmt.Errorf("%w: no project yet; install a catalog first", ErrConflict)
	}
	catalog, err := s.GetCatalog(account.CatalogID)
	if err != nil {
		return nil, err
	}
	platform, cred, err := s.platformFor(ctx, account)
	if err != nil {
		return nil, err
	}
	project, _, err := s.blueprint(ctx, platform, account)
	if err != nil {
		return nil, err
	}
	envs, err := s.store.ListEnvironments(ctx, account.ID)
	if err != nil {
		return nil, err
	}
	if envs == nil {
		envs = []Environment{}
	}
	return &ProjectView{Project: project, Catalog: renderCatalog(catalog, instructionVarsFor(account, cred)), Environments: envs}, nil
}

// blueprint loads the project and enriches each component with the bundle presets
// and schema exposed by the base environment's instance of that component.
func (s *Service) blueprint(ctx context.Context, platform Platform, account *Account) (*Project, *PlatformEnvironment, error) {
	project, err := platform.GetProject(ctx, account.ProjectID)
	if err != nil {
		return nil, nil, fmt.Errorf("load project %q: %w", account.ProjectID, err)
	}
	base, err := platform.GetEnvironment(ctx, account.BaseEnvironmentID)
	if err != nil {
		return nil, nil, fmt.Errorf("load base environment %q: %w", account.BaseEnvironmentID, err)
	}
	bySlug := make(map[string]PlatformInstance, len(base.Instances))
	for _, pi := range base.Instances {
		bySlug[pi.ComponentSlug] = pi
	}
	for i := range project.Components {
		c := &project.Components[i]
		if pi, ok := bySlug[c.Slug]; ok {
			c.Bundle = Bundle{Name: pi.BundleName, Version: pi.Version, Presets: pi.Presets(), ParamsSchema: pi.ParamsSchema}
		}
	}
	project.Components = dependencyOrder(project.Components, project.Links)
	return project, base, nil
}

// dependencyOrder lists producers before the components that consume them, so
// the platform reads top-down: network, data, API, application.
func dependencyOrder(components []Component, links []Link) []Component {
	index := make(map[string]int, len(components))
	for i, c := range components {
		index[c.Slug] = i
	}
	indegree := make(map[string]int, len(components))
	consumers := make(map[string][]string, len(components))
	for _, l := range links {
		if _, ok := index[l.From]; !ok {
			continue
		}
		if _, ok := index[l.To]; !ok {
			continue
		}
		indegree[l.To]++
		consumers[l.From] = append(consumers[l.From], l.To)
	}
	var ready []string
	for _, c := range components {
		if indegree[c.Slug] == 0 {
			ready = append(ready, c.Slug)
		}
	}
	ordered := make([]Component, 0, len(components))
	seen := make(map[string]bool, len(components))
	for len(ready) > 0 {
		slug := ready[0]
		ready = ready[1:]
		if seen[slug] {
			continue
		}
		seen[slug] = true
		ordered = append(ordered, components[index[slug]])
		for _, next := range consumers[slug] {
			indegree[next]--
			if indegree[next] == 0 {
				ready = append(ready, next)
			}
		}
	}
	for _, c := range components {
		if !seen[c.Slug] {
			ordered = append(ordered, c)
		}
	}
	return ordered
}

// --- environments ------------------------------------------------------------

// CreateEnvironmentInput creates an environment of the account's project.
type CreateEnvironmentInput struct {
	Slug    string            `json:"slug"`
	Name    string            `json:"name"`
	Presets map[string]string `json:"presets"`
	// Runtime and Addons apply in tenant mode.
	Runtime string   `json:"runtime,omitempty"`
	Addons  []string `json:"addons,omitempty"`
}

// CreateEnvironment forks the base environment, applies one preset per component,
// pins the account's credentials and deploys. The deploy continues in the background.
func (s *Service) CreateEnvironment(ctx context.Context, user *User, account *Account, in CreateEnvironmentInput) (*Environment, error) {
	if account.Status != AccountStatusReady {
		return nil, fmt.Errorf("%w: the account is %s", ErrConflict, account.Status)
	}
	if s.tenant() {
		return s.createTenantEnvironment(ctx, user, account, in)
	}
	if !ValidSlug(in.Slug) {
		return nil, Invalid("slug", "use 1-20 lowercase letters or digits")
	}
	if strings.TrimSpace(in.Name) == "" {
		in.Name = in.Slug
	}
	existing, err := s.store.ListEnvironments(ctx, account.ID)
	if err != nil {
		return nil, err
	}
	for _, e := range existing {
		if e.Slug == in.Slug {
			return nil, fmt.Errorf("%w: environment %q already exists", ErrConflict, in.Slug)
		}
	}
	platform, _, err := s.platformFor(ctx, account)
	if err != nil {
		return nil, err
	}
	project, base, err := s.blueprint(ctx, platform, account)
	if err != nil {
		return nil, err
	}
	presets, err := resolvePresets(project, in.Presets)
	if err != nil {
		return nil, err
	}

	forked, err := platform.ForkEnvironment(ctx, ForkRequest{
		ParentEnvironmentID:     base.ID,
		ID:                      in.Slug,
		Name:                    in.Name,
		Description:             "Created by Rocketsled",
		CopyEnvironmentDefaults: true,
	})
	if err != nil {
		return nil, fmt.Errorf("create environment: %w", err)
	}
	if forked == nil {
		return nil, errors.New("create environment: platform returned nothing")
	}

	now := s.now()
	env := &Environment{
		ID:            s.newID(),
		AccountID:     account.ID,
		Slug:          in.Slug,
		Name:          in.Name,
		EnvironmentID: forked.ID,
		Presets:       chosenNames(presets),
		State:         EnvironmentStateDeploying,
		CreatedBy:     user.ID,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := s.store.CreateEnvironment(ctx, env); err != nil {
		return nil, err
	}
	run := &Run{
		ID: s.newID(), EnvironmentID: env.ID, Number: 1, Trigger: TriggerCreate,
		Status: RunStatusQueued, RequestedBy: user.ID, CreatedAt: now, UpdatedAt: now,
	}
	if err := s.store.CreateRun(ctx, run); err != nil {
		return nil, err
	}

	resourceIDs := make([]string, 0, len(account.Platforms))
	for _, rid := range account.Platforms {
		resourceIDs = append(resourceIDs, rid)
	}
	sort.Strings(resourceIDs)
	baseID, envID, envRecordID := base.ID, forked.ID, env.ID
	instances := forked.Instances
	runCopy := *run
	s.runAsync(func() {
		bg := context.Background()
		err := s.configureAndDeploy(bg, platform, baseID, envID, instances, presets, resourceIDs)
		s.finishRun(bg, &runCopy, envRecordID, err)
	})
	return env, nil
}

func chosenNames(presets map[string]Preset) map[string]string {
	out := make(map[string]string, len(presets))
	for slug, p := range presets {
		out[slug] = p.Name
	}
	return out
}

// configureAndDeploy copies each preset onto the forked instances, pins the
// account's credentials and deploys the environment.
func (s *Service) configureAndDeploy(ctx context.Context, platform Platform, baseID, envID string, instances []PlatformInstance, presets map[string]Preset, resourceIDs []string) error {
	for _, pi := range instances {
		preset, ok := presets[pi.ComponentSlug]
		if !ok {
			continue
		}
		overrides := MergeParams(map[string]any{}, preset.Params)
		sourceID := baseID + "-" + pi.ComponentSlug
		if err := platform.CopyInstance(ctx, sourceID, pi.ID, overrides); err != nil {
			return fmt.Errorf("configure %s: %w", pi.ComponentSlug, err)
		}
	}
	for _, rid := range resourceIDs {
		if err := platform.SetEnvironmentDefault(ctx, envID, rid); err != nil {
			return fmt.Errorf("pin credential: %w", err)
		}
	}
	if err := platform.DeployEnvironment(ctx, envID); err != nil {
		return fmt.Errorf("deploy: %w", err)
	}
	return nil
}

func (s *Service) finishRun(ctx context.Context, run *Run, envRecordID string, err error) {
	run.UpdatedAt = s.now()
	if err != nil {
		s.log.Warn("environment run failed", "environment", envRecordID, "run", run.Number, "err", err)
		run.Status = RunStatusFailed
		run.Message = err.Error()
		_ = s.store.UpdateRun(ctx, run)
		if env, getErr := s.store.GetEnvironment(ctx, envRecordID); getErr == nil {
			env.State = EnvironmentStateFailed
			env.UpdatedAt = s.now()
			_ = s.store.UpdateEnvironment(ctx, env)
		}
		return
	}
	run.Status = RunStatusRunning
	_ = s.store.UpdateRun(ctx, run)
}

// resolvePresets picks a preset for every component: the chosen one, else the
// bundle's first.
func resolvePresets(project *Project, chosen map[string]string) (map[string]Preset, error) {
	out := make(map[string]Preset, len(project.Components))
	for _, c := range project.Components {
		name := chosen[c.Slug]
		if name == "" {
			if len(c.Bundle.Presets) == 0 {
				continue
			}
			out[c.Slug] = c.Bundle.Presets[0]
			continue
		}
		found := false
		for _, p := range c.Bundle.Presets {
			if p.Name == name {
				out[c.Slug] = p
				found = true
				break
			}
		}
		if !found {
			return nil, Invalid("presets."+c.Slug, "%s has no preset %q", c.Bundle.Name, name)
		}
	}
	for slug := range chosen {
		if _, ok := out[slug]; !ok {
			known := false
			for _, c := range project.Components {
				if c.Slug == slug {
					known = true
				}
			}
			if !known {
				return nil, Invalid("presets."+slug, "the project has no component %q", slug)
			}
		}
	}
	return out, nil
}

// ListEnvironments lists the account's environments.
func (s *Service) ListEnvironments(ctx context.Context, account *Account) ([]Environment, error) {
	if s.tenant() {
		if err := s.syncTenant(ctx, account); err != nil {
			s.log.Warn("syncing apps from Massdriver", "account", account.ID, "err", err)
		}
	}
	envs, err := s.store.ListEnvironments(ctx, account.ID)
	if err != nil {
		return nil, err
	}
	if envs == nil {
		envs = []Environment{}
	}
	return envs, nil
}

// EnvironmentView is an environment with live platform state and recent runs.
type EnvironmentView struct {
	Environment Environment          `json:"environment"`
	Platform    *PlatformEnvironment `json:"platform,omitempty"`
	Runs        []Run                `json:"runs"`
	// Gone is true when a decommissioned environment has been fully removed.
	Gone bool `json:"gone"`
	// Release says where released images land, when the environment takes them.
	Release *ReleaseTarget `json:"release,omitempty"`
}

// EnvironmentStatus returns live state, refreshing run statuses from the
// platform. A decommissioning environment whose instances are all gone is deleted.
func (s *Service) EnvironmentStatus(ctx context.Context, account *Account, id string) (*EnvironmentView, error) {
	env, err := s.ownedEnvironment(ctx, account, id)
	if err != nil {
		return nil, err
	}
	platform, _, err := s.platformFor(ctx, account)
	if err != nil {
		return nil, err
	}
	view := &EnvironmentView{Environment: *env, Runs: []Run{}}
	if target, err := s.releaseTarget(account, env); err == nil {
		view.Release = target
	}
	live, err := platform.GetEnvironment(ctx, env.EnvironmentID)
	if err != nil {
		if env.State == EnvironmentStateDecommissioning && errors.Is(err, ErrNotFound) {
			view.Gone = true
			_ = s.store.DeleteEnvironment(ctx, env.ID)
			return view, nil
		}
		return nil, fmt.Errorf("load environment: %w", err)
	}
	if s.tenant() {
		onlyPicked(env, live)
	}
	highlight(live, s.highlightsFor(account))
	for i := range live.Instances {
		live.Instances[i].Params = nil
		if live.Instances[i].Resources == nil {
			live.Instances[i].Resources = []InstanceResource{}
		}
	}
	if live.Unfulfilled == nil {
		live.Unfulfilled = []UnfulfilledDependency{}
	}
	view.Platform = live

	runs, err := s.store.ListRuns(ctx, env.ID, 25)
	if err != nil {
		return nil, err
	}
	for i := range runs {
		if runs[i].Status != RunStatusRunning {
			continue
		}
		if st := runStatusFrom(live); st != RunStatusRunning {
			runs[i].Status = st
			runs[i].UpdatedAt = s.now()
			_ = s.store.UpdateRun(ctx, &runs[i])
		}
	}
	if runs != nil {
		view.Runs = runs
	}

	if state := deriveState(env.State, live); state != env.State {
		env.State = state
		env.UpdatedAt = s.now()
		if err := s.store.UpdateEnvironment(ctx, env); err != nil {
			return nil, err
		}
		view.Environment = *env
	}
	if env.State == EnvironmentStateDecommissioning && allDecommissioned(live) {
		if err := platform.DeleteEnvironment(ctx, live.ID); err == nil {
			_ = s.store.DeleteEnvironment(ctx, env.ID)
			view.Gone = true
		} else {
			s.log.Warn("environment delete deferred", "environment", live.ID, "err", err)
		}
	}
	return view, nil
}

func (s *Service) highlightsFor(account *Account) map[string][]string {
	if s.tenant() {
		return s.paas.Highlights
	}
	if c, err := s.GetCatalog(account.CatalogID); err == nil {
		return c.Highlights
	}
	return nil
}

func (s *Service) ownedEnvironment(ctx context.Context, account *Account, id string) (*Environment, error) {
	env, err := s.store.GetEnvironment(ctx, id)
	if err != nil && s.tenant() && errors.Is(err, ErrNotFound) {
		if syncErr := s.syncTenant(ctx, account); syncErr == nil {
			env, err = s.store.GetEnvironment(ctx, id)
		}
	}
	if err != nil {
		return nil, err
	}
	if env.AccountID != account.ID {
		return nil, fmt.Errorf("%w: environment %s", ErrNotFound, id)
	}
	return env, nil
}

// DeployEnvironment redeploys every instance in the environment.
func (s *Service) DeployEnvironment(ctx context.Context, user *User, account *Account, id string) (*Run, error) {
	env, err := s.ownedEnvironment(ctx, account, id)
	if err != nil {
		return nil, err
	}
	platform, _, err := s.platformFor(ctx, account)
	if err != nil {
		return nil, err
	}
	envID := env.EnvironmentID
	return s.startRun(ctx, user, env, TriggerDeploy, "", func(bg context.Context) error {
		if s.tenant() {
			return s.redeployTenant(bg, platform, env, user)
		}
		if err := platform.DeployEnvironment(bg, envID); err != nil {
			return fmt.Errorf("deploy: %w", err)
		}
		return nil
	})
}

// startRun records the next numbered run of env, marks env deploying and does
// work in the background, recording how it ended. Only one change at a time
// runs per environment; releases are the exception, since Massdriver queues
// deployments of one instance and a build shouldn't fail because another is
// still going.
func (s *Service) startRun(ctx context.Context, user *User, env *Environment, trigger RunTrigger, summary string, work func(context.Context) error) (*Run, error) {
	if env.State == EnvironmentStateDecommissioning {
		return nil, fmt.Errorf("%w: the environment is being destroyed", ErrConflict)
	}
	exclusive := trigger != TriggerRelease
	if exclusive && !s.claim(env.ID) {
		return nil, fmt.Errorf("%w: %s is still deploying; try again when it finishes", ErrConflict, env.Name)
	}
	started := false
	defer func() {
		if exclusive && !started {
			s.release(env.ID)
		}
	}()
	previous, err := s.store.ListRuns(ctx, env.ID, 1)
	if err != nil {
		return nil, err
	}
	now := s.now()
	run := &Run{ID: s.newID(), EnvironmentID: env.ID, Number: 1, Trigger: trigger, Status: RunStatusQueued, Message: summary, RequestedBy: user.ID, CreatedAt: now, UpdatedAt: now}
	if len(previous) > 0 {
		run.Number = previous[0].Number + 1
	}
	if err := s.store.CreateRun(ctx, run); err != nil {
		return nil, err
	}
	env.State = EnvironmentStateDeploying
	env.UpdatedAt = now
	if err := s.store.UpdateEnvironment(ctx, env); err != nil {
		return nil, err
	}
	envRecordID, runCopy := env.ID, *run
	started = true
	s.runAsync(func() {
		if exclusive {
			defer s.release(envRecordID)
		}
		bg := context.Background()
		s.finishRun(bg, &runCopy, envRecordID, work(bg))
	})
	return run, nil
}

// claim marks an environment as having work in flight in this process, so two
// changes never deploy the same instances at once.
func (s *Service) claim(envID string) bool {
	_, taken := s.inFlight.LoadOrStore(envID, struct{}{})
	return !taken
}

func (s *Service) release(envID string) { s.inFlight.Delete(envID) }

// DestroyEnvironment decommissions the environment. The record is removed once
// every instance is gone (see EnvironmentStatus).
func (s *Service) DestroyEnvironment(ctx context.Context, account *Account, id string) (*Environment, error) {
	env, err := s.ownedEnvironment(ctx, account, id)
	if err != nil {
		return nil, err
	}
	if env.State == EnvironmentStateDecommissioning {
		return env, nil
	}
	platform, _, err := s.platformFor(ctx, account)
	if err != nil {
		return nil, err
	}
	if err := platform.DecommissionEnvironment(ctx, env.EnvironmentID); err != nil {
		return nil, fmt.Errorf("decommission: %w", err)
	}
	env.State = EnvironmentStateDecommissioning
	env.UpdatedAt = s.now()
	if err := s.store.UpdateEnvironment(ctx, env); err != nil {
		return nil, err
	}
	return env, nil
}

// --- status derivation -------------------------------------------------------

func runStatusFrom(env *PlatformEnvironment) RunStatus {
	if len(env.Instances) == 0 {
		return RunStatusRunning
	}
	for _, pi := range env.Instances {
		if strings.EqualFold(pi.Status, "FAILED") {
			return RunStatusFailed
		}
	}
	for _, pi := range env.Instances {
		if !strings.EqualFold(pi.Status, "PROVISIONED") {
			return RunStatusRunning
		}
	}
	return RunStatusSucceeded
}

func deriveState(current EnvironmentState, env *PlatformEnvironment) EnvironmentState {
	if current == EnvironmentStateDecommissioning || len(env.Instances) == 0 {
		return current
	}
	switch runStatusFrom(env) {
	case RunStatusFailed:
		return EnvironmentStateFailed
	case RunStatusSucceeded:
		return EnvironmentStateRunning
	case RunStatusQueued, RunStatusRunning:
		return EnvironmentStateDeploying
	}
	return EnvironmentStateDeploying
}

func allDecommissioned(env *PlatformEnvironment) bool {
	for _, pi := range env.Instances {
		switch strings.ToUpper(pi.Status) {
		case "DECOMMISSIONED", "INITIALIZED":
		default:
			return false
		}
	}
	return true
}

// maxLog caps the install log stored per account.
const maxLog = 128 << 10

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

// progressWriter appends to a tailBuffer and flushes its contents to a callback
// at most every couple of seconds.
type progressWriter struct {
	buf   *tailBuffer
	flush func(string)
	now   func() time.Time
	last  time.Time
}

func (p *progressWriter) Write(b []byte) (int, error) {
	n, err := p.buf.Write(b)
	if t := p.now(); t.Sub(p.last) >= 2*time.Second {
		p.last = t
		p.flush(p.buf.String())
	}
	return n, err
}

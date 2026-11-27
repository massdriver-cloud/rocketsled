package domain

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
)

// AccountModeTenant serves forks of templates that already exist in one
// organization: no install, no customer credential.
const AccountModeTenant AccountMode = "tenant"

// PaaS is what a tenant-mode server offers its customers.
type PaaS struct {
	Runtimes   []Runtime
	Pinned     map[string]map[string]any
	Addons     []Addon
	Plans      map[string]map[string]Plan
	Highlights map[string][]string
	Limits     Limits
}

// Limits keep an open sign-up from running up the operator's bill.
type Limits struct {
	AppsPerPerson int           `json:"appsPerPerson,omitempty"`
	TotalApps     int           `json:"totalApps,omitempty"`
	Presets       []string      `json:"-"`
	Addons        []string      `json:"-"`
	TTL           time.Duration `json:"-"`
	// TTLHours is TTL for the UI.
	TTLHours float64 `json:"ttlHours,omitempty"`
}

// Runtime is a template environment customers fork.
type Runtime struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Summary      string `json:"summary,omitempty"`
	Project      string `json:"project"`
	Template     string `json:"template"`
	AppComponent string `json:"appComponent,omitempty"`
	ImageParam   string `json:"imageParam,omitempty"`
	// Pinned applies over PaaS.Pinned for this runtime's apps.
	Pinned map[string]map[string]any `json:"-"`
}

// Addon is a component of a template that is deployed only when picked.
type Addon struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Summary string `json:"summary"`
}

// Plan labels a preset for customers.
type Plan struct {
	Price   string `json:"price,omitempty"`
	Summary string `json:"summary,omitempty"`
}

// Offer is everything a customer can pick from.
type Offer struct {
	Runtimes []RuntimeOffer `json:"runtimes"`
	Limits   Limits         `json:"limits"`
}

// RuntimeOffer is a runtime with the components every app of it gets and the
// add-ons it may pick.
type RuntimeOffer struct {
	ID         string             `json:"id"`
	Name       string             `json:"name"`
	Summary    string             `json:"summary,omitempty"`
	Project    string             `json:"project"`
	Components []OfferedComponent `json:"components"`
	Addons     []OfferedComponent `json:"addons"`
	links      []Link
}

// OfferedComponent is a template component with its presets.
type OfferedComponent struct {
	Slug    string          `json:"slug"`
	Name    string          `json:"name"`
	Summary string          `json:"summary,omitempty"`
	Bundle  string          `json:"bundle"`
	Presets []OfferedPreset `json:"presets"`
}

// OfferedPreset is a bundle preset with its plan label.
type OfferedPreset struct {
	Name    string         `json:"name"`
	Price   string         `json:"price,omitempty"`
	Summary string         `json:"summary,omitempty"`
	Params  map[string]any `json:"-"`
}

const offerTTL = 10 * time.Minute

type offerCache struct {
	mu    sync.Mutex
	offer *Offer
	at    time.Time
}

func (s *Service) tenant() bool { return s.mode == AccountModeTenant }

// Offer lists the runtimes and add-ons, read from each runtime's template. Every
// customer sees the same offer, so it is cached.
func (s *Service) Offer(ctx context.Context) (*Offer, error) {
	if !s.tenant() {
		return nil, fmt.Errorf("%w: only tenant mode has an offer", ErrNotFound)
	}
	s.offers.mu.Lock()
	defer s.offers.mu.Unlock()
	if s.offers.offer != nil && s.now().Sub(s.offers.at) < offerTTL {
		return s.offers.offer, nil
	}
	platform := s.platforms(s.shared)
	limits := s.paas.Limits
	limits.TTLHours = limits.TTL.Hours()
	offer := &Offer{Runtimes: []RuntimeOffer{}, Limits: limits}
	for _, rt := range s.paas.Runtimes {
		ro, err := s.runtimeOffer(ctx, platform, rt)
		if err != nil {
			return nil, err
		}
		offer.Runtimes = append(offer.Runtimes, *ro)
	}
	s.offers.offer, s.offers.at = offer, s.now()
	return offer, nil
}

func templateID(rt Runtime) string { return rt.Project + "-" + rt.Template }

// runtimeOffer sorts a template's components: deployed in the template means
// shared by every app (an image repository, say) and is not offered; a listed
// add-on is optional; everything else every app gets.
func (s *Service) runtimeOffer(ctx context.Context, platform Platform, rt Runtime) (*RuntimeOffer, error) {
	project, err := platform.GetProject(ctx, rt.Project)
	if err != nil {
		return nil, fmt.Errorf("load project %q: %w", rt.Project, err)
	}
	tmpl, err := platform.GetEnvironment(ctx, templateID(rt))
	if err != nil {
		return nil, fmt.Errorf("load template %q: %w", templateID(rt), err)
	}
	addons := map[string]Addon{}
	for _, a := range s.paas.Addons {
		addons[a.ID] = a
	}
	instances := map[string]PlatformInstance{}
	for _, pi := range tmpl.Instances {
		instances[pi.ComponentSlug] = pi
	}
	ro := &RuntimeOffer{ID: rt.ID, Name: rt.Name, Summary: rt.Summary, Project: rt.Project, Components: []OfferedComponent{}, Addons: []OfferedComponent{}, links: project.Links}
	for _, c := range dependencyOrder(project.Components, project.Links) {
		pi, ok := instances[c.Slug]
		if !ok || strings.EqualFold(pi.Status, "PROVISIONED") {
			continue
		}
		_, isAddon := addons[c.Slug]
		if isAddon && s.paas.Limits.Addons != nil && !slices.Contains(s.paas.Limits.Addons, c.Slug) {
			continue
		}
		oc := OfferedComponent{Slug: c.Slug, Name: c.Name, Bundle: pi.BundleName, Presets: []OfferedPreset{}}
		for _, p := range pi.Presets() {
			if len(s.paas.Limits.Presets) > 0 && !slices.Contains(s.paas.Limits.Presets, p.Name) {
				continue
			}
			plan := s.paas.Plans[c.Slug][p.Name]
			oc.Presets = append(oc.Presets, OfferedPreset{Name: p.Name, Price: plan.Price, Summary: plan.Summary, Params: p.Params})
		}
		if a, ok := addons[c.Slug]; ok {
			oc.Name, oc.Summary = a.Title, a.Summary
			ro.Addons = append(ro.Addons, oc)
			continue
		}
		ro.Components = append(ro.Components, oc)
	}
	// Add-ons read in the order the config lists them.
	order := make(map[string]int, len(s.paas.Addons))
	for i, a := range s.paas.Addons {
		order[a.ID] = i
	}
	slices.SortStableFunc(ro.Addons, func(a, b OfferedComponent) int { return order[a.Slug] - order[b.Slug] })
	return ro, nil
}

func (o *Offer) runtime(id string) (*RuntimeOffer, bool) {
	for i := range o.Runtimes {
		if o.Runtimes[i].ID == id {
			return &o.Runtimes[i], true
		}
	}
	return nil, false
}

func (s *Service) runtimeConfig(id string) Runtime {
	for _, rt := range s.paas.Runtimes {
		if rt.ID == id {
			return rt
		}
	}
	return Runtime{}
}

// createTenantEnvironment forks the runtime's template, then deploys only the
// runtime's own components and the add-ons picked.
func (s *Service) createTenantEnvironment(ctx context.Context, user *User, account *Account, in CreateEnvironmentInput) (*Environment, error) {
	if !ValidSlug(in.Slug) {
		return nil, Invalid("slug", "use 1-20 lowercase letters or digits")
	}
	if in.Slug == "template" {
		return nil, Invalid("slug", "that name is reserved")
	}
	offer, err := s.Offer(ctx)
	if err != nil {
		return nil, err
	}
	ro, ok := offer.runtime(in.Runtime)
	if !ok {
		return nil, Invalid("runtime", "pick a runtime")
	}
	rt := s.runtimeConfig(in.Runtime)
	picked := append([]OfferedComponent(nil), ro.Components...)
	addons := []string{}
	for _, id := range in.Addons {
		i := slices.IndexFunc(ro.Addons, func(c OfferedComponent) bool { return c.Slug == id })
		if i < 0 {
			return nil, Invalid("addons", "%s has no add-on %q", ro.Name, id)
		}
		if !slices.Contains(addons, id) {
			picked = append(picked, ro.Addons[i])
			addons = append(addons, id)
		}
	}
	presets := map[string]OfferedPreset{}
	for _, c := range picked {
		if len(c.Presets) == 0 {
			continue
		}
		name := in.Presets[c.Slug]
		if name == "" {
			presets[c.Slug] = c.Presets[0]
			continue
		}
		i := slices.IndexFunc(c.Presets, func(p OfferedPreset) bool { return p.Name == name })
		if i < 0 {
			return nil, Invalid("presets."+c.Slug, "%s has no plan %q", c.Name, name)
		}
		presets[c.Slug] = c.Presets[i]
	}
	if strings.TrimSpace(in.Name) == "" {
		in.Name = in.Slug
	}

	platform := s.platforms(s.shared)
	if err := s.checkLimits(ctx, platform, account); err != nil {
		return nil, err
	}
	envID := rt.Project + "-" + in.Slug
	if _, err := platform.GetEnvironment(ctx, envID); err == nil {
		return nil, Invalid("slug", "%s is taken; try another name", in.Slug)
	}
	forked, err := platform.ForkEnvironment(ctx, ForkRequest{
		ParentEnvironmentID:     templateID(rt),
		ID:                      in.Slug,
		Name:                    in.Name,
		Description:             ownerMarker(user.Login, account.OwnerUserID, pickedPresets(picked, presets)),
		CopyEnvironmentDefaults: true,
	})
	if err != nil {
		return nil, fmt.Errorf("create %s: %w", in.Slug, err)
	}
	if forked == nil {
		return nil, errors.New("create environment: platform returned nothing")
	}

	now := s.now()
	names := map[string]string{}
	for _, c := range picked {
		names[c.Slug] = presets[c.Slug].Name
	}
	env := &Environment{
		ID: forked.ID, AccountID: account.ID, Slug: in.Slug, Name: in.Name, EnvironmentID: forked.ID,
		Presets: names, Runtime: rt.ID, Addons: addons, State: EnvironmentStateDeploying,
		CreatedBy: user.ID, CreatedAt: now, UpdatedAt: now,
	}
	if err := s.store.CreateEnvironment(ctx, env); err != nil {
		return nil, err
	}
	run := &Run{ID: s.newID(), EnvironmentID: env.ID, Number: 1, Trigger: TriggerCreate, Status: RunStatusQueued, RequestedBy: user.ID, CreatedAt: now, UpdatedAt: now}
	if err := s.store.CreateRun(ctx, run); err != nil {
		return nil, err
	}

	vars := map[string]string{"environment_id": forked.ID, "app": in.Slug, "runtime": rt.ID}
	params := map[string]map[string]any{}
	for _, c := range picked {
		pinned := MergeParams(renderPinned(s.paas.Pinned[c.Slug], vars), renderPinned(rt.Pinned[c.Slug], vars))
		params[c.Slug] = MergeParams(MergeParams(map[string]any{}, presets[c.Slug].Params), pinned)
	}
	links, envRecordID, runCopy := ro.links, env.ID, *run
	s.runAsync(func() {
		bg := context.Background()
		err := s.deployInLayers(bg, platform, forked.ID, params, links, "Created by "+user.Login)
		s.finishRun(bg, &runCopy, envRecordID, err)
	})
	return env, nil
}

// redeployTenant redeploys the components the customer picked with the params
// they already have; nothing else in the fork is touched.
func (s *Service) redeployTenant(ctx context.Context, platform Platform, env *Environment, user *User) error {
	live, err := platform.GetEnvironment(ctx, env.EnvironmentID)
	if err != nil {
		return err
	}
	rt := s.runtimeConfig(env.Runtime)
	vars := map[string]string{"environment_id": env.EnvironmentID, "app": env.Slug, "runtime": rt.ID}
	params := map[string]map[string]any{}
	for _, pi := range live.Instances {
		if _, ok := env.Presets[pi.ComponentSlug]; ok {
			// Pinned params win again, so a redeploy picks up what the operator changed.
			pinned := MergeParams(renderPinned(s.paas.Pinned[pi.ComponentSlug], vars), renderPinned(rt.Pinned[pi.ComponentSlug], vars))
			params[pi.ComponentSlug] = MergeParams(pi.Params, pinned)
		}
	}
	var links []Link
	if offer, err := s.Offer(ctx); err == nil {
		if ro, ok := offer.runtime(env.Runtime); ok {
			links = ro.links
		}
	}
	return s.deployInLayers(ctx, platform, env.EnvironmentID, params, links, "Redeployed by "+user.Login)
}

// deployPollEvery and deployTimeout bound how long a layer may take.
var (
	deployPollEvery = 10 * time.Second
	deployTimeout   = 50 * time.Minute
)

// deployInLayers deploys producers before the components linked to them,
// waiting for each layer to provision, so a consumer reads a real connection.
func (s *Service) deployInLayers(ctx context.Context, platform Platform, envID string, params map[string]map[string]any, links []Link, message string) error {
	return s.deployLayers(ctx, platform, envID, params, links, message, false)
}

// deployLayers with resuming set carries on past a component Massdriver won't
// deploy again, which is one a previous server already started.
func (s *Service) deployLayers(ctx context.Context, platform Platform, envID string, params map[string]map[string]any, links []Link, message string, resuming bool) error {
	remaining := map[string]bool{}
	for slug := range params {
		remaining[slug] = true
	}
	for len(remaining) > 0 {
		var layer []string
		for slug := range remaining {
			ready := true
			for _, l := range links {
				if l.To == slug && remaining[l.From] {
					ready = false
				}
			}
			if ready {
				layer = append(layer, slug)
			}
		}
		if len(layer) == 0 {
			for slug := range remaining {
				layer = append(layer, slug)
			}
		}
		slices.Sort(layer)
		for _, slug := range layer {
			if err := platform.DeployInstance(ctx, envID+"-"+slug, params[slug], message); err != nil {
				if !resuming {
					return fmt.Errorf("deploy %s: %w", slug, err)
				}
				s.log.Info("resume: leaving a deploy already under way", "instance", envID+"-"+slug, "err", err)
			}
		}
		if err := s.waitProvisioned(ctx, platform, envID, layer); err != nil {
			return err
		}
		for _, slug := range layer {
			delete(remaining, slug)
		}
	}
	return nil
}

func (s *Service) waitProvisioned(ctx context.Context, platform Platform, envID string, slugs []string) error {
	deadline := time.Now().Add(deployTimeout)
	for {
		live, err := platform.GetEnvironment(ctx, envID)
		if err == nil {
			done := true
			for _, pi := range live.Instances {
				if !slices.Contains(slugs, pi.ComponentSlug) {
					continue
				}
				switch strings.ToUpper(pi.Status) {
				case "PROVISIONED":
				case "FAILED":
					return fmt.Errorf("%s failed to deploy", pi.ComponentName)
				default:
					done = false
				}
			}
			if done {
				return nil
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("still deploying %s after %s", strings.Join(slugs, ", "), deployTimeout)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(deployPollEvery):
		}
	}
}

func renderPinned(pinned map[string]any, vars map[string]string) map[string]any {
	out := make(map[string]any, len(pinned))
	for k, v := range pinned {
		out[k] = renderValue(v, vars)
	}
	return out
}

func renderValue(v any, vars map[string]string) any {
	switch t := v.(type) {
	case string:
		for name, value := range vars {
			t = strings.ReplaceAll(t, "{{"+name+"}}", value)
		}
		return t
	case map[string]any:
		return renderPinned(t, vars)
	case []any:
		out := make([]any, len(t))
		for i, item := range t {
			out[i] = renderValue(item, vars)
		}
		return out
	}
	return v
}

var loginPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,38}$`)

// SignInWithPassword signs in anyone who knows the shared password, as the name they type.
func (s *Service) SignInWithPassword(ctx context.Context, login, password string) (*User, error) {
	if s.password == "" {
		return nil, fmt.Errorf("%w: password sign-in is not configured", ErrNotFound)
	}
	login = strings.TrimSpace(login)
	if !loginPattern.MatchString(login) {
		return nil, Invalid("login", "use letters, digits, dots, dashes or underscores")
	}
	if subtle.ConstantTimeCompare([]byte(password), []byte(s.password)) != 1 {
		return nil, Invalid("password", "that password is not right")
	}
	id := strings.ToLower(login)
	user := &User{ID: string(ProviderPassword) + ":" + id, Provider: ProviderPassword, ProviderUserID: id, Login: login, Name: login, CreatedAt: s.now()}
	if existing, err := s.store.GetUser(ctx, user.ID); err == nil {
		user.CreatedAt = existing.CreatedAt
	}
	if err := s.store.UpsertUser(ctx, user); err != nil {
		return nil, err
	}
	return user, nil
}

// onlyPicked drops the template components a tenant environment never deployed.
func onlyPicked(env *Environment, live *PlatformEnvironment) {
	kept := live.Instances[:0]
	for _, pi := range live.Instances {
		if _, ok := env.Presets[pi.ComponentSlug]; ok {
			kept = append(kept, pi)
		}
	}
	live.Instances = kept
	unfulfilled := live.Unfulfilled[:0]
	for _, u := range live.Unfulfilled {
		if slices.ContainsFunc(kept, func(pi PlatformInstance) bool { return pi.ID == u.InstanceID }) {
			unfulfilled = append(unfulfilled, u)
		}
	}
	live.Unfulfilled = unfulfilled
}

// The owner is written into each environment's description, so who owns which
// app survives the server losing its database.
var ownerPattern = regexp.MustCompile(`\[owner ([^\]\s]+)\]$`)

func ownerMarker(login, userID string, picks map[string]string) string {
	parts := make([]string, 0, len(picks))
	for slug, preset := range picks {
		parts = append(parts, slug+"="+preset)
	}
	slices.Sort(parts)
	return fmt.Sprintf("Made in Rocketsled for %s [deploy %s] [owner %s]", login, strings.Join(parts, ","), userID)
}

var picksPattern = regexp.MustCompile(`\[deploy ([^\]]*)\]`)

// picksOf reads which components an app deploys, and with which preset.
func picksOf(description string) map[string]string {
	m := picksPattern.FindStringSubmatch(description)
	if m == nil {
		return nil
	}
	out := map[string]string{}
	for part := range strings.SplitSeq(m[1], ",") {
		if slug, preset, ok := strings.Cut(part, "="); ok && slug != "" {
			out[slug] = preset
		}
	}
	return out
}

func pickedPresets(picked []OfferedComponent, presets map[string]OfferedPreset) map[string]string {
	out := make(map[string]string, len(picked))
	for _, c := range picked {
		out[c.Slug] = presets[c.Slug].Name
	}
	return out
}

// ResumeInterrupted finishes creating apps whose server stopped partway, a
// Spot interruption or a redeploy, say: every picked component still waiting
// to be deployed is deployed. Call it once at startup.
func (s *Service) ResumeInterrupted(ctx context.Context) {
	if !s.tenant() {
		return
	}
	platform := s.platforms(s.shared)
	all, runtimeOf, err := s.madeHere(ctx, platform)
	if err != nil {
		s.log.Warn("resume: listing apps", "err", err)
		return
	}
	offer, err := s.Offer(ctx)
	if err != nil {
		s.log.Warn("resume: reading the offer", "err", err)
		return
	}
	for _, e := range all {
		picks := picksOf(e.Description)
		if len(picks) == 0 || s.now().Sub(e.CreatedAt) > 6*time.Hour {
			continue
		}
		rt := runtimeOf[e.ID]
		ro, ok := offer.runtime(rt.ID)
		if !ok {
			continue
		}
		live, err := platform.GetEnvironment(ctx, e.ID)
		if err != nil {
			continue
		}
		waiting := map[string]bool{}
		for _, pi := range live.Instances {
			if _, ok := picks[pi.ComponentSlug]; ok && strings.EqualFold(pi.Status, "INITIALIZED") {
				waiting[pi.ComponentSlug] = true
			}
		}
		if len(waiting) == 0 {
			continue
		}
		vars := map[string]string{"environment_id": e.ID, "app": strings.TrimPrefix(e.ID, rt.Project+"-"), "runtime": rt.ID}
		params := map[string]map[string]any{}
		for _, c := range append(append([]OfferedComponent{}, ro.Components...), ro.Addons...) {
			if !waiting[c.Slug] {
				continue
			}
			preset := OfferedPreset{Params: map[string]any{}}
			for _, p := range c.Presets {
				if p.Name == picks[c.Slug] || preset.Name == "" {
					preset = p
				}
			}
			pinned := MergeParams(renderPinned(s.paas.Pinned[c.Slug], vars), renderPinned(rt.Pinned[c.Slug], vars))
			params[c.Slug] = MergeParams(MergeParams(map[string]any{}, preset.Params), pinned)
		}
		envID, links := e.ID, ro.links
		s.log.Info("resume: finishing an interrupted app", "environment", envID, "components", len(params))
		s.runAsync(func() {
			if err := s.deployLayers(context.Background(), platform, envID, params, links, "Resumed after a restart", true); err != nil {
				s.log.Warn("resume", "environment", envID, "err", err)
			}
		})
	}
}

func ownerOf(description string) string {
	if m := ownerPattern.FindStringSubmatch(description); m != nil {
		return m[1]
	}
	return ""
}

// madeHere lists every environment this server made, across its runtimes.
func (s *Service) madeHere(ctx context.Context, platform Platform) ([]EnvironmentSummary, map[string]Runtime, error) {
	var out []EnvironmentSummary
	runtimeOf := map[string]Runtime{}
	seen := map[string]bool{}
	for _, rt := range s.paas.Runtimes {
		if seen[rt.Project] {
			continue
		}
		seen[rt.Project] = true
		project, err := platform.GetProject(ctx, rt.Project)
		if err != nil {
			return nil, nil, fmt.Errorf("list %s: %w", rt.Project, err)
		}
		for _, e := range project.Environments {
			if ownerOf(e.Description) != "" {
				out = append(out, e)
				runtimeOf[e.ID] = rt
			}
		}
	}
	return out, runtimeOf, nil
}

func (s *Service) checkLimits(ctx context.Context, platform Platform, account *Account) error {
	l := s.paas.Limits
	if l.AppsPerPerson == 0 && l.TotalApps == 0 {
		return nil
	}
	all, _, err := s.madeHere(ctx, platform)
	if err != nil {
		return err
	}
	mine := 0
	for _, e := range all {
		if ownerOf(e.Description) == account.OwnerUserID {
			mine++
		}
	}
	if l.AppsPerPerson > 0 && mine >= l.AppsPerPerson {
		return fmt.Errorf("%w: you can have %d at a time; delete one first", ErrConflict, l.AppsPerPerson)
	}
	if l.TotalApps > 0 && len(all) >= l.TotalApps {
		return fmt.Errorf("%w: this demo is full right now; try again later", ErrConflict)
	}
	return nil
}

// syncTenant makes the account's list match Massdriver: it adopts the owner's
// environments the store doesn't know (after a restart, say) and forgets ones
// that are gone.
func (s *Service) syncTenant(ctx context.Context, account *Account) error {
	platform := s.platforms(s.shared)
	all, runtimeOf, err := s.madeHere(ctx, platform)
	if err != nil {
		return err
	}
	known, err := s.store.ListEnvironments(ctx, account.ID)
	if err != nil {
		return err
	}
	live := map[string]EnvironmentSummary{}
	for _, e := range all {
		if ownerOf(e.Description) == account.OwnerUserID {
			live[e.ID] = e
		}
	}
	for _, e := range known {
		if _, ok := live[e.EnvironmentID]; !ok && s.now().Sub(e.CreatedAt) > time.Minute {
			_ = s.store.DeleteEnvironment(ctx, e.ID)
		}
		delete(live, e.EnvironmentID)
	}
	addons := map[string]bool{}
	for _, a := range s.paas.Addons {
		addons[a.ID] = true
	}
	for id, summary := range live {
		pe, err := platform.GetEnvironment(ctx, id)
		if err != nil {
			continue
		}
		rt := runtimeOf[id]
		env := &Environment{
			ID: id, AccountID: account.ID, Slug: strings.TrimPrefix(id, rt.Project+"-"), Name: summary.Name, EnvironmentID: id,
			Presets: map[string]string{}, Runtime: rt.ID, State: EnvironmentStateDeploying,
			CreatedBy: account.OwnerUserID, CreatedAt: summary.CreatedAt, UpdatedAt: s.now(),
		}
		tmpl := map[string]bool{}
		if offer, err := s.Offer(ctx); err == nil {
			if ro, ok := offer.runtime(rt.ID); ok {
				for _, c := range append(append([]OfferedComponent{}, ro.Components...), ro.Addons...) {
					tmpl[c.Slug] = true
				}
				for _, c := range ro.Components {
					env.Presets[c.Slug] = ""
				}
			}
		}
		for _, pi := range pe.Instances {
			if !tmpl[pi.ComponentSlug] || strings.EqualFold(pi.Status, "INITIALIZED") {
				continue
			}
			name, _ := pi.Params[PresetNameKey].(string)
			env.Presets[pi.ComponentSlug] = name
			if addons[pi.ComponentSlug] {
				env.Addons = append(env.Addons, pi.ComponentSlug)
			}
		}
		if err := s.store.CreateEnvironment(ctx, env); err != nil {
			s.log.Warn("adopting environment", "environment", id, "err", err)
		}
	}
	return nil
}

// ReapExpired removes apps older than the TTL and deletes the ones whose
// resources are all gone. Call it periodically.
func (s *Service) ReapExpired(ctx context.Context) {
	if !s.tenant() || s.paas.Limits.TTL == 0 {
		return
	}
	platform := s.platforms(s.shared)
	all, _, err := s.madeHere(ctx, platform)
	if err != nil {
		s.log.Warn("reaper: listing apps", "err", err)
		return
	}
	for _, e := range all {
		age := s.now().Sub(e.CreatedAt)
		expired := age >= s.paas.Limits.TTL
		// A customer's delete only finishes while they watch the page, so
		// anything left with nothing deployed is cleaned up here too.
		if !expired && age < 30*time.Minute {
			continue
		}
		pe, err := platform.GetEnvironment(ctx, e.ID)
		if err != nil {
			continue
		}
		if allDecommissioned(pe) {
			if err := platform.DeleteEnvironment(ctx, e.ID); err != nil {
				s.log.Warn("reaper: delete", "environment", e.ID, "err", err)
			} else {
				s.log.Info("reaper: deleted expired app", "environment", e.ID)
			}
			continue
		}
		if !expired {
			continue
		}
		s.reaped.Lock()
		last, tried := s.reaped.at[e.ID]
		if tried && s.now().Sub(last) < time.Hour {
			s.reaped.Unlock()
			continue
		}
		s.reaped.at[e.ID] = s.now()
		s.reaped.Unlock()
		if err := platform.DecommissionEnvironment(ctx, e.ID); err != nil {
			s.log.Warn("reaper: decommission", "environment", e.ID, "err", err)
		} else {
			s.log.Info("reaper: removing expired app", "environment", e.ID, "age", s.now().Sub(e.CreatedAt).Round(time.Minute))
		}
	}
}

type reapLog struct {
	sync.Mutex
	at map[string]time.Time
}

// SignUp gives anyone a fresh identity under the name they type. The identity
// lives in the signed session, so it outlasts the server's database.
func (s *Service) SignUp(ctx context.Context, name, inviteCode string) (*User, error) {
	if !s.open {
		return nil, fmt.Errorf("%w: sign-up is closed", ErrNotFound)
	}
	name = strings.TrimSpace(name)
	if !loginPattern.MatchString(name) {
		return nil, Invalid("login", "use letters, digits, dots, dashes or underscores, up to 39")
	}
	if s.inviteCode != "" && subtle.ConstantTimeCompare([]byte(inviteCode), []byte(s.inviteCode)) != 1 {
		return nil, Invalid("inviteCode", "that invite code is not right")
	}
	id := strings.ReplaceAll(s.newID(), "-", "")
	id = id[len(id)-12:]
	user := &User{ID: string(ProviderOpen) + ":" + id + ":" + name, Provider: ProviderOpen, ProviderUserID: id, Login: name, Name: name, CreatedAt: s.now()}
	if err := s.store.UpsertUser(ctx, user); err != nil {
		return nil, err
	}
	return user, nil
}

// sessionUser rebuilds a user from their signed session's ID when the
// database has forgotten them, for the providers whose IDs carry the name.
func (s *Service) sessionUser(id string) (*User, bool) {
	if s.open {
		if u, ok := openUser(id); ok {
			return u, true
		}
	}
	if s.password != "" {
		if name, ok := strings.CutPrefix(id, string(ProviderPassword)+":"); ok && loginPattern.MatchString(name) {
			return &User{ID: id, Provider: ProviderPassword, ProviderUserID: name, Login: name, Name: name}, true
		}
	}
	return nil, false
}

// openUser rebuilds a signed-up user from their session's ID.
func openUser(id string) (*User, bool) {
	parts := strings.SplitN(id, ":", 3)
	if len(parts) != 3 || parts[0] != string(ProviderOpen) || !loginPattern.MatchString(parts[2]) {
		return nil, false
	}
	return &User{ID: id, Provider: ProviderOpen, ProviderUserID: parts[1], Login: parts[2], Name: parts[2]}, true
}

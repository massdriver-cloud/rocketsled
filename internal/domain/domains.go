package domain

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"
)

// Domains says where customers' own hostnames land: a param of one component,
// and the field of its resource they point a CNAME at.
type Domains struct {
	Component   string
	Param       string
	TargetField string
}

// DomainSetting is the hostnames an app answers on. List says whether the
// param takes a list of hostnames or a single one.
type DomainSetting struct {
	Hosts []string `json:"hosts"`
	List  bool     `json:"list"`
}

func (d *DomainSetting) value() any {
	if d.List {
		out := make([]any, len(d.Hosts))
		for i, h := range d.Hosts {
			out[i] = h
		}
		return out
	}
	if len(d.Hosts) == 0 {
		return ""
	}
	return d.Hosts[0]
}

// DomainStatus is pending until the component has deployed with the domain.
type DomainStatus string

const (
	DomainStatusPending    DomainStatus = "pending"
	DomainStatusConfigured DomainStatus = "configured"
)

// DomainRecord is a domain with the DNS record the customer must create.
type DomainRecord struct {
	Domain     string       `json:"domain"`
	RecordType string       `json:"recordType"`
	Target     string       `json:"target"`
	Status     DomainStatus `json:"status"`
}

// DomainsView lists an app's domains. Target is empty until the component has
// deployed and reported an address.
type DomainsView struct {
	Domains []DomainRecord `json:"domains"`
	Target  string         `json:"target"`
	// Multiple is true when the app can answer on more than one domain.
	Multiple bool `json:"multiple"`
}

// DomainsInput replaces an app's custom domains.
type DomainsInput struct {
	Domains []string `json:"domains"`
}

var hostLabel = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

// NormalizeHostnames lowercases and validates hostnames.
func NormalizeHostnames(in []string) ([]string, error) {
	out := make([]string, 0, len(in))
	seen := map[string]bool{}
	for _, raw := range in {
		h := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(raw)), ".")
		if h == "" {
			return nil, Invalid("domains", "a domain is empty")
		}
		if len(h) > 253 {
			return nil, Invalid("domains", "%s is longer than 253 characters", h)
		}
		labels := strings.Split(h, ".")
		if len(labels) < 2 {
			return nil, Invalid("domains", "%s needs a dot, like www.example.com", h)
		}
		for _, l := range labels {
			if !hostLabel.MatchString(l) {
				return nil, Invalid("domains", "%s is not a hostname: use letters, digits and hyphens between dots", h)
			}
		}
		if strings.Trim(labels[len(labels)-1], "0123456789") == "" {
			return nil, Invalid("domains", "%s is an address, not a hostname", h)
		}
		if seen[h] {
			return nil, Invalid("domains", "%s is listed twice", h)
		}
		seen[h] = true
		out = append(out, h)
	}
	return out, nil
}

func (s *Service) domainsApp(ctx context.Context, account *Account, id string) (*Environment, *Domains, error) {
	if !s.tenant() || s.paas.Domains == nil {
		return nil, nil, fmt.Errorf("%w: custom domains are not offered", ErrNotFound)
	}
	env, ro, err := s.tenantApp(ctx, account, id)
	if err != nil {
		return nil, nil, err
	}
	d := s.paas.Domains
	if _, picked := env.Presets[d.Component]; !ro.Domains || !picked {
		return nil, nil, fmt.Errorf("%w: %s has no custom domains", ErrNotFound, env.Name)
	}
	return env, d, nil
}

// SetDomains replaces an app's custom domains and deploys the one component
// they land on.
func (s *Service) SetDomains(ctx context.Context, user *User, account *Account, id string, in DomainsInput) (*Run, error) {
	env, d, err := s.domainsApp(ctx, account, id)
	if err != nil {
		return nil, err
	}
	hosts, err := NormalizeHostnames(in.Domains)
	if err != nil {
		return nil, err
	}
	platform := s.platforms(s.shared)
	live, err := platform.GetEnvironment(ctx, env.EnvironmentID)
	if err != nil {
		return nil, fmt.Errorf("load %s: %w", env.EnvironmentID, err)
	}
	pi, ok := instanceOf(live, d.Component)
	if !ok {
		return nil, fmt.Errorf("%w: %s has no %s", ErrNotFound, env.EnvironmentID, d.Component)
	}
	list := paramIsList(pi.ParamsSchema, d.Param)
	if !list && len(hosts) > 1 {
		return nil, Invalid("domains", "%s answers on one domain", env.Name)
	}
	if err := s.domainsFree(ctx, platform, env, hosts); err != nil {
		return nil, err
	}
	previous := env.Domains
	env.Domains = nil
	if len(hosts) > 0 {
		env.Domains = &DomainSetting{Hosts: hosts, List: list}
	}

	rt := s.runtimeConfig(env.Runtime)
	params := s.componentParams(env, rt, d.Component, pi.Params)
	if _, pinned := s.pinnedFor(rt, d.Component, appVars(env, rt))[d.Param]; env.Domains == nil && !pinned {
		params[d.Param] = (&DomainSetting{List: list}).value()
	}
	summary := "Domains removed"
	if len(hosts) > 0 {
		summary = "Domains: " + strings.Join(hosts, ", ")
	}
	envID, envRecordID, slug := env.EnvironmentID, env.ID, d.Component
	return s.startRun(ctx, user, env, TriggerDomains, summary, func(bg context.Context) error {
		err := s.deployInLayers(bg, platform, envID, map[string]map[string]any{slug: params}, nil, summary+" by "+user.Login)
		if err != nil {
			// A domain the bundle rejected must not ride along on every later deploy.
			if stored, getErr := s.store.GetEnvironment(bg, envRecordID); getErr == nil {
				stored.Domains = previous
				_ = s.store.UpdateEnvironment(bg, stored)
			}
		}
		return err
	})
}

// domainsFree refuses a hostname another app on this server already answers
// on, so one customer can't take another's traffic.
func (s *Service) domainsFree(ctx context.Context, platform Platform, env *Environment, hosts []string) error {
	if len(hosts) == 0 {
		return nil
	}
	all, _, err := s.madeHere(ctx, platform)
	if err != nil {
		return err
	}
	for _, e := range all {
		if e.ID == env.ID {
			continue
		}
		other, err := s.store.GetEnvironment(ctx, e.ID)
		if err != nil || other.Domains == nil {
			continue
		}
		for _, h := range hosts {
			if slices.Contains(other.Domains.Hosts, h) {
				return Invalid("domains", "%s is already in use", h)
			}
		}
	}
	return nil
}

// AppDomains lists an app's domains with the CNAME each needs. A domain is
// configured once the component has deployed with it.
func (s *Service) AppDomains(ctx context.Context, account *Account, id string) (*DomainsView, error) {
	env, d, err := s.domainsApp(ctx, account, id)
	if err != nil {
		return nil, err
	}
	live, err := s.platforms(s.shared).GetEnvironment(ctx, env.EnvironmentID)
	if err != nil {
		return nil, fmt.Errorf("load %s: %w", env.EnvironmentID, err)
	}
	pi, _ := instanceOf(live, d.Component)
	view := &DomainsView{Domains: []DomainRecord{}, Target: domainTarget(pi, d.TargetField), Multiple: paramIsList(pi.ParamsSchema, d.Param)}
	if env.Domains == nil {
		return view, nil
	}
	status := DomainStatusPending
	if strings.EqualFold(pi.Status, "PROVISIONED") && sameValue(pi.Params[d.Param], env.Domains.value()) {
		status = DomainStatusConfigured
	}
	for _, h := range env.Domains.Hosts {
		view.Domains = append(view.Domains, DomainRecord{Domain: h, RecordType: "CNAME", Target: view.Target, Status: status})
	}
	return view, nil
}

// adoptDomains reads the domains back from a live instance: a value that is
// a valid hostname and not what the operator pins.
func (s *Service) adoptDomains(env *Environment, live *PlatformEnvironment, rt Runtime) {
	d := s.paas.Domains
	if d == nil {
		return
	}
	pi, ok := instanceOf(live, d.Component)
	if !ok {
		return
	}
	pinned := s.pinnedFor(rt, d.Component, appVars(env, rt))[d.Param]
	value := pi.Params[d.Param]
	if value == nil || sameValue(value, pinned) {
		return
	}
	var raw []string
	list := false
	switch v := value.(type) {
	case string:
		raw = []string{v}
	case []any:
		list = true
		for _, item := range v {
			if s, ok := item.(string); ok {
				raw = append(raw, s)
			}
		}
	default:
		return
	}
	hosts, err := NormalizeHostnames(raw)
	if err != nil || len(hosts) == 0 {
		return
	}
	env.Domains = &DomainSetting{Hosts: hosts, List: list}
}

func instanceOf(env *PlatformEnvironment, slug string) (PlatformInstance, bool) {
	for _, pi := range env.Instances {
		if pi.ComponentSlug == slug {
			return pi, true
		}
	}
	return PlatformInstance{}, false
}

func paramIsList(schema map[string]any, param string) bool {
	props, _ := schema["properties"].(map[string]any)
	p, _ := props[param].(map[string]any)
	t, _ := p["type"].(string)
	return t == "array"
}

// domainTarget is the host a CNAME points at: the target field of the
// component's resources, without a scheme or path.
func domainTarget(pi PlatformInstance, field string) string {
	for _, r := range pi.Resources {
		v, ok := r.Payload[field].(string)
		if !ok || v == "" {
			continue
		}
		if u, err := url.Parse(v); err == nil && u.Host != "" {
			return u.Hostname()
		}
		host, _, _ := strings.Cut(v, "/")
		return host
	}
	return ""
}

func sameValue(a, b any) bool {
	return fmt.Sprint(a) == fmt.Sprint(b)
}

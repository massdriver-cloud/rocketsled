package domain

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// TokenPrefix starts every deploy token, so a leaked one is easy to recognize.
const TokenPrefix = "rs_"

// Token lets a build release images to an account's environments. Only its
// hash is stored; the secret is shown once, when it is created.
type Token struct {
	ID         string     `json:"id"`
	AccountID  string     `json:"-"`
	Name       string     `json:"name"`
	CreatedBy  string     `json:"createdBy"`
	CreatedAt  time.Time  `json:"createdAt"`
	LastUsedAt *time.Time `json:"lastUsedAt,omitempty"`
}

// NewToken is a token with its secret, returned only when it is created.
type NewToken struct {
	Token
	Secret string `json:"token"`
}

// Release is an image put on an environment's app component.
type Release struct {
	ID            string    `json:"id"`
	EnvironmentID string    `json:"environmentId"`
	Number        int       `json:"number"`
	Image         string    `json:"image"`
	Description   string    `json:"description,omitempty"`
	RequestedBy   string    `json:"requestedBy"`
	RunID         string    `json:"runId"`
	CreatedAt     time.Time `json:"createdAt"`
}

// ReleaseInput releases an image.
type ReleaseInput struct {
	Image       string `json:"image"`
	Description string `json:"description,omitempty"`
}

// ReleaseTarget is where an environment's releases land.
type ReleaseTarget struct {
	Component string `json:"component"`
	Param     string `json:"param"`
}

const tokenUserPrefix = "token:"

// CreateToken mints a deploy token for the account.
func (s *Service) CreateToken(ctx context.Context, user *User, account *Account, name string) (*NewToken, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 64 {
		return nil, Invalid("name", "name the token, in up to 64 characters")
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return nil, err
	}
	secret := TokenPrefix + base64.RawURLEncoding.EncodeToString(raw)
	t := &Token{ID: s.newID(), AccountID: account.ID, Name: name, CreatedBy: user.ID, CreatedAt: s.now()}
	if err := s.store.CreateToken(ctx, t, hashToken(secret)); err != nil {
		return nil, err
	}
	return &NewToken{Token: *t, Secret: secret}, nil
}

// ListTokens lists the account's deploy tokens, without their secrets.
func (s *Service) ListTokens(ctx context.Context, account *Account) ([]Token, error) {
	return s.store.ListTokens(ctx, account.ID)
}

// RevokeToken deletes a deploy token.
func (s *Service) RevokeToken(ctx context.Context, account *Account, id string) error {
	return s.store.DeleteToken(ctx, account.ID, id)
}

// Authenticate finds the account a deploy token belongs to, and a user that
// stands for the token in runs and releases.
func (s *Service) Authenticate(ctx context.Context, secret string) (*Account, *User, error) {
	if !strings.HasPrefix(secret, TokenPrefix) {
		return nil, nil, fmt.Errorf("%w: not a deploy token", ErrForbidden)
	}
	t, err := s.store.GetTokenByHash(ctx, hashToken(secret))
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, nil, fmt.Errorf("%w: unknown or revoked deploy token", ErrForbidden)
		}
		return nil, nil, err
	}
	account, err := s.store.GetAccount(ctx, t.AccountID)
	if err != nil {
		return nil, nil, err
	}
	if err := s.store.TouchToken(ctx, t.ID, s.now()); err != nil {
		s.log.Warn("recording token use", "token", t.ID, "err", err)
	}
	return account, &User{ID: tokenUserPrefix + t.ID, Login: t.Name, Name: t.Name}, nil
}

func hashToken(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

// Release sets the image param on the environment's app component and deploys
// only that instance. ref is the environment's ID or slug.
func (s *Service) Release(ctx context.Context, user *User, account *Account, ref string, in ReleaseInput) (*Release, error) {
	env, err := s.environmentByRef(ctx, account, ref)
	if err != nil {
		return nil, err
	}
	target, err := s.releaseTarget(account, env)
	if err != nil {
		return nil, err
	}
	image := strings.TrimSpace(in.Image)
	if image == "" {
		return nil, Invalid("image", "name the image to release")
	}
	platform, err := s.platformOf(ctx, account)
	if err != nil {
		return nil, err
	}
	live, err := platform.GetEnvironment(ctx, env.EnvironmentID)
	if err != nil {
		return nil, fmt.Errorf("load %s: %w", env.EnvironmentID, err)
	}
	var app *PlatformInstance
	for i := range live.Instances {
		if live.Instances[i].ComponentSlug == target.Component {
			app = &live.Instances[i]
		}
	}
	if app == nil {
		return nil, fmt.Errorf("%w: %s has no %s component", ErrConflict, env.Name, target.Component)
	}
	if err := checkParam(app.ParamsSchema, target.Param, image); err != nil {
		return nil, err
	}

	previous, err := s.store.ListReleases(ctx, env.ID, 1)
	if err != nil {
		return nil, err
	}
	number := 1
	if len(previous) > 0 {
		number = previous[0].Number + 1
	}
	summary := fmt.Sprintf("Release #%d", number)
	if d := strings.TrimSpace(in.Description); d != "" {
		summary += ": " + d
	}
	released := *env
	released.Image = image
	var params map[string]any
	if s.tenant() {
		params = s.componentParams(&released, s.runtimeConfig(env.Runtime), target.Component, app.Params)
	} else {
		params = MergeParams(app.Params, map[string]any{target.Param: image})
	}
	instanceID, envRecordID, envID := app.ID, env.ID, env.EnvironmentID
	run, err := s.startRun(ctx, user, env, TriggerRelease, summary, func(bg context.Context) error {
		deployment, err := platform.StartDeployment(bg, instanceID, params, summary+" by "+user.Login)
		if err != nil {
			return fmt.Errorf("release %s: %w", target.Component, err)
		}
		// The image counts as released only once its own deployment completes,
		// so a bad one is never carried into the next deploy. The instance's
		// status can't tell: it still says PROVISIONED from the last deploy.
		if err := s.waitDeployment(bg, platform, deployment, envID); err != nil {
			return err
		}
		stored, err := s.store.GetEnvironment(bg, envRecordID)
		if err != nil {
			return err
		}
		stored.Image = image
		return s.store.UpdateEnvironment(bg, stored)
	})
	if err != nil {
		return nil, err
	}
	release := &Release{
		ID: s.newID(), EnvironmentID: env.ID, Number: number, Image: image, Description: strings.TrimSpace(in.Description),
		RequestedBy: user.Login, RunID: run.ID, CreatedAt: s.now(),
	}
	if err := s.store.CreateRelease(ctx, release); err != nil {
		return nil, err
	}
	return release, nil
}

// waitDeployment polls a deployment until it completes.
func (s *Service) waitDeployment(ctx context.Context, platform Platform, deploymentID, envID string) error {
	deadline := time.Now().Add(deployTimeout)
	for {
		status, err := platform.DeploymentStatus(ctx, deploymentID)
		if err == nil {
			switch strings.ToUpper(status) {
			case "COMPLETED":
				return nil
			case "FAILED", "ABORTED", "REJECTED":
				return fmt.Errorf("the deployment to %s %s", envID, strings.ToLower(status))
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("still deploying to %s after %s", envID, deployTimeout)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(deployPollEvery):
		}
	}
}

// ListReleases lists an environment's releases, newest first.
func (s *Service) ListReleases(ctx context.Context, account *Account, ref string) ([]Release, error) {
	env, err := s.environmentByRef(ctx, account, ref)
	if err != nil {
		return nil, err
	}
	return s.store.ListReleases(ctx, env.ID, 50)
}

// environmentByRef finds an owned environment by ID or, for a build that only
// knows the name it deploys to, by slug.
func (s *Service) environmentByRef(ctx context.Context, account *Account, ref string) (*Environment, error) {
	if env, err := s.bySlug(ctx, account, ref); env != nil || err != nil {
		return env, err
	}
	if s.tenant() {
		// The store may have lost an app Massdriver still has.
		if err := s.syncTenant(ctx, account); err == nil {
			if env, err := s.bySlug(ctx, account, ref); env != nil || err != nil {
				return env, err
			}
		}
	}
	return s.ownedEnvironment(ctx, account, ref)
}

func (s *Service) bySlug(ctx context.Context, account *Account, slug string) (*Environment, error) {
	envs, err := s.store.ListEnvironments(ctx, account.ID)
	if err != nil {
		return nil, err
	}
	for i := range envs {
		if envs[i].Slug == slug {
			return &envs[i], nil
		}
	}
	return nil, nil
}

// releaseTarget is the runtime's app component in tenant mode, the catalog's otherwise.
func (s *Service) releaseTarget(account *Account, env *Environment) (*ReleaseTarget, error) {
	var t ReleaseTarget
	if s.tenant() {
		rt := s.runtimeConfig(env.Runtime)
		t = ReleaseTarget{Component: rt.AppComponent, Param: rt.ImageParam}
	} else if c, err := s.GetCatalog(account.CatalogID); err == nil {
		t = ReleaseTarget{Component: c.Bootstrap.AppComponent, Param: c.Bootstrap.ImageParam}
	}
	if t.Component == "" || t.Param == "" {
		return nil, fmt.Errorf("%w: %s has no app component to release images to", ErrConflict, env.Name)
	}
	return &t, nil
}

// platformOf is the operator's organization in tenant mode, the account's otherwise.
func (s *Service) platformOf(ctx context.Context, account *Account) (Platform, error) {
	if s.tenant() {
		return s.platforms(s.shared), nil
	}
	p, _, err := s.platformFor(ctx, account)
	return p, err
}

// checkParam validates a string against a param's JSON Schema pattern and
// length, using the bundle's own message when it has one.
func checkParam(schema map[string]any, param, value string) error {
	props, _ := schema["properties"].(map[string]any)
	prop, _ := props[param].(map[string]any)
	if prop == nil {
		return nil
	}
	if limit, ok := prop["maxLength"].(float64); ok && len(value) > int(limit) {
		return Invalid("image", "use at most %d characters", int(limit))
	}
	pattern, _ := prop["pattern"].(string)
	if pattern == "" {
		return nil
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil //nolint:nilerr // a pattern Go's regexp can't compile is left to Massdriver's own validation
	}
	if re.MatchString(value) {
		return nil
	}
	msg := "must match " + pattern
	if m, ok := prop["message"].(map[string]any); ok {
		if custom, ok := m["pattern"].(string); ok && custom != "" {
			msg = custom
		}
	}
	return Invalid("image", "%s", msg)
}

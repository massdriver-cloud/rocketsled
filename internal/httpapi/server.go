// Package httpapi exposes Rocketsled over REST and serves the embedded UI.
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/massdriver-cloud/rocketsled/internal/auth"
	"github.com/massdriver-cloud/rocketsled/internal/domain"
	"github.com/massdriver-cloud/rocketsled/internal/skin"
)

// Server holds the handlers' dependencies.
type Server struct {
	svc      *domain.Service
	sessions *auth.Sessions
	baseURL  string
	ui       fs.FS
	log      *slog.Logger
	mux      *http.ServeMux
	skin     *skin.Skin
	mode     string
	index    []byte
}

// Options configures a Server.
type Options struct {
	Service  *domain.Service
	Sessions *auth.Sessions
	BaseURL  string
	UI       fs.FS
	Logger   *slog.Logger
	// Skin, when set, replaces Rocketsled's own name and look.
	Skin *skin.Skin
	// Mode is the account mode, which decides which UI the browser shows.
	Mode string
}

// New wires every route.
func New(o Options) *Server {
	s := &Server{
		svc:      o.Service,
		sessions: o.Sessions,
		baseURL:  strings.TrimRight(o.BaseURL, "/"),
		ui:       o.UI,
		log:      o.Logger,
		mux:      http.NewServeMux(),
		skin:     o.Skin,
		mode:     o.Mode,
	}
	s.index = s.renderIndex()
	if s.log == nil {
		s.log = slog.Default()
	}
	s.routes()
	return s
}

func (s *Server) routes() {
	m := s.mux
	m.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	m.HandleFunc("POST /auth/password", s.handlePasswordLogin)
	m.HandleFunc("POST /auth/signup", s.handleSignUp)
	m.HandleFunc("GET /auth/{provider}/login", s.handleLogin)
	m.HandleFunc("GET /skin/{file}", s.handleSkinFile)
	m.HandleFunc("GET /api/v1/offer", s.account(s.handleOffer))
	m.HandleFunc("GET /auth/{provider}/callback", s.handleCallback)

	m.HandleFunc("GET /api/v1/meta", s.handleMeta)
	m.HandleFunc("POST /api/v1/logout", s.handleLogout)
	m.HandleFunc("GET /api/v1/me", s.user(s.handleMe))
	m.HandleFunc("GET /api/v1/catalogs", s.user(s.handleCatalogs))
	m.HandleFunc("POST /api/v1/account", s.user(s.handleCreateAccount))
	m.HandleFunc("GET /api/v1/account", s.account(s.handleGetAccount))
	m.HandleFunc("POST /api/v1/account/install", s.account(s.handleInstall))
	m.HandleFunc("GET /api/v1/account/platforms", s.account(s.handlePlatforms))
	m.HandleFunc("POST /api/v1/account/platforms/{type}", s.account(s.handleProvidePlatform))
	m.HandleFunc("GET /api/v1/project", s.account(s.handleProject))
	m.HandleFunc("GET /api/v1/environments", s.account(s.handleListEnvironments))
	m.HandleFunc("POST /api/v1/environments", s.account(s.handleCreateEnvironment))
	m.HandleFunc("GET /api/v1/environments/{id}", s.account(s.handleGetEnvironment))
	m.HandleFunc("DELETE /api/v1/environments/{id}", s.account(s.handleDestroyEnvironment))
	m.HandleFunc("POST /api/v1/environments/{id}/deploys", s.account(s.handleDeployEnvironment))
	m.HandleFunc("GET /api/v1/account/tokens", s.account(s.handleListTokens))
	m.HandleFunc("POST /api/v1/account/tokens", s.account(s.handleCreateToken))
	m.HandleFunc("DELETE /api/v1/account/tokens/{id}", s.account(s.handleRevokeToken))
	m.HandleFunc("GET /api/v1/environments/{id}/releases", s.releaser(s.handleListReleases))
	m.HandleFunc("POST /api/v1/environments/{id}/releases", s.releaser(s.handleRelease))
	m.HandleFunc("POST /api/v1/environments/{id}/addons", s.account(s.handleAddAddon))
	m.HandleFunc("DELETE /api/v1/environments/{id}/addons/{component}", s.account(s.handleRemoveAddon))
	m.HandleFunc("/api/", func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusNotFound, "no such endpoint", "")
	})

	m.HandleFunc("/", s.handleUI)
}

// ServeHTTP adds request logging around the mux.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	rw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
	s.mux.ServeHTTP(rw, r)
	if strings.HasPrefix(r.URL.Path, "/assets/") {
		return
	}
	s.log.Info("http", "method", r.Method, "path", r.URL.Path, "status", rw.status, "ms", time.Since(start).Milliseconds())
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

// --- helpers -----------------------------------------------------------------

type (
	userKey    struct{}
	accountKey struct{}
)

func userFrom(ctx context.Context) *domain.User {
	u, _ := ctx.Value(userKey{}).(*domain.User)
	return u
}

func accountFrom(ctx context.Context) *domain.Account {
	a, _ := ctx.Value(accountKey{}).(*domain.Account)
	return a
}

// user requires a signed-in user.
func (s *Server) user(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := s.sessions.UserFromRequest(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "sign in to continue", "")
			return
		}
		u, err := s.svc.GetUser(r.Context(), id)
		if err != nil {
			s.sessions.ClearSession(w)
			writeError(w, http.StatusUnauthorized, "sign in to continue", "")
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), userKey{}, u)))
	}
}

// account requires a signed-in user who has completed signup.
func (s *Server) account(next http.HandlerFunc) http.HandlerFunc {
	return s.user(func(w http.ResponseWriter, r *http.Request) {
		a, err := s.svc.AccountFor(r.Context(), userFrom(r.Context()))
		if err != nil {
			writeError(w, http.StatusPreconditionFailed, "finish signing up first", "")
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), accountKey{}, a)))
	})
}

// releaser accepts a deploy token as a bearer token, or a signed-in user.
func (s *Server) releaser(next http.HandlerFunc) http.HandlerFunc {
	session := s.account(next)
	return func(w http.ResponseWriter, r *http.Request) {
		scheme, secret, _ := strings.Cut(r.Header.Get("Authorization"), " ")
		if !strings.EqualFold(scheme, "Bearer") {
			session(w, r)
			return
		}
		a, u, err := s.svc.Authenticate(r.Context(), strings.TrimSpace(secret))
		if err != nil {
			if errors.Is(err, domain.ErrForbidden) {
				writeError(w, http.StatusUnauthorized, err.Error(), "")
				return
			}
			s.writeDomainError(w, err)
			return
		}
		ctx := context.WithValue(context.WithValue(r.Context(), userKey{}, u), accountKey{}, a)
		next(w, r.WithContext(ctx))
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

type errorBody struct {
	Error string `json:"error"`
	Field string `json:"field,omitempty"`
}

func writeError(w http.ResponseWriter, status int, msg, field string) {
	writeJSON(w, status, errorBody{Error: msg, Field: field})
}

func (s *Server) writeDomainError(w http.ResponseWriter, err error) {
	var verr *domain.ValidationError
	switch {
	case errors.As(err, &verr):
		writeError(w, http.StatusUnprocessableEntity, verr.Message, verr.Field)
	case errors.Is(err, domain.ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error(), "")
	case errors.Is(err, domain.ErrForbidden):
		writeError(w, http.StatusForbidden, err.Error(), "")
	case errors.Is(err, domain.ErrConflict):
		writeError(w, http.StatusConflict, err.Error(), "")
	default:
		s.log.Error("request failed", "err", err)
		writeError(w, http.StatusBadGateway, err.Error(), "")
	}
}

func decode(r *http.Request, v any) error {
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return domain.Invalid("body", "invalid JSON: %s", err.Error())
	}
	return nil
}

// --- auth --------------------------------------------------------------------

func (s *Server) callbackURL(p domain.Provider) string {
	return s.baseURL + "/auth/" + string(p) + "/callback"
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	provider := domain.Provider(r.PathValue("provider"))
	scm, err := s.svc.SCM(provider)
	if err != nil {
		writeError(w, http.StatusNotFound, "unknown sign-in provider", "")
		return
	}
	state := s.sessions.BeginOAuth(w, r.URL.Query().Get("next"))
	http.Redirect(w, r, scm.AuthCodeURL(state, s.callbackURL(provider)), http.StatusFound) //nolint:gosec // provider authorize URL built from config
}

func (s *Server) handleCallback(w http.ResponseWriter, r *http.Request) {
	provider := domain.Provider(r.PathValue("provider"))
	if msg := r.URL.Query().Get("error_description"); msg != "" {
		http.Redirect(w, r, "/login?error="+url.QueryEscape(msg), http.StatusFound)
		return
	}
	st, err := s.sessions.FinishOAuth(w, r, r.URL.Query().Get("state"))
	if err != nil {
		http.Redirect(w, r, "/login?error=sign-in+expired,+try+again", http.StatusFound)
		return
	}
	user, err := s.svc.SignIn(r.Context(), provider, r.URL.Query().Get("code"), s.callbackURL(provider))
	if err != nil {
		s.log.Warn("sign-in failed", "provider", provider, "err", err)
		http.Redirect(w, r, "/login?error=sign-in+failed", http.StatusFound)
		return
	}
	s.sessions.SetSession(w, user.ID)
	http.Redirect(w, r, st.Next, http.StatusFound)
}

func (s *Server) handleLogout(w http.ResponseWriter, _ *http.Request) {
	s.sessions.ClearSession(w)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleMeta(w http.ResponseWriter, _ *http.Request) {
	meta := map[string]any{"providers": s.svc.Providers(), "baseUrl": s.baseURL, "mode": s.mode}
	if s.skin != nil {
		sk := *s.skin
		sk.Logo, sk.Favicon, sk.CSS = skin.URL(sk.Logo), skin.URL(sk.Favicon), skin.URL(sk.CSS)
		meta["skin"] = sk
	}
	writeJSON(w, http.StatusOK, meta)
}

func (s *Server) handlePasswordLogin(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Login    string `json:"login"`
		Password string `json:"password"`
	}
	if err := decode(r, &in); err != nil {
		s.writeDomainError(w, err)
		return
	}
	user, err := s.svc.SignInWithPassword(r.Context(), in.Login, in.Password)
	if err != nil {
		s.writeDomainError(w, err)
		return
	}
	s.sessions.SetSession(w, user.ID)
	writeJSON(w, http.StatusOK, user)
}

func (s *Server) handleSignUp(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Login      string `json:"login"`
		InviteCode string `json:"inviteCode"`
	}
	if err := decode(r, &in); err != nil {
		s.writeDomainError(w, err)
		return
	}
	user, err := s.svc.SignUp(r.Context(), in.Login, in.InviteCode)
	if err != nil {
		s.writeDomainError(w, err)
		return
	}
	s.sessions.SetSession(w, user.ID)
	writeJSON(w, http.StatusOK, user)
}

func (s *Server) handleOffer(w http.ResponseWriter, r *http.Request) {
	offer, err := s.svc.Offer(r.Context())
	if err != nil {
		s.writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, offer)
}

func (s *Server) handleSkinFile(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("file")
	if s.skin == nil || (name != s.skin.Logo && name != s.skin.Favicon && name != s.skin.CSS) {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=300")
	http.ServeFileFS(w, r, s.skin.Files, name)
}

// renderIndex puts the skin's title, icon and tokens into index.html.
func (s *Server) renderIndex() []byte {
	if s.ui == nil || s.skin == nil {
		return nil
	}
	raw, err := fs.ReadFile(s.ui, "index.html")
	if err != nil {
		return nil
	}
	page := string(raw)
	if start := strings.Index(page, "<title>"); start >= 0 {
		if end := strings.Index(page[start:], "</title>"); end >= 0 {
			page = page[:start] + page[start+end+len("</title>"):]
		}
	}
	if s.skin.Favicon != "" {
		if start := strings.Index(page, `<link rel="icon"`); start >= 0 {
			if end := strings.Index(page[start:], ">"); end >= 0 {
				page = page[:start] + page[start+end+1:]
			}
		}
	}
	return []byte(strings.Replace(page, "</head>", s.skin.Head()+"</head>", 1))
}

// meResponse is the signed-in user and their account, if signup is complete.
type meResponse struct {
	User    *domain.User    `json:"user"`
	Account *domain.Account `json:"account,omitempty"`
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r.Context())
	a, _ := s.svc.AccountFor(r.Context(), u)
	writeJSON(w, http.StatusOK, meResponse{User: u, Account: a})
}

// --- account -----------------------------------------------------------------

func (s *Server) handleCatalogs(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.svc.ListCatalogs())
}

func (s *Server) handleCreateAccount(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name string `json:"name"`
	}
	if r.ContentLength != 0 {
		if err := decode(r, &in); err != nil {
			s.writeDomainError(w, err)
			return
		}
	}
	a, err := s.svc.CreateAccount(r.Context(), userFrom(r.Context()), in.Name)
	if err != nil {
		s.writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, a)
}

func (s *Server) handleGetAccount(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, accountFrom(r.Context()))
}

func (s *Server) handleInstall(w http.ResponseWriter, r *http.Request) {
	var in struct {
		CatalogID string `json:"catalogId"`
	}
	if err := decode(r, &in); err != nil {
		s.writeDomainError(w, err)
		return
	}
	a, err := s.svc.InstallCatalog(r.Context(), accountFrom(r.Context()), in.CatalogID)
	if err != nil {
		s.writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, a)
}

func (s *Server) handlePlatforms(w http.ResponseWriter, r *http.Request) {
	reqs, err := s.svc.PlatformRequirements(r.Context(), accountFrom(r.Context()))
	if err != nil {
		s.writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, reqs)
}

func (s *Server) handleProvidePlatform(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name    string         `json:"name"`
		Payload map[string]any `json:"payload"`
	}
	if err := decode(r, &in); err != nil {
		s.writeDomainError(w, err)
		return
	}
	a, err := s.svc.ProvidePlatform(r.Context(), accountFrom(r.Context()), r.PathValue("type"), in.Name, in.Payload)
	if err != nil {
		s.writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, a)
}

// --- project & environments --------------------------------------------------

func (s *Server) handleProject(w http.ResponseWriter, r *http.Request) {
	view, err := s.svc.Project(r.Context(), accountFrom(r.Context()))
	if err != nil {
		s.writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) handleListEnvironments(w http.ResponseWriter, r *http.Request) {
	envs, err := s.svc.ListEnvironments(r.Context(), accountFrom(r.Context()))
	if err != nil {
		s.writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, envs)
}

func (s *Server) handleCreateEnvironment(w http.ResponseWriter, r *http.Request) {
	var in domain.CreateEnvironmentInput
	if err := decode(r, &in); err != nil {
		s.writeDomainError(w, err)
		return
	}
	env, err := s.svc.CreateEnvironment(r.Context(), userFrom(r.Context()), accountFrom(r.Context()), in)
	if err != nil {
		s.writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, env)
}

func (s *Server) handleGetEnvironment(w http.ResponseWriter, r *http.Request) {
	view, err := s.svc.EnvironmentStatus(r.Context(), accountFrom(r.Context()), r.PathValue("id"))
	if err != nil {
		s.writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) handleDestroyEnvironment(w http.ResponseWriter, r *http.Request) {
	env, err := s.svc.DestroyEnvironment(r.Context(), accountFrom(r.Context()), r.PathValue("id"))
	if err != nil {
		s.writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, env)
}

func (s *Server) handleDeployEnvironment(w http.ResponseWriter, r *http.Request) {
	run, err := s.svc.DeployEnvironment(r.Context(), userFrom(r.Context()), accountFrom(r.Context()), r.PathValue("id"))
	if err != nil {
		s.writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, run)
}

func (s *Server) handleListTokens(w http.ResponseWriter, r *http.Request) {
	tokens, err := s.svc.ListTokens(r.Context(), accountFrom(r.Context()))
	if err != nil {
		s.writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, tokens)
}

func (s *Server) handleCreateToken(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name string `json:"name"`
	}
	if err := decode(r, &in); err != nil {
		s.writeDomainError(w, err)
		return
	}
	t, err := s.svc.CreateToken(r.Context(), userFrom(r.Context()), accountFrom(r.Context()), in.Name)
	if err != nil {
		s.writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, t)
}

func (s *Server) handleRevokeToken(w http.ResponseWriter, r *http.Request) {
	if err := s.svc.RevokeToken(r.Context(), accountFrom(r.Context()), r.PathValue("id")); err != nil {
		s.writeDomainError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleListReleases(w http.ResponseWriter, r *http.Request) {
	releases, err := s.svc.ListReleases(r.Context(), accountFrom(r.Context()), r.PathValue("id"))
	if err != nil {
		s.writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, releases)
}

func (s *Server) handleRelease(w http.ResponseWriter, r *http.Request) {
	var in domain.ReleaseInput
	if err := decode(r, &in); err != nil {
		s.writeDomainError(w, err)
		return
	}
	release, err := s.svc.Release(r.Context(), userFrom(r.Context()), accountFrom(r.Context()), r.PathValue("id"), in)
	if err != nil {
		s.writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, release)
}

func (s *Server) handleAddAddon(w http.ResponseWriter, r *http.Request) {
	var in domain.AddonInput
	if err := decode(r, &in); err != nil {
		s.writeDomainError(w, err)
		return
	}
	run, err := s.svc.AddAddon(r.Context(), userFrom(r.Context()), accountFrom(r.Context()), r.PathValue("id"), in)
	if err != nil {
		s.writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, run)
}

func (s *Server) handleRemoveAddon(w http.ResponseWriter, r *http.Request) {
	run, err := s.svc.RemoveAddon(r.Context(), userFrom(r.Context()), accountFrom(r.Context()), r.PathValue("id"), r.PathValue("component"))
	if err != nil {
		s.writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, run)
}

// --- UI ----------------------------------------------------------------------

// handleUI serves the embedded single-page app, falling back to index.html for
// client-side routes.
func (s *Server) handleUI(w http.ResponseWriter, r *http.Request) {
	if s.ui == nil {
		writeError(w, http.StatusNotFound, "the UI is not built; run `make ui` or use the API under /api/v1", "")
		return
	}
	name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	if name == "" {
		name = "index.html"
	}
	if s.index != nil && (name == "index.html" || !strings.HasPrefix(name, "assets/")) {
		if f, err := s.ui.Open(name); name == "index.html" || err != nil {
			if err == nil {
				_ = f.Close()
			}
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write(s.index)
			return
		}
	}
	if f, err := s.ui.Open(name); err == nil {
		_ = f.Close()
		if strings.HasPrefix(name, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		http.ServeFileFS(w, r, s.ui, name)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	http.ServeFileFS(w, r, s.ui, "index.html")
}

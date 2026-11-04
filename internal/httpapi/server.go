// Package httpapi exposes Rocketsled over REST and serves the embedded UI.
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
)

// Server holds the handlers' dependencies.
type Server struct {
	svc           *domain.Service
	sessions      *auth.Sessions
	baseURL       string
	webhookSecret string
	ui            fs.FS
	log           *slog.Logger
	mux           *http.ServeMux
}

// Options configures a Server.
type Options struct {
	Service       *domain.Service
	Sessions      *auth.Sessions
	BaseURL       string
	WebhookSecret string
	UI            fs.FS
	Logger        *slog.Logger
}

// New wires every route.
func New(o Options) *Server {
	s := &Server{
		svc:           o.Service,
		sessions:      o.Sessions,
		baseURL:       strings.TrimRight(o.BaseURL, "/"),
		webhookSecret: o.WebhookSecret,
		ui:            o.UI,
		log:           o.Logger,
		mux:           http.NewServeMux(),
	}
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

	m.HandleFunc("GET /auth/{provider}/login", s.handleLogin)
	m.HandleFunc("GET /auth/{provider}/callback", s.handleCallback)
	m.HandleFunc("POST /hooks/{provider}", s.handleWebhook)
	m.HandleFunc("POST /ci/v1/stacks/{id}/deploys", s.handleCIDeploy)

	m.HandleFunc("GET /api/v1/meta", s.handleMeta)
	m.HandleFunc("GET /api/v1/me", s.requireUser(s.handleMe))
	m.HandleFunc("POST /api/v1/logout", s.handleLogout)
	m.HandleFunc("GET /api/v1/repos", s.requireUser(s.handleListRepos))
	m.HandleFunc("GET /api/v1/projects", s.requireUser(s.handleListProjects))
	m.HandleFunc("GET /api/v1/projects/{id}", s.requireUser(s.handleGetProject))
	m.HandleFunc("GET /api/v1/stacks", s.requireUser(s.handleListStacks))
	m.HandleFunc("POST /api/v1/stacks", s.requireUser(s.handleCreateStack))
	m.HandleFunc("GET /api/v1/stacks/{id}", s.requireUser(s.handleGetStack))
	m.HandleFunc("DELETE /api/v1/stacks/{id}", s.requireUser(s.handleDeleteStack))
	m.HandleFunc("GET /api/v1/stacks/{id}/instances", s.requireUser(s.handleListInstances))
	m.HandleFunc("POST /api/v1/stacks/{id}/instances", s.requireUser(s.handleCreateInstance))
	m.HandleFunc("GET /api/v1/instances/{id}", s.requireUser(s.handleGetInstance))
	m.HandleFunc("DELETE /api/v1/instances/{id}", s.requireUser(s.handleDestroyInstance))
	m.HandleFunc("POST /api/v1/instances/{id}/deploys", s.requireUser(s.handleDeploy))
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

type ctxKey struct{}

func userFrom(ctx context.Context) *domain.User {
	u, _ := ctx.Value(ctxKey{}).(*domain.User)
	return u
}

func (s *Server) requireUser(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := s.sessions.UserFromRequest(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "sign in to continue", "")
			return
		}
		user, err := s.svc.GetUser(r.Context(), id)
		if err != nil {
			s.sessions.ClearSession(w)
			writeError(w, http.StatusUnauthorized, "sign in to continue", "")
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, user)))
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

// writeDomainError maps domain errors onto HTTP statuses.
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
	return fmt.Sprintf("%s/auth/%s/callback", s.baseURL, p)
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

type metaResponse struct {
	Providers []domain.Provider `json:"providers"`
	BaseURL   string            `json:"baseUrl"`
}

func (s *Server) handleMeta(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, metaResponse{Providers: s.svc.Providers(), BaseURL: s.baseURL})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, userFrom(r.Context()))
}

// --- repos & projects --------------------------------------------------------

func (s *Server) handleListRepos(w http.ResponseWriter, r *http.Request) {
	repos, err := s.svc.ListRepos(r.Context(), userFrom(r.Context()))
	if err != nil {
		s.writeDomainError(w, err)
		return
	}
	if repos == nil {
		repos = []domain.Repo{}
	}
	writeJSON(w, http.StatusOK, repos)
}

func (s *Server) handleListProjects(w http.ResponseWriter, r *http.Request) {
	projects, err := s.svc.ListProjects(r.Context())
	if err != nil {
		s.writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, projects)
}

func (s *Server) handleGetProject(w http.ResponseWriter, r *http.Request) {
	project, err := s.svc.GetProject(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, project)
}

// --- stacks ------------------------------------------------------------------

type stackResponse struct {
	Stack     domain.Stack      `json:"stack"`
	Instances []domain.Instance `json:"instances"`
	// DeployToken is only present in the create response.
	DeployToken string `json:"deployToken,omitempty"`
}

func (s *Server) handleListStacks(w http.ResponseWriter, r *http.Request) {
	stacks, err := s.svc.ListStacks(r.Context())
	if err != nil {
		s.writeDomainError(w, err)
		return
	}
	out := make([]stackResponse, 0, len(stacks))
	for _, st := range stacks {
		instances, err := s.svc.ListInstances(r.Context(), st.ID)
		if err != nil {
			s.writeDomainError(w, err)
			return
		}
		out = append(out, stackResponse{Stack: st, Instances: instances})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleCreateStack(w http.ResponseWriter, r *http.Request) {
	var in domain.CreateStackInput
	if err := decode(r, &in); err != nil {
		s.writeDomainError(w, err)
		return
	}
	stack, token, err := s.svc.CreateStack(r.Context(), userFrom(r.Context()), in)
	if err != nil {
		s.writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, stackResponse{Stack: *stack, Instances: []domain.Instance{}, DeployToken: token})
}

func (s *Server) handleGetStack(w http.ResponseWriter, r *http.Request) {
	stack, err := s.svc.GetStack(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeDomainError(w, err)
		return
	}
	instances, err := s.svc.ListInstances(r.Context(), stack.ID)
	if err != nil {
		s.writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, stackResponse{Stack: *stack, Instances: instances})
}

func (s *Server) handleDeleteStack(w http.ResponseWriter, r *http.Request) {
	if err := s.svc.DeleteStack(r.Context(), userFrom(r.Context()), r.PathValue("id")); err != nil {
		s.writeDomainError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- instances ---------------------------------------------------------------

func (s *Server) handleListInstances(w http.ResponseWriter, r *http.Request) {
	instances, err := s.svc.ListInstances(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, instances)
}

type launchResponse struct {
	Instance *domain.Instance `json:"instance"`
}

func (s *Server) handleCreateInstance(w http.ResponseWriter, r *http.Request) {
	var in domain.CreateInstanceInput
	if err := decode(r, &in); err != nil {
		s.writeDomainError(w, err)
		return
	}
	inst, err := s.svc.CreateInstance(r.Context(), userFrom(r.Context()), r.PathValue("id"), in)
	if err != nil {
		s.writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, launchResponse{Instance: inst})
}

func (s *Server) handleGetInstance(w http.ResponseWriter, r *http.Request) {
	view, err := s.svc.InstanceStatus(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) handleDestroyInstance(w http.ResponseWriter, r *http.Request) {
	inst, err := s.svc.DestroyInstance(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, inst)
}

func (s *Server) handleDeploy(w http.ResponseWriter, r *http.Request) {
	var in domain.DeployInput
	if r.ContentLength != 0 {
		if err := decode(r, &in); err != nil {
			s.writeDomainError(w, err)
			return
		}
	}
	in.Trigger = domain.TriggerManual
	in.Actor = userFrom(r.Context())
	d, err := s.svc.Deploy(r.Context(), r.PathValue("id"), in)
	if err != nil {
		s.writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, d)
}

// --- automation --------------------------------------------------------------

func (s *Server) handleWebhook(w http.ResponseWriter, r *http.Request) {
	provider := domain.Provider(r.PathValue("provider"))
	scm, err := s.svc.SCM(provider)
	if err != nil {
		writeError(w, http.StatusNotFound, "unknown provider", "")
		return
	}
	ev, err := scm.ParsePush(r, s.webhookSecret)
	if err != nil {
		if errors.Is(err, domain.ErrForbidden) {
			writeError(w, http.StatusForbidden, "webhook signature rejected", "")
			return
		}
		writeError(w, http.StatusBadRequest, err.Error(), "")
		return
	}
	if ev == nil {
		writeJSON(w, http.StatusAccepted, map[string]string{"status": "ignored"})
		return
	}
	deploys, err := s.svc.HandlePush(r.Context(), *ev)
	if err != nil {
		s.log.Warn("push handling had errors", "repo", ev.RepoFullName, "branch", ev.Branch, "err", err)
	}
	if deploys == nil {
		deploys = []domain.Deploy{}
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"deploys": deploys, "errors": errString(err)})
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func (s *Server) handleCIDeploy(w http.ResponseWriter, r *http.Request) {
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || token == "" {
		writeError(w, http.StatusUnauthorized, "send the stack deploy token as a Bearer token", "")
		return
	}
	var in domain.DeployInput
	if err := decode(r, &in); err != nil {
		s.writeDomainError(w, err)
		return
	}
	deploys, err := s.svc.DeployFromCI(r.Context(), r.PathValue("id"), token, in)
	if err != nil && len(deploys) == 0 {
		s.writeDomainError(w, err)
		return
	}
	if deploys == nil {
		deploys = []domain.Deploy{}
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"deploys": deploys, "errors": errString(err)})
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

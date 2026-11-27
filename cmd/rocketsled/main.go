// Command rocketsled runs the Rocketsled server.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/massdriver-cloud/rocketsled/internal/auth"
	"github.com/massdriver-cloud/rocketsled/internal/catalog"
	"github.com/massdriver-cloud/rocketsled/internal/config"
	"github.com/massdriver-cloud/rocketsled/internal/domain"
	"github.com/massdriver-cloud/rocketsled/internal/httpapi"
	"github.com/massdriver-cloud/rocketsled/internal/installer"
	"github.com/massdriver-cloud/rocketsled/internal/massdriver"
	"github.com/massdriver-cloud/rocketsled/internal/scm/dev"
	"github.com/massdriver-cloud/rocketsled/internal/scm/github"
	"github.com/massdriver-cloud/rocketsled/internal/scm/gitlab"
	"github.com/massdriver-cloud/rocketsled/internal/skin"
	"github.com/massdriver-cloud/rocketsled/internal/store/sqlite"
	"github.com/massdriver-cloud/rocketsled/web"
)

var version = "dev"

const usage = `rocketsled — a PaaS on Massdriver

Usage:
  rocketsled serve  [-config rocketsled.yaml]   start the API and UI
  rocketsled check  [-config rocketsled.yaml]   validate config, credential and catalogs
  rocketsled version
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	var err error
	switch os.Args[1] {
	case "serve":
		err = serve(os.Args[2:], log)
	case "check":
		err = check(os.Args[2:], log)
	case "version":
		fmt.Println(version)
	case "-h", "--help", "help":
		fmt.Print(usage)
	default:
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	if err != nil {
		log.Error(err.Error())
		os.Exit(1)
	}
}

func loadConfig(args []string, name string) (*config.Config, error) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	path := fs.String("config", "rocketsled.yaml", "path to the configuration file")
	envFile := fs.String("env-file", ".env", "KEY=VALUE file loaded into the environment before the config is read")
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	if err := config.LoadDotEnv(*envFile); err != nil {
		return nil, err
	}
	return config.Load(*path)
}

type app struct {
	cfg      *config.Config
	svc      *domain.Service
	catalogs []domain.Catalog
	store    *sqlite.Store
}

func build(cfg *config.Config, log *slog.Logger) (*app, error) {
	if err := os.MkdirAll(cfg.DataDir, 0o750); err != nil {
		return nil, err
	}
	store, err := sqlite.Open(cfg.Database.Path, cfg.Server.SessionSecret)
	if err != nil {
		return nil, err
	}
	catalogs, err := catalog.LoadAll(cfg.Catalogs.Dir)
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	var scms []domain.SCM
	if p := cfg.Auth.Providers.GitHub; p != nil {
		scms = append(scms, github.New(p.ClientID, p.ClientSecret, p.BaseURL, nil))
	}
	if p := cfg.Auth.Providers.GitLab; p != nil {
		scms = append(scms, gitlab.New(p.ClientID, p.ClientSecret, p.BaseURL, nil))
	}
	if p := cfg.Auth.Providers.Dev; p != nil {
		log.Warn("dev sign-in stub enabled; anyone who can reach this server is signed in as " + p.Login)
		scms = append(scms, dev.New(p.Login))
	}
	password := ""
	if p := cfg.Auth.Providers.Password; p != nil {
		password = p.Password
	}
	creatorToken := cfg.Massdriver.OrgCreatorToken
	if creatorToken == "" {
		creatorToken = cfg.Massdriver.APIKey
	}
	svc, err := domain.NewService(domain.Options{
		Platforms:        massdriver.Factory,
		Store:            store,
		SCMs:             scms,
		Installer:        installer.New(cfg.Tools.Mass, cfg.Tools.Tofu, filepath.Join(cfg.DataDir, "plugin-cache")),
		Catalogs:         catalogs,
		Mode:             domain.AccountMode(cfg.Massdriver.AccountMode),
		SharedCredential: domain.Credential{URL: cfg.Massdriver.URL, OrganizationID: cfg.Massdriver.OrganizationID, APIKey: cfg.Massdriver.APIKey},
		OrgCreator:       domain.Credential{URL: cfg.Massdriver.URL, APIKey: creatorToken},
		DataDir:          cfg.DataDir,
		StableReleases:   cfg.Catalogs.ReleaseChannel == "stable",
		PaaS:             paasFrom(cfg.PaaS),
		Password:         password,
		Logger:           log,
	})
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	return &app{cfg: cfg, svc: svc, catalogs: catalogs, store: store}, nil
}

func serve(args []string, log *slog.Logger) error {
	cfg, err := loadConfig(args, "serve")
	if err != nil {
		return err
	}
	a, err := build(cfg, log)
	if err != nil {
		return err
	}
	defer func() { _ = a.store.Close() }()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if n, err := a.svc.RecoverInterruptedInstalls(ctx); err != nil {
		log.Warn("recovering interrupted installs", "err", err)
	} else if n > 0 {
		log.Warn("marked interrupted installs as failed", "accounts", n)
	}

	shared := massdriver.Factory(domain.Credential{URL: cfg.Massdriver.URL, OrganizationID: cfg.Massdriver.OrganizationID, APIKey: cfg.Massdriver.APIKey})
	if org, err := shared.Ping(ctx); err != nil {
		log.Warn("massdriver check failed; continuing", "err", err)
	} else {
		log.Info("massdriver connected", "organization", org, "mode", cfg.Massdriver.AccountMode, "catalogs", len(a.catalogs))
	}

	var sk *skin.Skin
	if cfg.Skin != "" {
		if sk, err = skin.Load(cfg.Skin); err != nil {
			return err
		}
	}
	ui := web.Dist()
	if ui == nil {
		log.Warn("UI not built; serving API only (run `make ui`)")
	}
	handler := httpapi.New(httpapi.Options{
		Service:  a.svc,
		Sessions: auth.New(cfg.Server.SessionSecret, cfg.Server.SessionTTL, strings.HasPrefix(cfg.Server.BaseURL, "https://")),
		BaseURL:  cfg.Server.BaseURL,
		UI:       ui,
		Logger:   log,
		Skin:     sk,
		Mode:     cfg.Massdriver.AccountMode,
	})
	srv := &http.Server{Addr: cfg.Server.Addr, Handler: handler, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second}
	errc := make(chan error, 1)
	go func() {
		log.Info("rocketsled listening", "addr", cfg.Server.Addr, "base_url", cfg.Server.BaseURL, "version", version)
		errc <- srv.ListenAndServe()
	}()
	select {
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		log.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	}
	return nil
}

func check(args []string, log *slog.Logger) error {
	cfg, err := loadConfig(args, "check")
	if err != nil {
		return err
	}
	a, err := build(cfg, log)
	if err != nil {
		return err
	}
	defer func() { _ = a.store.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	shared := massdriver.Factory(domain.Credential{URL: cfg.Massdriver.URL, OrganizationID: cfg.Massdriver.OrganizationID, APIKey: cfg.Massdriver.APIKey})
	org, err := shared.Ping(ctx)
	if err != nil {
		return fmt.Errorf("massdriver: %w", err)
	}
	fmt.Printf("organization  %s (%s)\n", org, cfg.Massdriver.OrganizationID)
	fmt.Printf("account mode  %s\n", cfg.Massdriver.AccountMode)
	fmt.Printf("providers     %v\n", a.svc.Providers())
	for _, c := range a.catalogs {
		fmt.Printf("catalog       %-16s %s: %d platform(s), %d resource type(s), %d bundle(s), bootstrap %s/%s\n",
			c.ID, c.Name, len(c.Platforms), len(c.ResourceTypes), len(c.Bundles), c.Bootstrap.Project, c.Bootstrap.BaseEnvironment)
	}
	return nil
}

func paasFrom(p *config.PaaS) *domain.PaaS {
	if p == nil {
		return nil
	}
	out := &domain.PaaS{Pinned: p.Pinned, Highlights: p.Highlights, Plans: map[string]map[string]domain.Plan{}}
	for _, r := range p.Runtimes {
		out.Runtimes = append(out.Runtimes, domain.Runtime{
			ID: r.ID, Name: r.Name, Summary: r.Summary, Project: r.Project, Template: r.Template,
			AppComponent: r.AppComponent, ImageParam: r.ImageParam,
		})
	}
	for _, a := range p.Addons {
		out.Addons = append(out.Addons, domain.Addon{ID: a.ID, Title: a.Title, Summary: a.Summary})
	}
	for component, plans := range p.Plans {
		out.Plans[component] = map[string]domain.Plan{}
		for name, plan := range plans {
			out.Plans[component][name] = domain.Plan{Price: plan.Price, Summary: plan.Summary}
		}
	}
	return out
}

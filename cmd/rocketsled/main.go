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
	"strings"
	"syscall"
	"time"

	"github.com/massdriver-cloud/rocketsled/internal/auth"
	"github.com/massdriver-cloud/rocketsled/internal/builder/docker"
	"github.com/massdriver-cloud/rocketsled/internal/config"
	"github.com/massdriver-cloud/rocketsled/internal/domain"
	"github.com/massdriver-cloud/rocketsled/internal/httpapi"
	"github.com/massdriver-cloud/rocketsled/internal/massdriver"
	"github.com/massdriver-cloud/rocketsled/internal/scm/dev"
	"github.com/massdriver-cloud/rocketsled/internal/scm/github"
	"github.com/massdriver-cloud/rocketsled/internal/scm/gitlab"
	"github.com/massdriver-cloud/rocketsled/internal/store/sqlite"
	"github.com/massdriver-cloud/rocketsled/web"
)

var version = "dev"

const usage = `rocketsled — a small PaaS on top of Massdriver

Usage:
  rocketsled serve  [-config rocketsled.yaml]   start the API and UI
  rocketsled check  [-config rocketsled.yaml]   validate config and Massdriver access
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
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	return config.Load(*path)
}

type app struct {
	cfg      *config.Config
	svc      *domain.Service
	platform *massdriver.Client
	store    *sqlite.Store
}

func build(cfg *config.Config, log *slog.Logger) (*app, error) {
	store, err := sqlite.Open(cfg.Database.Path, cfg.Server.SessionSecret)
	if err != nil {
		return nil, err
	}
	platform := massdriver.New(cfg.Massdriver.URL, cfg.Massdriver.OrganizationID, cfg.Massdriver.APIKey, nil)

	var scms []domain.SCM
	if p := cfg.Auth.Providers.GitHub; p != nil {
		scms = append(scms, github.New(p.ClientID, p.ClientSecret, p.BaseURL, nil))
	}
	if p := cfg.Auth.Providers.GitLab; p != nil {
		scms = append(scms, gitlab.New(p.ClientID, p.ClientSecret, p.BaseURL, nil))
	}
	if p := cfg.Auth.Providers.Dev; p != nil {
		log.Warn("dev sign-in stub enabled; anyone who can reach this server is signed in as " + p.Login)
		scms = append(scms, dev.New(p.Login, p.Repos, "", nil))
	}
	links := make([]domain.ProjectLink, 0, len(cfg.Massdriver.Projects))
	for _, p := range cfg.Massdriver.Projects {
		links = append(links, domain.ProjectLink{
			ID:                   p.ID,
			BaseEnvironment:      p.BaseEnvironment,
			AppComponent:         p.AppComponent,
			ImageTagParam:        p.ImageTagParam,
			ImageRepositoryParam: p.ImageRepositoryParam,
			ImageRepository:      p.ImageRepository,
			ImageRegistry:        p.ImageRegistry,
			CopySecrets:          p.CopySecrets,
			CopyRemoteReferences: p.CopyRemoteReferences,
		})
	}
	var builder domain.Builder
	if d := cfg.Builder.Docker; d != nil {
		builder = docker.New(d.Platform, d.LoginCommand)
	}
	svc, err := domain.NewService(domain.Options{
		Platform:      platform,
		Store:         store,
		SCMs:          scms,
		Projects:      links,
		BaseURL:       cfg.Server.BaseURL,
		WebhookSecret: cfg.Auth.Webhooks.Secret,
		Builder:       builder,
		Logger:        log,
	})
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	return &app{cfg: cfg, svc: svc, platform: platform, store: store}, nil
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

	if org, err := a.platform.Ping(ctx); err != nil {
		log.Warn("massdriver check failed; continuing", "err", err)
	} else {
		log.Info("massdriver connected", "organization", org, "projects", len(cfg.Massdriver.Projects))
	}

	ui := web.Dist()
	if ui == nil {
		log.Warn("UI not built; serving API only (run `make ui`)")
	}
	handler := httpapi.New(httpapi.Options{
		Service:       a.svc,
		Sessions:      auth.New(cfg.Server.SessionSecret, cfg.Server.SessionTTL, strings.HasPrefix(cfg.Server.BaseURL, "https://")),
		BaseURL:       cfg.Server.BaseURL,
		WebhookSecret: cfg.Auth.Webhooks.Secret,
		UI:            ui,
		Logger:        log,
	})
	srv := &http.Server{
		Addr:              cfg.Server.Addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
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
	org, err := a.platform.Ping(ctx)
	if err != nil {
		return fmt.Errorf("massdriver: %w", err)
	}
	fmt.Printf("organization  %s (%s)\n", org, cfg.Massdriver.OrganizationID)
	fmt.Printf("providers     %v\n", a.svc.Providers())
	var failed bool
	for _, link := range a.svc.ListProjectLinks() {
		lp, err := a.svc.GetProject(ctx, link.ID)
		if err != nil {
			failed = true
			fmt.Printf("project       %-20s ERROR %v\n", link.ID, err)
			continue
		}
		fmt.Printf("project       %-20s base=%s app=%s components=%d\n", link.ID, link.BaseEnvironment, lp.AppComponent, len(lp.Project.Components))
		for _, c := range lp.Project.Components {
			names := make([]string, 0, len(c.Bundle.Presets))
			for _, p := range c.Bundle.Presets {
				names = append(names, p.Name)
			}
			fmt.Printf("  %-18s %s@%s presets=%v\n", c.Slug, c.Bundle.Name, c.Bundle.Version, names)
		}
	}
	if failed {
		return errors.New("one or more projects failed to load")
	}
	return nil
}

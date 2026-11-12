// Command vandyke serves the modern vanDyke misspelling ledger.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"vandyke.thor.edu/vandyke/internal/admin"
	"vandyke.thor.edu/vandyke/internal/config"
	"vandyke.thor.edu/vandyke/internal/httpapi"
	"vandyke.thor.edu/vandyke/internal/httpx"
	"vandyke.thor.edu/vandyke/internal/media"
	"vandyke.thor.edu/vandyke/internal/metrics"
	"vandyke.thor.edu/vandyke/internal/ratelimit"
	"vandyke.thor.edu/vandyke/internal/settings"
	"vandyke.thor.edu/vandyke/internal/store"
	"vandyke.thor.edu/vandyke/internal/web"
	"vandyke.thor.edu/vandyke/internal/webhook"
)

// Version is the application version. It is rewritten by semantic-release on
// every published release.
var Version = "1.11.1"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "fatal:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	st, err := store.Open(ctx, cfg.DBPath)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer func() { _ = st.Close() }()

	if err := media.Backfill(ctx, st, logger); err != nil {
		logger.Warn("backfill photo derivatives", "error", err)
	}

	limiter := ratelimit.New(cfg.AddRateLimitPerHour, cfg.AddRateLimitBurst)
	settingsStore, err := settings.New(ctx, st, cfg.Umami, cfg.Webhook)
	if err != nil {
		return fmt.Errorf("load settings: %w", err)
	}
	registry := metrics.New(metrics.Options{
		Version: Version,
		DBPath:  cfg.DBPath,
		Logger:  logger,
		Snapshot: func(ctx context.Context) (metrics.DBSnapshot, error) {
			m, err := st.MetricsSnapshot(ctx)
			if err != nil {
				return metrics.DBSnapshot{}, err
			}
			return metrics.DBSnapshot{
				Total:             m.Total,
				Live:              m.Live,
				Trashed:           m.Trashed,
				WithPhoto:         m.WithPhoto,
				PendingDeliveries: m.PendingDeliveries,
			}, nil
		},
	})

	// One delivery engine is shared by the public UI, the JSON API and the
	// admin pages. It starts before the listener and stops before the store.
	engine := webhook.NewEngine(st, settingsStore.CurrentWebhook, cfg.PublicBaseURL, registry, logger)
	engine.Start()
	defer engine.Close()
	registry.SetQueueDepth(engine.QueueDepth)

	api := httpapi.New(st, limiter, cfg.TrustProxy, cfg.PublicBaseURL, engine, registry, cfg.MetricsToken, logger)
	ui, err := web.New(st, limiter, settingsStore.Current, engine, cfg.TrustProxy, cfg.PublicBaseURL, registry, logger)
	if err != nil {
		return err
	}
	adminUI, err := admin.New(ctx, st, settingsStore, cfg, engine, logger)
	if err != nil {
		return fmt.Errorf("admin: %w", err)
	}

	mux := http.NewServeMux()
	api.Register(mux)
	ui.Register(mux)
	adminUI.Register(mux)

	handler := httpx.SecurityHeadersFunc(func(*http.Request) string {
		return cspFor(settingsStore.Current())
	})(
		httpx.RequestLogger(logger, cfg.TrustProxy)(
			metrics.Middleware(registry)(
				httpx.Recover(logger)(mux),
			),
		),
	)

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		logger.Info("vanDyke listening",
			"version", Version,
			"addr", srv.Addr,
			"db", cfg.DBPath,
			"umami", settingsStore.Current().Enabled(),
			"webhook", settingsStore.CurrentWebhook().Enabled(),
			"metrics_token", cfg.MetricsToken != "",
			"admin_setup_token", cfg.AdminSetupToken != "",
			"oidc", cfg.OIDC.Enabled(),
			"public_base_url", cfg.PublicBaseURL,
			"rate_limit_per_hour", cfg.AddRateLimitPerHour,
		)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server error", "error", err)
			stop()
		}
	}()

	<-ctx.Done()
	logger.Info("shutting down")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}

// cspFor builds a Content-Security-Policy that permits the configured
// self-hosted Umami origin when analytics is enabled.
func cspFor(u config.Umami) string {
	scriptSrc := "'self'"
	connectSrc := "'self'"
	if origin := u.Origin(); origin != "" {
		scriptSrc += " " + origin
		connectSrc += " " + origin
	}
	return fmt.Sprintf(
		"default-src 'self'; script-src %s; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; font-src 'self'; connect-src %s; base-uri 'self'; form-action 'self'; frame-ancestors 'none'",
		scriptSrc, connectSrc,
	)
}

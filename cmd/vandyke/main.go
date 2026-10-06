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

	"vandyke.thor.edu/vandyke/internal/config"
	"vandyke.thor.edu/vandyke/internal/httpapi"
	"vandyke.thor.edu/vandyke/internal/httpx"
	"vandyke.thor.edu/vandyke/internal/ratelimit"
	"vandyke.thor.edu/vandyke/internal/store"
	"vandyke.thor.edu/vandyke/internal/web"
)

// Version is the application version. It is rewritten by semantic-release on
// every published release.
var Version = "1.0.0"

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

	limiter := ratelimit.New(cfg.AddRateLimitPerHour, cfg.AddRateLimitBurst)
	api := httpapi.New(st, limiter, cfg.TrustProxy, logger)
	ui, err := web.New(st, limiter, cfg.Umami, cfg.TrustProxy, logger)
	if err != nil {
		return err
	}

	mux := http.NewServeMux()
	api.Register(mux)
	ui.Register(mux)

	handler := httpx.SecurityHeaders(cspFor(cfg))(
		httpx.RequestLogger(logger)(
			httpx.Recover(logger)(mux),
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
			"umami", cfg.Umami.Enabled(),
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
func cspFor(cfg config.Config) string {
	scriptSrc := "'self'"
	connectSrc := "'self'"
	if origin := cfg.Umami.Origin(); origin != "" {
		scriptSrc += " " + origin
		connectSrc += " " + origin
	}
	return fmt.Sprintf(
		"default-src 'self'; script-src %s; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self'; connect-src %s; base-uri 'self'; form-action 'self'; frame-ancestors 'none'",
		scriptSrc, connectSrc,
	)
}

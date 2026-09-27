package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/api"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/audit"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/auth"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/config"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/database"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/monitoring"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/operations"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/repository"
)

func main() {
	devboxLogs := operations.NewRingLogSource("devbox", 2000)
	logger := slog.New(operations.NewSlogCaptureHandler(slog.NewJSONHandler(os.Stdout, nil), devboxLogs))
	cfg, err := config.Load()
	if err != nil {
		logger.Error("configuration error", "error", err)
		os.Exit(1)
	}
	db, err := database.Open(cfg.DatabasePath)
	if err != nil {
		logger.Error("database open failed", "error", err)
		os.Exit(1)
	}
	defer db.Close()
	if err := database.Migrate(context.Background(), db, cfg.MigrationsDir); err != nil {
		logger.Error("database migration failed", "error", err)
		os.Exit(1)
	}

	users := repository.NewSQLiteUsers(db)
	sessions := repository.NewSQLiteSessions(db)
	jobsRepo := repository.NewSQLiteJobs(db)
	auditRepo := repository.NewSQLiteAudit(db)
	authService := auth.NewService(users, sessions, cfg.SessionTTL)
	if err := authService.BootstrapAdmin(context.Background(), cfg.BootstrapAdminUsername, cfg.BootstrapAdminPassword); err != nil {
		logger.Error("bootstrap admin failed", "error", err)
		os.Exit(1)
	}
	auditService := audit.NewService(auditRepo)

	logRegistry := operations.NewRegistry()
	logSources := []operations.LogSource{
		devboxLogs,
		operations.NewSQLLogSource("project", db, operations.LogModeProject),
		operations.NewSQLLogSource("deployment", db, operations.LogModeDeployment),
		operations.NewSQLLogSource("job", db, operations.LogModeJob),
	}
	for _, source := range logSources {
		if err := logRegistry.Register(source); err != nil {
			logger.Error("register log source failed", "source", source.Name(), "error", err)
			os.Exit(1)
		}
	}

	modules := []api.Module{
		monitoring.NewModule(monitoring.NewCollector()),
		operations.NewModule(logRegistry),
	}
	handler := api.New(api.Dependencies{
		DB:           db,
		Auth:         authService,
		Audit:        auditService,
		Jobs:         jobsRepo,
		Version:      cfg.AppVersion,
		CookieSecure: cfg.CookieSecure,
		Modules:      modules,
	})

	server := &http.Server{Addr: cfg.HTTPAddr, Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second}
	errCh := make(chan error, 1)
	go func() {
		logger.Info("devbox backend listening", "addr", cfg.HTTPAddr, "version", cfg.AppVersion)
		errCh <- server.ListenAndServe()
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	select {
	case sig := <-sigCh:
		logger.Info("shutdown requested", "signal", sig.String())
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server failed", "error", err)
			os.Exit(1)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		logger.Error("graceful shutdown failed", "error", err)
		os.Exit(1)
	}
}

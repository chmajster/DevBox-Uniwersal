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
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/jobs"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/projects"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/repository"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/runtimes"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/secrets"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
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

	var secretStore secrets.SecretStore
	if cfg.MasterKeyBase64 != "" {
		cipher, cipherErr := secrets.NewAESGCMFromBase64(cfg.MasterKeyBase64)
		if cipherErr != nil {
			logger.Error("secret store initialization failed", "error", cipherErr)
			os.Exit(1)
		}
		secretStore = secrets.NewSQLiteStore(db, cipher)
	}

	jobRunner := jobs.NewRunner(jobsRepo)
	gitClient := projects.NewGitClient(secretStore)
	projectRepo := projects.NewRepository(db)
	runtimeRegistry := runtimes.NewRegistry()
	projectService := projects.NewService(projectRepo, gitClient, jobRunner, secretStore, cfg.ProjectsRoot)
	for _, handler := range []jobs.Handler{
		projects.NewGitJobHandler(projects.JobClone, projectRepo, gitClient, jobRunner),
		projects.NewGitJobHandler(projects.JobFetch, projectRepo, gitClient, jobRunner),
		projects.NewGitJobHandler(projects.JobPull, projectRepo, gitClient, jobRunner),
		projects.NewGitJobHandler(projects.JobCheckout, projectRepo, gitClient, jobRunner),
		projects.NewDeploymentHandler(projectRepo, gitClient, runtimeRegistry, jobRunner),
	} {
		if err := jobRunner.Register(handler); err != nil {
			logger.Error("job handler registration failed", "type", handler.Type(), "error", err)
			os.Exit(1)
		}
	}
	workerCtx, stopWorkers := context.WithCancel(context.Background())
	defer stopWorkers()
	if err := jobRunner.Start(workerCtx); err != nil {
		logger.Error("job runner start failed", "error", err)
		os.Exit(1)
	}

	projectModule := projects.NewModule(projectService, auditService)
	handler := api.New(api.Dependencies{DB: db, Auth: authService, Audit: auditService, Jobs: jobsRepo, Version: cfg.AppVersion, CookieSecure: cfg.CookieSecure, Modules: []api.Module{projectModule}})

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
	stopWorkers()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		logger.Error("graceful shutdown failed", "error", err)
		os.Exit(1)
	}
}

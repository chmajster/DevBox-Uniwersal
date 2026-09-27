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
	controldb "github.com/chmajster/DevBox-Uniwersal/backend/internal/database"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/databases"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/monitoring"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/operations"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/proxy"
	dockermodule "github.com/chmajster/DevBox-Uniwersal/backend/internal/docker"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/repository"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/runtimes"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/secrets"
)

func main() {
	devboxLogs := operations.NewRingLogSource("devbox", 2000)
	logger := slog.New(operations.NewSlogCaptureHandler(slog.NewJSONHandler(os.Stdout, nil), devboxLogs))
	cfg, err := config.Load()
	if err != nil {
		logger.Error("configuration error", "error", err)
		os.Exit(1)
	}
	db, err := controldb.Open(cfg.DatabasePath)
	if err != nil {
		logger.Error("database open failed", "error", err)
		os.Exit(1)
	}
	defer db.Close()
	if err := controldb.Migrate(context.Background(), db, cfg.MigrationsDir); err != nil {
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
		cipher, err := secrets.NewAESGCMFromBase64(cfg.MasterKeyBase64)
		if err != nil {
			logger.Error("secret store initialization failed", "error", err)
			os.Exit(1)
		}
		secretStore = secrets.NewSQLiteStore(db, cipher)
	} else {
		logger.Warn("DEVBOX_MASTER_KEY is not configured; project database credentials cannot be provisioned")
	}
	runtimeModule := runtimes.NewModule(
		runtimes.NewDefaultRegistry(),
		runtimes.NewSQLiteProjectResolver(db),
		secretStore,
	)

	dockerProvider := dockermodule.NewCLIProvider()
	dockerService := dockermodule.NewService(dockerProvider, auditService, cfg.ProjectsRoot)
	dockerModule := dockermodule.NewModule(dockerService)

	mysqlProvider := databases.NewMySQLProvider(databases.MySQLConfig{
		Host:            cfg.MySQLHost,
		Port:            cfg.MySQLPort,
		AdminUser:       cfg.MySQLAdminUser,
		AdminPassword:   cfg.MySQLAdminPassword,
		ApplicationHost: cfg.MySQLAppHost,
		MySQLBinary:     cfg.MySQLBinary,
		DumpBinary:      cfg.MySQLDumpBinary,
	}, secretStore)
	databaseRepo := databases.NewRepository(db)
	databaseJobs := databases.NewSQLiteJobRunner(db)
	phpMyAdmin := databases.NewPHPMyAdminManager(databases.PHPMyAdminConfig{
		DockerBinary: cfg.PHPMyAdminDockerBinary,
		Image:        cfg.PHPMyAdminImage,
		Container:    cfg.PHPMyAdminContainer,
		HostPort:     cfg.PHPMyAdminHostPort,
		MySQLHost:    cfg.MySQLHost,
		MySQLPort:    cfg.MySQLPort,
	})
	databaseService, err := databases.NewService(databaseRepo, mysqlProvider, secretStore, databaseJobs, auditService, phpMyAdmin, cfg.MySQLBackupDir)
	if err != nil {
		logger.Error("database module initialization failed", "error", err)
		os.Exit(1)
	}
	databaseModule := databases.NewModule(databaseService)

	networkRepo := proxy.NewSQLiteRepository(db)
	portManager := proxy.NewPortManager(db, cfg.PortRangeStart, cfg.PortRangeEnd)
	nginxProvider := proxy.NewNginxProvider(proxy.NginxOptions{
		Binary:         cfg.NginxBinary,
		SitesAvailable: cfg.NginxSitesAvailable,
		SitesEnabled:   cfg.NginxSitesEnabled,
	})
	hostsManager := proxy.NewFileHostsManager(proxy.DefaultHostsPath(cfg.HostsFile))
	healthChecker := proxy.NewHealthChecker(networkRepo)
	networkService := proxy.NewService(networkRepo, nginxProvider, hostsManager, healthChecker, cfg.HealthTimeout)
	networkModule := proxy.NewModule(networkService, portManager, healthChecker, nginxProvider, auditService, cfg.HealthTimeout)

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
		runtimeModule,
		dockerModule,
		databaseModule,
		networkModule,
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

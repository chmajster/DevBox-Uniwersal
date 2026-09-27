package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/api"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/audit"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/auth"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/config"
	controldb "github.com/chmajster/DevBox-Uniwersal/backend/internal/database"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/databases"
	dockermodule "github.com/chmajster/DevBox-Uniwersal/backend/internal/docker"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/jobs"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/monitoring"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/operations"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/projects"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/proxy"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/repository"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/runtimes"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/secrets"
	devsystem "github.com/chmajster/DevBox-Uniwersal/backend/internal/system"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/webui"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	command := "serve"
	if len(args) > 0 {
		command = strings.ToLower(strings.TrimSpace(args[0]))
	}
	switch command {
	case "serve":
		if err := serve(); err != nil {
			fmt.Fprintln(os.Stderr, "[FAIL]", err)
			return 1
		}
		return 0
	case "status":
		return status()
	case "doctor":
		return doctor()
	case "help", "--help", "-h":
		printUsage()
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n", command)
		printUsage()
		return 2
	}
}

func serve() error {
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
	runtimeRegistry := runtimes.NewDefaultRegistry()
	runtimeModule := runtimes.NewModule(
		runtimeRegistry,
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

	jobRunner := jobs.NewRunner(jobsRepo)
	gitClient := projects.NewGitClient(secretStore)
	projectRepo := projects.NewRepository(db)
	projectService := projects.NewService(projectRepo, gitClient, jobRunner, secretStore, cfg.ProjectsRoot)
	for _, jobHandler := range []jobs.Handler{
		projects.NewGitJobHandler(projects.JobClone, projectRepo, gitClient, jobRunner),
		projects.NewGitJobHandler(projects.JobFetch, projectRepo, gitClient, jobRunner),
		projects.NewGitJobHandler(projects.JobPull, projectRepo, gitClient, jobRunner),
		projects.NewGitJobHandler(projects.JobCheckout, projectRepo, gitClient, jobRunner),
		projects.NewDeploymentHandler(projectRepo, gitClient, runtimeRegistry, jobRunner),
	} {
		if err := jobRunner.Register(jobHandler); err != nil {
			logger.Error("job handler registration failed", "type", jobHandler.Type(), "error", err)
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
		projectModule,
		dockerModule,
		databaseModule,
		networkModule,
		monitoring.NewModule(monitoring.NewCollector()),
		operations.NewModule(logRegistry),
	}

	apiHandler := api.New(api.Dependencies{
		DB:           db,
		Auth:         authService,
		Audit:        auditService,
		Jobs:         jobsRepo,
		Version:      cfg.AppVersion,
		CookieSecure: cfg.CookieSecure,
		Modules:      modules,
	})
	handler := webui.Wrap(apiHandler, cfg.FrontendDir)

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
	return nil
}

func status() int {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "[FAIL] configuration:", err)
		return 1
	}
	platform := devsystem.DetectPlatform()
	fmt.Printf("[INFO] platform: %s/%s", platform.OS, platform.Arch)
	if platform.DistroName != "" {
		fmt.Printf(" %s", platform.DistroName)
	}
	if platform.WSL {
		fmt.Printf(" WSL%d", platform.WSLVersion)
	}
	fmt.Println()
	fmt.Printf("[INFO] http: %s\n", cfg.HTTPAddr)
	fmt.Printf("[INFO] database: %s\n", cfg.DatabasePath)
	for _, component := range devsystem.DetectComponents(context.Background()) {
		marker := "WARN"
		if component.Installed && component.State == "available" {
			marker = " OK "
		}
		version := component.Version
		if version == "" {
			version = component.State
		}
		fmt.Printf("[%s] %-9s %s\n", marker, component.Name, version)
	}
	return 0
}

func doctor() int {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "[FAIL] configuration:", err)
		return 1
	}
	var dbErr error
	var dbClose func()
	var dbPathExists bool
	if _, statErr := os.Stat(cfg.DatabasePath); statErr == nil {
		dbPathExists = true
	} else {
		dbErr = statErr
	}
	var db *sql.DB
	if dbPathExists {
		db, err = controldb.Open(cfg.DatabasePath)
		if err != nil {
			dbErr = err
		} else {
			dbClose = func() { _ = db.Close() }
		}
	}
	if dbClose != nil {
		defer dbClose()
	}
	report := devsystem.RunDoctor(context.Background(), devsystem.DoctorOptions{
		DB:            db,
		DatabasePath:  cfg.DatabasePath,
		MigrationsDir: cfg.MigrationsDir,
		DataDir:       devsystem.DatabaseDataDir(cfg.DatabasePath),
		HTTPAddr:      cfg.HTTPAddr,
		ServiceName:   "devbox",
	})
	if dbErr != nil && len(report.Checks) > 0 {
		report.Checks[0].Message = dbErr.Error()
	}
	for _, check := range report.Checks {
		marker := strings.ToUpper(string(check.Status))
		if check.Status == devsystem.CheckOK {
			marker = " OK "
		}
		fmt.Printf("[%s] %-16s %s\n", marker, check.Name, check.Message)
	}
	if !report.Healthy {
		return 1
	}
	return 0
}

func printUsage() {
	fmt.Println("usage: devbox [serve|status|doctor|help]")
}

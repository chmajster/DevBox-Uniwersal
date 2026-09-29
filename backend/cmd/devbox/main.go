package main

import (
	"bufio"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/api"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/apphealth"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/audit"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/auth"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/backups"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/config"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/credentials"
	controldb "github.com/chmajster/DevBox-Uniwersal/backend/internal/database"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/databases"
	dockermodule "github.com/chmajster/DevBox-Uniwersal/backend/internal/docker"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/jobs"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/monitoring"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/operations"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/plugins"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/projects"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/proxy"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/repository"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/runtimes"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/scriptapps"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/secrets"
	devsystem "github.com/chmajster/DevBox-Uniwersal/backend/internal/system"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/updater"
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
	restoreResult, err := backups.ApplyPendingRestore(backups.ApplyRestoreOptions{
		DatabasePath:        cfg.DatabasePath,
		BackupDir:           cfg.ControlPlaneBackupDir,
		ProjectsRoot:        cfg.ProjectsRoot,
		NginxSitesAvailable: cfg.NginxSitesAvailable,
		NginxSitesEnabled:   cfg.NginxSitesEnabled,
	})
	if err != nil {
		logger.Error("pending control-plane restore failed", "error", err)
		os.Exit(1)
	}
	if restoreResult.Applied {
		logger.Info("pending control-plane restore applied", "backup_id", restoreResult.BackupID, "rollback_path", restoreResult.RollbackPath)
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
	jobRunner := jobs.NewRunner(jobsRepo)
	auditRepo := repository.NewSQLiteAudit(db)
	authService := auth.NewService(users, sessions, cfg.SessionTTL)
	if cfg.AuthDisabled && cfg.BootstrapAdminUsername == "" {
		cfg.BootstrapAdminUsername = "admin"
	}
	if err := authService.BootstrapAdmin(context.Background(), cfg.BootstrapAdminUsername, cfg.BootstrapAdminPassword); err != nil {
		logger.Error("bootstrap admin failed", "error", err)
		os.Exit(1)
	}
	auditService := audit.NewService(auditRepo)

	backupRepo := backups.NewRepository(db)
	backupManager := backups.NewManager(db, backups.ManagerOptions{
		DatabasePath:        cfg.DatabasePath,
		BackupDir:           cfg.ControlPlaneBackupDir,
		ProjectsRoot:        cfg.ProjectsRoot,
		NginxSitesAvailable: cfg.NginxSitesAvailable,
		NginxSitesEnabled:   cfg.NginxSitesEnabled,
		AppVersion:          cfg.AppVersion,
	})
	backupService := backups.NewService(backupRepo, jobRunner, backupManager, cfg.ControlPlaneBackupDir)
	for _, handler := range backupService.Handlers() {
		if err := jobRunner.Register(handler); err != nil {
			logger.Error("backup job handler registration failed", "type", handler.Type(), "error", err)
			os.Exit(1)
		}
	}
	backupModule := backups.NewModule(backupService, auditService)

	masterKey, err := secrets.ResolveMasterKey(context.Background(), db, cfg.MasterKeyBase64, filepath.Join(filepath.Dir(cfg.DatabasePath), "master.key"))
	if err != nil {
		logger.Error("secret store initialization failed", "error", err)
		os.Exit(1)
	}
	cipher, err := secrets.NewAESGCMFromBase64(masterKey)
	if err != nil {
		logger.Error("secret store initialization failed", "error", err)
		os.Exit(1)
	}
	var secretStore secrets.SecretStore = secrets.NewSQLiteStore(db, cipher)
	credentialRepo := credentials.NewRepository(db)
	credentialService := credentials.NewService(credentialRepo, secretStore)
	credentialModule := credentials.NewModule(credentialService, auditService)

	runtimeRegistry := runtimes.NewDefaultRegistry()
	runtimeProjectResolver := runtimes.NewSQLiteProjectResolver(db)
	runtimeEnvironmentResolver := runtimes.NewEnvironmentResolver(runtimeProjectResolver, secretStore)
	runtimeModule := runtimes.NewModule(
		runtimeRegistry,
		runtimeProjectResolver,
		secretStore,
	)

	dockerProvider := dockermodule.NewCLIProvider()
	dockerService := dockermodule.NewService(dockerProvider, auditService, cfg.ProjectsRoot)
	dockerModule := dockermodule.NewModule(dockerService)

	databaseRepo := databases.NewRepository(db)
	mysqlConfig := databases.MySQLConfig{
		Host:                    cfg.MySQLHost,
		Port:                    cfg.MySQLPort,
		AdminUser:               cfg.MySQLAdminUser,
		AdminPassword:           cfg.MySQLAdminPassword,
		ApplicationHost:         cfg.MySQLAppHost,
		ApplicationEndpointHost: cfg.MySQLHost,
		ApplicationEndpointPort: cfg.MySQLPort,
		MySQLBinary:             cfg.MySQLBinary,
		DumpBinary:              cfg.MySQLDumpBinary,
	}
	var managedMySQL *databases.ManagedMySQLManager
	phpMySQLHost := cfg.MySQLHost
	phpMySQLPort := cfg.MySQLPort
	phpMySQLNetwork := ""
	if cfg.ManagedMySQLEnabled {
		managedMySQL = databases.NewManagedMySQLManager(dockerProvider, secretStore, databases.ManagedMySQLConfig{
			Image:               cfg.ManagedMySQLImage,
			Container:           cfg.ManagedMySQLContainer,
			Network:             cfg.ManagedMySQLNetwork,
			Volume:              cfg.ManagedMySQLVolume,
			AdminHost:           "127.0.0.1",
			AdminPort:           cfg.MySQLPort,
			InitialRootPassword: cfg.MySQLAdminPassword,
		})
		adminScope, adminSecret := managedMySQL.AdminSecretRef()
		adminEndpoint := managedMySQL.AdminEndpoint()
		applicationEndpoint := managedMySQL.ApplicationEndpoint()
		mysqlConfig.Host = adminEndpoint.Host
		mysqlConfig.Port = adminEndpoint.Port
		mysqlConfig.AdminUser = "root"
		mysqlConfig.AdminPassword = ""
		mysqlConfig.AdminSecretScope = adminScope
		mysqlConfig.AdminSecretRef = adminSecret
		mysqlConfig.ApplicationEndpointHost = applicationEndpoint.Host
		mysqlConfig.ApplicationEndpointPort = applicationEndpoint.Port
		phpMySQLHost = applicationEndpoint.Host
		phpMySQLPort = applicationEndpoint.Port
		phpMySQLNetwork = managedMySQL.Network()

		reconcileCtx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		if err := managedMySQL.Ensure(reconcileCtx); err != nil {
			logger.Warn("managed MySQL reconciliation failed; database operations will retry on demand", "error", err)
		}
		cancel()
	}
	mysqlProvider := databases.NewMySQLProvider(mysqlConfig, secretStore)
	phpMyAdmin := databases.NewPHPMyAdminManager(databases.PHPMyAdminConfig{
		DockerBinary: cfg.PHPMyAdminDockerBinary,
		Image:        cfg.PHPMyAdminImage,
		Container:    cfg.PHPMyAdminContainer,
		HostPort:     cfg.PHPMyAdminHostPort,
		MySQLHost:    phpMySQLHost,
		MySQLPort:    phpMySQLPort,
		Network:      phpMySQLNetwork,
	})
	phpMyAdminReconcileCtx, phpMyAdminReconcileCancel := context.WithTimeout(context.Background(), 60*time.Second)
	if _, err := phpMyAdmin.Reconcile(phpMyAdminReconcileCtx); err != nil {
		logger.Warn("phpMyAdmin reconciliation failed; it will retry when opened", "error", err)
	}
	phpMyAdminReconcileCancel()
	databaseOptions := []databases.ServiceOption{databases.WithComposeDatabaseProvider(dockerProvider)}
	if managedMySQL != nil {
		databaseOptions = append(databaseOptions, databases.WithManagedMySQL(managedMySQL))
	}
	databaseService, err := databases.NewService(databaseRepo, mysqlProvider, secretStore, jobRunner, auditService, phpMyAdmin, cfg.MySQLBackupDir, databaseOptions...)
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
		HelperBinary:   cfg.NginxHelperBinary,
		SudoBinary:     cfg.SudoBinary,
	})
	hostsManager := proxy.NewFileHostsManager(proxy.DefaultHostsPath(cfg.HostsFile))
	healthChecker := proxy.NewHealthChecker(networkRepo)
	networkService := proxy.NewService(networkRepo, nginxProvider, hostsManager, healthChecker, cfg.HealthTimeout)
	networkModule := proxy.NewModule(networkService, portManager, healthChecker, nginxProvider, auditService, cfg.HealthTimeout)
	appHealthService := apphealth.NewService(db, cfg.HealthMonitorInterval, cfg.HealthTimeout, cfg.HealthHistoryRetentionDays)
	appHealthModule := apphealth.NewModule(appHealthService, auditService)

	gitClient := projects.NewGitClient(secretStore)
	projectRepo := projects.NewRepository(db)
	projectService := projects.NewService(projectRepo, gitClient, jobRunner, secretStore, cfg.ProjectsRoot, cfg.DirectoryBrowseRoots...)
	for _, jobHandler := range []jobs.Handler{
		projects.NewGitJobHandler(projects.JobClone, projectRepo, gitClient, jobRunner),
		projects.NewGitJobHandler(projects.JobFetch, projectRepo, gitClient, jobRunner),
		projects.NewGitJobHandler(projects.JobPull, projectRepo, gitClient, jobRunner),
		projects.NewGitJobHandler(projects.JobCheckout, projectRepo, gitClient, jobRunner),
		projects.NewDeploymentHandler(projectRepo, gitClient, runtimeRegistry, jobRunner, projects.DeploymentIntegrations{
			Ports:       portManager,
			Routes:      networkService,
			Compose:     dockerProvider,
			Managed:     dockerProvider,
			Database:    databaseService,
			Environment: runtimeEnvironmentResolver,
		}),
	} {
		if err := jobRunner.Register(jobHandler); err != nil {
			logger.Error("job handler registration failed", "type", jobHandler.Type(), "error", err)
			os.Exit(1)
		}
	}
	pluginOptions := []plugins.ServiceOption{plugins.WithJobRunner(jobRunner)}
	if cfg.ManagedMySQLEnabled {
		pluginOptions = append(pluginOptions, plugins.WithReservedHostPort(
			cfg.MySQLPort,
			fmt.Sprintf("zarządzany MySQL DevBox (%s)", cfg.ManagedMySQLContainer),
		))
	}
	pluginService := plugins.NewService(cfg.NginxHelperBinary, cfg.SudoBinary, pluginOptions...)
	for _, handler := range pluginService.Handlers() {
		if err := jobRunner.Register(handler); err != nil {
			logger.Error("plugin job handler registration failed", "type", handler.Type(), "error", err)
			os.Exit(1)
		}
	}

	workerCtx, stopWorkers := context.WithCancel(context.Background())
	defer stopWorkers()
	if err := jobRunner.Start(workerCtx); err != nil {
		logger.Error("job runner start failed", "error", err)
		os.Exit(1)
	}
	go appHealthService.Run(workerCtx)
	reconciledJobs, err := projectService.ReconcileAutoStart(context.Background())
	if err != nil {
		logger.Error("desired-state reconciliation failed", "error", err)
		os.Exit(1)
	}
	if len(reconciledJobs) > 0 {
		logger.Info("desired-state reconciliation queued", "jobs", len(reconciledJobs))
	}
	projectModule := projects.NewModule(projectService, auditService)
	updaterModule := updater.NewModule(updater.NewService(cfg.AppVersion, cfg.NginxHelperBinary, cfg.SudoBinary), auditService)
	pluginModule := plugins.NewModule(pluginService, auditService)

	scriptAppRepo := scriptapps.NewRepository(db)
	scriptAppService := scriptapps.NewService(scriptAppRepo, jobRunner)
	for _, jobType := range []string{scriptapps.JobInstall, scriptapps.JobUpdate, scriptapps.JobUninstall, scriptapps.JobStart, scriptapps.JobStop, scriptapps.JobRestart} {
		if err := jobRunner.Register(scriptapps.NewHandler(jobType, scriptAppRepo, jobRunner)); err != nil {
			logger.Error("script app job handler registration failed", "type", jobType, "error", err)
			os.Exit(1)
		}
	}
	scriptAppModule := scriptapps.NewModule(scriptAppService, auditService)

	logRegistry := operations.NewRegistry()
	logSources := []operations.LogSource{
		devboxLogs,
		operations.NewSQLLogSource("project", db, operations.LogModeProject),
		operations.NewSQLLogSource("deployment", db, operations.LogModeDeployment),
		operations.NewSQLLogSource("job", db, operations.LogModeJob),
		dockermodule.NewOperationsLogSource(dockerProvider),
	}
	if cfg.NginxLogPath != "" {
		logSources = append(logSources, operations.NewFileLogSource("nginx", cfg.NginxLogPath))
	}
	if cfg.MySQLLogPath != "" {
		logSources = append(logSources, operations.NewFileLogSource("mysql", cfg.MySQLLogPath))
	}
	for _, source := range logSources {
		if err := logRegistry.Register(source); err != nil {
			logger.Error("register log source failed", "source", source.Name(), "error", err)
			os.Exit(1)
		}
	}
	if err := logRegistry.Register(operations.NewAggregateLogSource(logRegistry)); err != nil {
		logger.Error("register aggregate log source failed", "error", err)
		os.Exit(1)
	}
	modules := []api.Module{
		credentialModule,
		updaterModule,
		pluginModule,
		runtimeModule,
		projectModule,
		scriptAppModule,
		dockerModule,
		databaseModule,
		networkModule,
		appHealthModule,
		backupModule,
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
		AuthDisabled: cfg.AuthDisabled,
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

func loadInstalledEnvironment() {
	path := strings.TrimSpace(os.Getenv("DEVBOX_ENV_FILE"))
	if path == "" {
		path = "/etc/devbox/devbox.env"
	}
	file, err := os.Open(path)
	if err != nil {
		return
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		if !strings.HasPrefix(key, "DEVBOX_") || os.Getenv(key) != "" {
			continue
		}
		_ = os.Setenv(key, strings.TrimSpace(value))
	}
}

func status() int {
	loadInstalledEnvironment()
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
	loadInstalledEnvironment()
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

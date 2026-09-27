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
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/database"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/repository"
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
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("configuration error: %w", err)
	}
	db, err := database.Open(cfg.DatabasePath)
	if err != nil {
		return fmt.Errorf("database open failed: %w", err)
	}
	defer db.Close()
	if err := database.Migrate(context.Background(), db, cfg.MigrationsDir); err != nil {
		return fmt.Errorf("database migration failed: %w", err)
	}

	users := repository.NewSQLiteUsers(db)
	sessions := repository.NewSQLiteSessions(db)
	jobsRepo := repository.NewSQLiteJobs(db)
	auditRepo := repository.NewSQLiteAudit(db)
	authService := auth.NewService(users, sessions, cfg.SessionTTL)
	if err := authService.BootstrapAdmin(context.Background(), cfg.BootstrapAdminUsername, cfg.BootstrapAdminPassword); err != nil {
		return fmt.Errorf("bootstrap admin failed: %w", err)
	}
	auditService := audit.NewService(auditRepo)
	apiHandler := api.New(api.Dependencies{DB: db, Auth: authService, Audit: auditService, Jobs: jobsRepo, Version: cfg.AppVersion, CookieSecure: cfg.CookieSecure})
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
			return fmt.Errorf("server failed: %w", err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		return fmt.Errorf("graceful shutdown failed: %w", err)
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
		db, err = database.Open(cfg.DatabasePath)
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

package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	HTTPAddr               string
	DatabasePath           string
	MigrationsDir          string
	FrontendDir            string
	SessionTTL             time.Duration
	CookieSecure           bool
	BootstrapAdminUsername string
	BootstrapAdminPassword string
	MasterKeyBase64        string
	AppVersion             string
}

func Load() (Config, error) {
	ttl, err := time.ParseDuration(getEnv("DEVBOX_SESSION_TTL", "24h"))
	if err != nil {
		return Config{}, fmt.Errorf("parse DEVBOX_SESSION_TTL: %w", err)
	}
	if ttl <= 0 {
		return Config{}, fmt.Errorf("DEVBOX_SESSION_TTL must be greater than zero")
	}

	cookieSecure, err := strconv.ParseBool(getEnv("DEVBOX_COOKIE_SECURE", "false"))
	if err != nil {
		return Config{}, fmt.Errorf("parse DEVBOX_COOKIE_SECURE: %w", err)
	}

	cfg := Config{
		HTTPAddr:               getEnv("DEVBOX_HTTP_ADDR", "127.0.0.1:8787"),
		DatabasePath:           getEnv("DEVBOX_DATABASE_PATH", "./data/devbox.db"),
		MigrationsDir:          getEnv("DEVBOX_MIGRATIONS_DIR", "./migrations"),
		FrontendDir:            strings.TrimSpace(os.Getenv("DEVBOX_FRONTEND_DIR")),
		SessionTTL:             ttl,
		CookieSecure:           cookieSecure,
		BootstrapAdminUsername: strings.TrimSpace(os.Getenv("DEVBOX_BOOTSTRAP_ADMIN_USERNAME")),
		BootstrapAdminPassword: os.Getenv("DEVBOX_BOOTSTRAP_ADMIN_PASSWORD"),
		MasterKeyBase64:        strings.TrimSpace(os.Getenv("DEVBOX_MASTER_KEY")),
		AppVersion:             getEnv("DEVBOX_VERSION", "dev"),
	}

	if cfg.HTTPAddr == "" || cfg.DatabasePath == "" || cfg.MigrationsDir == "" {
		return Config{}, fmt.Errorf("HTTP address, database path and migrations directory are required")
	}
	if (cfg.BootstrapAdminUsername == "") != (cfg.BootstrapAdminPassword == "") {
		return Config{}, fmt.Errorf("bootstrap admin username and password must be configured together")
	}
	return cfg, nil
}

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

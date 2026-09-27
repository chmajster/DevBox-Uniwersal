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
	SessionTTL             time.Duration
	CookieSecure           bool
	BootstrapAdminUsername string
	BootstrapAdminPassword string
	MasterKeyBase64        string
	AppVersion             string
	PortRangeStart         int
	PortRangeEnd           int
	NginxBinary            string
	NginxSitesAvailable    string
	NginxSitesEnabled      string
	HostsFile              string
	HealthTimeout          time.Duration
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
	portStart, err := getEnvInt("DEVBOX_PORT_RANGE_START", 8000)
	if err != nil {
		return Config{}, err
	}
	portEnd, err := getEnvInt("DEVBOX_PORT_RANGE_END", 8999)
	if err != nil {
		return Config{}, err
	}
	if portStart < 1 || portEnd > 65535 || portStart > portEnd {
		return Config{}, fmt.Errorf("DEVBOX port range must be within 1..65535 and start <= end")
	}
	healthTimeout, err := time.ParseDuration(getEnv("DEVBOX_HEALTH_TIMEOUT", "5s"))
	if err != nil {
		return Config{}, fmt.Errorf("parse DEVBOX_HEALTH_TIMEOUT: %w", err)
	}
	if healthTimeout <= 0 || healthTimeout > 60*time.Second {
		return Config{}, fmt.Errorf("DEVBOX_HEALTH_TIMEOUT must be greater than zero and at most 60s")
	}

	cfg := Config{
		HTTPAddr:               getEnv("DEVBOX_HTTP_ADDR", "127.0.0.1:8787"),
		DatabasePath:           getEnv("DEVBOX_DATABASE_PATH", "./data/devbox.db"),
		MigrationsDir:          getEnv("DEVBOX_MIGRATIONS_DIR", "./migrations"),
		SessionTTL:             ttl,
		CookieSecure:           cookieSecure,
		BootstrapAdminUsername: strings.TrimSpace(os.Getenv("DEVBOX_BOOTSTRAP_ADMIN_USERNAME")),
		BootstrapAdminPassword: os.Getenv("DEVBOX_BOOTSTRAP_ADMIN_PASSWORD"),
		MasterKeyBase64:        strings.TrimSpace(os.Getenv("DEVBOX_MASTER_KEY")),
		AppVersion:             getEnv("DEVBOX_VERSION", "dev"),
		PortRangeStart:         portStart,
		PortRangeEnd:           portEnd,
		NginxBinary:            getEnv("DEVBOX_NGINX_BINARY", "nginx"),
		NginxSitesAvailable:    getEnv("DEVBOX_NGINX_SITES_AVAILABLE", "/etc/nginx/sites-available"),
		NginxSitesEnabled:      getEnv("DEVBOX_NGINX_SITES_ENABLED", "/etc/nginx/sites-enabled"),
		HostsFile:              strings.TrimSpace(os.Getenv("DEVBOX_HOSTS_FILE")),
		HealthTimeout:          healthTimeout,
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

func getEnvInt(key string, fallback int) (int, error) {
	value := getEnv(key, strconv.Itoa(fallback))
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", key, err)
	}
	return parsed, nil
}

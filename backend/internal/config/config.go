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
	ProjectsRoot           string
	SessionTTL             time.Duration
	CookieSecure           bool
	BootstrapAdminUsername string
	BootstrapAdminPassword string
	MasterKeyBase64        string
	AppVersion             string

	MySQLHost          string
	MySQLPort          int
	MySQLAdminUser     string
	MySQLAdminPassword string
	MySQLAppHost       string
	MySQLBinary        string
	MySQLDumpBinary    string
	MySQLBackupDir     string

	PHPMyAdminDockerBinary string
	PHPMyAdminImage        string
	PHPMyAdminContainer    string
	PHPMyAdminHostPort     int

	PortRangeStart      int
	PortRangeEnd        int
	NginxBinary         string
	NginxSitesAvailable string
	NginxSitesEnabled   string
	HostsFile           string
	HealthTimeout       time.Duration
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
	mysqlPort, err := getEnvInt("DEVBOX_MYSQL_PORT", 3306)
	if err != nil {
		return Config{}, err
	}
	phpMyAdminPort, err := getEnvInt("DEVBOX_PHPMYADMIN_PORT", 8081)
	if err != nil {
		return Config{}, err
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
		FrontendDir:            strings.TrimSpace(os.Getenv("DEVBOX_FRONTEND_DIR")),
		ProjectsRoot:           getEnv("DEVBOX_PROJECTS_ROOT", "./projects"),
		SessionTTL:             ttl,
		CookieSecure:           cookieSecure,
		BootstrapAdminUsername: strings.TrimSpace(os.Getenv("DEVBOX_BOOTSTRAP_ADMIN_USERNAME")),
		BootstrapAdminPassword: os.Getenv("DEVBOX_BOOTSTRAP_ADMIN_PASSWORD"),
		MasterKeyBase64:        strings.TrimSpace(os.Getenv("DEVBOX_MASTER_KEY")),
		AppVersion:             getEnv("DEVBOX_VERSION", "dev"),
		MySQLHost:              getEnv("DEVBOX_MYSQL_HOST", "127.0.0.1"),
		MySQLPort:              mysqlPort,
		MySQLAdminUser:         getEnv("DEVBOX_MYSQL_ADMIN_USER", "devbox_admin"),
		MySQLAdminPassword:     os.Getenv("DEVBOX_MYSQL_ADMIN_PASSWORD"),
		MySQLAppHost:           getEnv("DEVBOX_MYSQL_APP_HOST", "%"),
		MySQLBinary:            getEnv("DEVBOX_MYSQL_BIN", "mysql"),
		MySQLDumpBinary:        getEnv("DEVBOX_MYSQLDUMP_BIN", "mysqldump"),
		MySQLBackupDir:         getEnv("DEVBOX_MYSQL_BACKUP_DIR", "./data/backups/mysql"),
		PHPMyAdminDockerBinary: getEnv("DEVBOX_DOCKER_BIN", "docker"),
		PHPMyAdminImage:        getEnv("DEVBOX_PHPMYADMIN_IMAGE", "phpmyadmin:5.2-apache"),
		PHPMyAdminContainer:    getEnv("DEVBOX_PHPMYADMIN_CONTAINER", "devbox-phpmyadmin"),
		PHPMyAdminHostPort:     phpMyAdminPort,
		PortRangeStart:         portStart,
		PortRangeEnd:           portEnd,
		NginxBinary:            getEnv("DEVBOX_NGINX_BINARY", "nginx"),
		NginxSitesAvailable:    getEnv("DEVBOX_NGINX_SITES_AVAILABLE", "/etc/nginx/sites-available"),
		NginxSitesEnabled:      getEnv("DEVBOX_NGINX_SITES_ENABLED", "/etc/nginx/sites-enabled"),
		HostsFile:              strings.TrimSpace(os.Getenv("DEVBOX_HOSTS_FILE")),
		HealthTimeout:          healthTimeout,
	}

	if cfg.HTTPAddr == "" || cfg.DatabasePath == "" || cfg.MigrationsDir == "" || cfg.ProjectsRoot == "" {
		return Config{}, fmt.Errorf("HTTP address, database path, migrations directory and projects root are required")
	}
	if (cfg.BootstrapAdminUsername == "") != (cfg.BootstrapAdminPassword == "") {
		return Config{}, fmt.Errorf("bootstrap admin username and password must be configured together")
	}
	if cfg.MySQLPort < 1 || cfg.MySQLPort > 65535 {
		return Config{}, fmt.Errorf("DEVBOX_MYSQL_PORT must be between 1 and 65535")
	}
	if cfg.PHPMyAdminHostPort < 1 || cfg.PHPMyAdminHostPort > 65535 {
		return Config{}, fmt.Errorf("DEVBOX_PHPMYADMIN_PORT must be between 1 and 65535")
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

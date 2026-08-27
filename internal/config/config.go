package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	HTTPAddr               string
	DatabaseDSN            string
	SessionTTL             time.Duration
	ShutdownTimeout        time.Duration
	WorkerPollInterval     time.Duration
	WorkerLease            time.Duration
	WorkerJobTimeout       time.Duration
	WorkerMaxAttempts      int
	BootstrapAdminEmail    string
	BootstrapAdminPassword string
}

func Load() (Config, error) {
	cfg := Config{
		HTTPAddr:               env("HTTP_ADDR", ":8080"),
		DatabaseDSN:            env("DATABASE_DSN", "file:data/cultivar.db"),
		BootstrapAdminEmail:    env("BOOTSTRAP_ADMIN_EMAIL", "admin@example.test"),
		BootstrapAdminPassword: env("BOOTSTRAP_ADMIN_PASSWORD", "change-me-now-2026"),
	}
	var err error
	if cfg.SessionTTL, err = duration("SESSION_TTL", 8*time.Hour); err != nil {
		return Config{}, err
	}
	if cfg.ShutdownTimeout, err = duration("SHUTDOWN_TIMEOUT", 10*time.Second); err != nil {
		return Config{}, err
	}
	if cfg.WorkerPollInterval, err = duration("WORKER_POLL_INTERVAL", 2*time.Second); err != nil {
		return Config{}, err
	}
	if cfg.WorkerLease, err = duration("WORKER_LEASE", 30*time.Second); err != nil {
		return Config{}, err
	}
	if cfg.WorkerJobTimeout, err = duration("WORKER_JOB_TIMEOUT", 20*time.Second); err != nil {
		return Config{}, err
	}
	if cfg.WorkerMaxAttempts, err = integer("WORKER_MAX_ATTEMPTS", 5); err != nil {
		return Config{}, err
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) Validate() error {
	var failures []string
	if strings.TrimSpace(c.HTTPAddr) == "" {
		failures = append(failures, "HTTP_ADDR is empty")
	}
	if strings.TrimSpace(c.DatabaseDSN) == "" {
		failures = append(failures, "DATABASE_DSN is empty")
	}
	if c.SessionTTL <= 0 {
		failures = append(failures, "SESSION_TTL must be positive")
	}
	if c.WorkerLease <= c.WorkerPollInterval {
		failures = append(failures, "WORKER_LEASE must exceed WORKER_POLL_INTERVAL")
	}
	if c.WorkerMaxAttempts < 1 || c.WorkerMaxAttempts > 20 {
		failures = append(failures, "WORKER_MAX_ATTEMPTS must be between 1 and 20")
	}
	if len(c.BootstrapAdminPassword) < 10 {
		failures = append(failures, "BOOTSTRAP_ADMIN_PASSWORD must contain at least 10 characters")
	}
	if len(failures) > 0 {
		return errors.New(strings.Join(failures, "; "))
	}
	return nil
}

func env(name, fallback string) string {
	if value, ok := os.LookupEnv(name); ok {
		return strings.TrimSpace(value)
	}
	return fallback
}

func duration(name string, fallback time.Duration) (time.Duration, error) {
	value := env(name, fallback.String())
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", name, err)
	}
	return parsed, nil
}

func integer(name string, fallback int) (int, error) {
	value := env(name, strconv.Itoa(fallback))
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", name, err)
	}
	return parsed, nil
}

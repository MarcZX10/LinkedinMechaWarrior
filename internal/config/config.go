// Package config reads lmw settings from environment variables.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Config holds every setting; nothing is required to run.
type Config struct {
	StateDir            string
	RequestTimeout      time.Duration
	MaxPostChars        int
	RequestMinDelay     time.Duration
	RequestMaxDelay     time.Duration
	HourlyRequestBudget int
	DailyRequestBudget  int
	// SessionStore is "auto" (keyring, falling back to a file), "keyring" or "file".
	SessionStore string
}

// RequestLogFile stores request timestamps and cooldowns for pacing.
func (c Config) RequestLogFile() string { return filepath.Join(c.StateDir, "request-log.json") }

// SessionFile is used when the system keyring is not available.
func (c Config) SessionFile() string { return filepath.Join(c.StateDir, "session.json") }

// IdentityFile stores the browser identity imported from a HAR file.
func (c Config) IdentityFile() string { return filepath.Join(c.StateDir, "identity.json") }

// QueriesFile stores the GraphQL query versions learned from HAR files.
func (c Config) QueriesFile() string { return filepath.Join(c.StateDir, "graphql-queries.json") }

// WatchesFile stores the watches `lmw daemon` checks.
func (c Config) WatchesFile() string { return filepath.Join(c.StateDir, "watches.json") }

// NotifyFile stores the notification channels, with their tokens.
func (c Config) NotifyFile() string { return filepath.Join(c.StateDir, "notify.json") }

// Load reads the configuration from the environment and validates it.
func Load() (Config, error) {
	return FromEnv(os.Getenv)
}

// FromEnv builds a validated Config from a getenv function.
func FromEnv(getenv func(string) string) (Config, error) {
	r := reader{getenv: getenv}
	stateDir := getenv("LMW_STATE_DIR")
	if stateDir == "" {
		stateDir = defaultStateDir(getenv)
	}
	cfg := Config{
		StateDir:            stateDir,
		RequestTimeout:      r.seconds("LMW_REQUEST_TIMEOUT", 30),
		MaxPostChars:        r.integer("LMW_MAX_POST_CHARS", 3000),
		RequestMinDelay:     r.seconds("LMW_REQUEST_MIN_DELAY", 2),
		RequestMaxDelay:     r.seconds("LMW_REQUEST_MAX_DELAY", 6),
		HourlyRequestBudget: r.integer("LMW_HOURLY_REQUEST_BUDGET", 60),
		DailyRequestBudget:  r.integer("LMW_DAILY_REQUEST_BUDGET", 300),
		SessionStore:        strings.ToLower(strings.TrimSpace(getenv("LMW_SESSION_STORE"))),
	}
	if cfg.SessionStore == "" {
		cfg.SessionStore = "auto"
	}
	if r.err != nil {
		return Config{}, r.err
	}
	return cfg, cfg.Validate()
}

// Validate rejects settings that are unusable or unsafe.
func (c Config) Validate() error {
	switch {
	case c.RequestTimeout < 5*time.Second:
		return fmt.Errorf("LMW_REQUEST_TIMEOUT must be at least 5 seconds")
	case c.MaxPostChars < 280:
		return fmt.Errorf("LMW_MAX_POST_CHARS must be at least 280")
	case c.RequestMinDelay < time.Second:
		return fmt.Errorf("LMW_REQUEST_MIN_DELAY must be at least 1 second")
	case c.RequestMaxDelay < c.RequestMinDelay:
		return fmt.Errorf("LMW_REQUEST_MAX_DELAY must be greater than or equal to LMW_REQUEST_MIN_DELAY")
	case c.HourlyRequestBudget < 1 || c.DailyRequestBudget < 1:
		return fmt.Errorf("request budgets must be positive")
	case c.HourlyRequestBudget > c.DailyRequestBudget:
		return fmt.Errorf("LMW_HOURLY_REQUEST_BUDGET cannot exceed LMW_DAILY_REQUEST_BUDGET")
	}
	switch c.SessionStore {
	case "auto", "keyring", "file":
	default:
		return fmt.Errorf("LMW_SESSION_STORE must be auto, keyring or file, got %q", c.SessionStore)
	}
	return nil
}

// EnsureStateDir creates the state directory, readable only by the user.
func (c Config) EnsureStateDir() error {
	return os.MkdirAll(c.StateDir, 0o700)
}

func defaultStateDir(getenv func(string) string) string {
	home, _ := os.UserHomeDir()
	switch runtime.GOOS {
	case "windows":
		base := getenv("LOCALAPPDATA")
		if base == "" {
			base = filepath.Join(home, "AppData", "Local")
		}
		return filepath.Join(base, "LinkedinMechaWarrior")
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "LinkedinMechaWarrior")
	default:
		base := getenv("XDG_STATE_HOME")
		if base == "" {
			base = filepath.Join(home, ".local", "state")
		}
		return filepath.Join(base, "linkedin-mecha-warrior")
	}
}

type reader struct {
	getenv func(string) string
	err    error
}

func (r *reader) integer(name string, fallback int) int {
	raw := strings.TrimSpace(r.getenv(name))
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil && r.err == nil {
		r.err = fmt.Errorf("%s must be an integer, got %q", name, raw)
	}
	return value
}

func (r *reader) seconds(name string, fallback float64) time.Duration {
	raw := strings.TrimSpace(r.getenv(name))
	value := fallback
	if raw != "" {
		parsed, err := strconv.ParseFloat(raw, 64)
		if err != nil && r.err == nil {
			r.err = fmt.Errorf("%s must be a number of seconds, got %q", name, raw)
		}
		value = parsed
	}
	return time.Duration(value * float64(time.Second))
}

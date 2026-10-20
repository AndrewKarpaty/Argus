// Package config loads the platform configuration from the environment. Every
// setting has a usable default, so `go run ./cmd/argusd` works in a fresh
// checkout with no setup at all.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
)

// Engine names the reasoner the investigator should use.
type Engine string

const (
	// EngineAuto picks Claude when credentials are present, heuristic
	// otherwise. This is the default so the platform always starts.
	EngineAuto Engine = "auto"
	// EngineClaude is the model-driven agent.
	EngineClaude Engine = "claude"
	// EngineHeuristic is the deterministic graph heuristic.
	EngineHeuristic Engine = "heuristic"
)

// Config is the full configuration of an Argus process.
type Config struct {
	// HTTP
	Addr              string
	ReadHeaderTimeout time.Duration
	RequestTimeout    time.Duration
	ShutdownTimeout   time.Duration

	// Logging
	LogLevel  slog.Level
	LogFormat string // json | text

	// Catalog
	CatalogPath    string
	CatalogRefresh time.Duration

	// Investigation
	Engine               Engine
	InvestigationTimeout time.Duration

	// Model
	AnthropicAPIKey  string
	Model            string
	Effort           string
	MaxTokens        int64
	MaxIterations    int
	AnthropicBaseURL string

	// Telemetry
	// DemoTelemetry wires the synthetic provider, so the platform can be run
	// end to end before Prometheus and friends are connected.
	DemoTelemetry bool
	DemoFaulty    string
}

// Load reads the configuration from the environment. Every variable is
// prefixed ARGUS_, except ANTHROPIC_API_KEY which keeps the name the SDK and
// every other Anthropic tool already use.
func Load() (Config, error) {
	var errs []error
	fail := func(key string, err error) {
		errs = append(errs, fmt.Errorf("%s: %w", key, err))
	}

	cfg := Config{
		Addr:             env("ARGUS_ADDR", ":8080"),
		LogFormat:        strings.ToLower(env("ARGUS_LOG_FORMAT", "text")),
		CatalogPath:      env("ARGUS_CATALOG_PATH", "configs/services.yaml"),
		Engine:           Engine(strings.ToLower(env("ARGUS_ENGINE", string(EngineAuto)))),
		AnthropicAPIKey:  os.Getenv("ANTHROPIC_API_KEY"),
		Model:            env("ARGUS_MODEL", "claude-opus-5-5"),
		Effort:           strings.ToLower(env("ARGUS_EFFORT", "high")),
		AnthropicBaseURL: os.Getenv("ANTHROPIC_BASE_URL"),
		DemoFaulty:       env("ARGUS_DEMO_FAULTY_SERVICE", "postgres-main"),
	}

	var err error
	if cfg.ReadHeaderTimeout, err = envDuration("ARGUS_READ_HEADER_TIMEOUT", 10*time.Second); err != nil {
		fail("ARGUS_READ_HEADER_TIMEOUT", err)
	}
	if cfg.RequestTimeout, err = envDuration("ARGUS_REQUEST_TIMEOUT", 30*time.Second); err != nil {
		fail("ARGUS_REQUEST_TIMEOUT", err)
	}
	if cfg.ShutdownTimeout, err = envDuration("ARGUS_SHUTDOWN_TIMEOUT", 20*time.Second); err != nil {
		fail("ARGUS_SHUTDOWN_TIMEOUT", err)
	}
	if cfg.CatalogRefresh, err = envDuration("ARGUS_CATALOG_REFRESH", 0); err != nil {
		fail("ARGUS_CATALOG_REFRESH", err)
	}
	if cfg.InvestigationTimeout, err = envDuration("ARGUS_INVESTIGATION_TIMEOUT", 5*time.Minute); err != nil {
		fail("ARGUS_INVESTIGATION_TIMEOUT", err)
	}
	if cfg.LogLevel, err = envLevel("ARGUS_LOG_LEVEL", slog.LevelInfo); err != nil {
		fail("ARGUS_LOG_LEVEL", err)
	}
	if cfg.MaxTokens, err = envInt64("ARGUS_MAX_TOKENS", 16000); err != nil {
		fail("ARGUS_MAX_TOKENS", err)
	}
	maxIter, err := envInt64("ARGUS_MAX_ITERATIONS", 12)
	if err != nil {
		fail("ARGUS_MAX_ITERATIONS", err)
	}
	cfg.MaxIterations = int(maxIter)
	if cfg.DemoTelemetry, err = envBool("ARGUS_DEMO_TELEMETRY", true); err != nil {
		fail("ARGUS_DEMO_TELEMETRY", err)
	}

	if err := cfg.validate(); err != nil {
		errs = append(errs, err)
	}
	if len(errs) > 0 {
		return Config{}, fmt.Errorf("config: %w", errors.Join(errs...))
	}
	return cfg, nil
}

func (c *Config) validate() error {
	var errs []error
	switch c.Engine {
	case EngineAuto, EngineClaude, EngineHeuristic:
	default:
		errs = append(errs, fmt.Errorf("ARGUS_ENGINE: unknown engine %q, want auto, claude or heuristic", c.Engine))
	}
	switch c.LogFormat {
	case "json", "text":
	default:
		errs = append(errs, fmt.Errorf("ARGUS_LOG_FORMAT: want json or text, got %q", c.LogFormat))
	}
	if c.Engine == EngineClaude && !c.HasModelCredentials() {
		errs = append(errs, errors.New("ARGUS_ENGINE=claude but no model credentials found: set ANTHROPIC_API_KEY, run `ant auth login`, or use ARGUS_ENGINE=heuristic"))
	}
	if c.CatalogPath == "" {
		errs = append(errs, errors.New("ARGUS_CATALOG_PATH must not be empty"))
	}
	return errors.Join(errs...)
}

// HasModelCredentials reports whether a Claude credential is discoverable.
// The SDK also resolves OAuth profiles written by `ant auth login`, so an
// unset ANTHROPIC_API_KEY does not by itself mean there are no credentials.
func (c Config) HasModelCredentials() bool {
	if c.AnthropicAPIKey != "" || os.Getenv("ANTHROPIC_AUTH_TOKEN") != "" {
		return true
	}
	if home, err := os.UserConfigDir(); err == nil {
		if _, err := os.Stat(home + "/anthropic"); err == nil {
			return true
		}
	}
	return false
}

// ResolvedEngine turns EngineAuto into a concrete choice.
func (c Config) ResolvedEngine() Engine {
	if c.Engine != EngineAuto {
		return c.Engine
	}
	if c.HasModelCredentials() {
		return EngineClaude
	}
	return EngineHeuristic
}

// Logger builds the process logger from the configuration.
func (c Config) Logger() *slog.Logger {
	opts := &slog.HandlerOptions{Level: c.LogLevel}
	if c.LogFormat == "json" {
		return slog.New(slog.NewJSONHandler(os.Stdout, opts))
	}
	return slog.New(slog.NewTextHandler(os.Stdout, opts))
}

func env(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func envDuration(key string, fallback time.Duration) (time.Duration, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return fallback, fmt.Errorf("not a duration (e.g. 30s, 5m): %w", err)
	}
	if d < 0 {
		return fallback, errors.New("must not be negative")
	}
	return d, nil
}

func envInt64(key string, fallback int64) (int64, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback, nil
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return fallback, fmt.Errorf("not an integer: %w", err)
	}
	if n <= 0 {
		return fallback, errors.New("must be positive")
	}
	return n, nil
}

func envBool(key string, fallback bool) (bool, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback, nil
	}
	b, err := strconv.ParseBool(raw)
	if err != nil {
		return fallback, fmt.Errorf("not a boolean: %w", err)
	}
	return b, nil
}

func envLevel(key string, fallback slog.Level) (slog.Level, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback, nil
	}
	var level slog.Level
	if err := level.UnmarshalText([]byte(raw)); err != nil {
		return fallback, fmt.Errorf("want debug, info, warn or error: %w", err)
	}
	return level, nil
}

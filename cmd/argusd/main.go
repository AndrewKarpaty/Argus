// Command argusd runs the Argus HTTP API: incident intake, the service
// dependency graph, and investigations.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"
	"time"

	"github.com/AndrewKarpaty/Argus/internal/agent"
	"github.com/AndrewKarpaty/Argus/internal/api"
	"github.com/AndrewKarpaty/Argus/internal/catalog"
	"github.com/AndrewKarpaty/Argus/internal/config"
	"github.com/AndrewKarpaty/Argus/internal/investigator"
	"github.com/AndrewKarpaty/Argus/internal/store"
	"github.com/AndrewKarpaty/Argus/internal/telemetry"
	"github.com/AndrewKarpaty/Argus/internal/telemetry/demo"
)

// version is overridden at build time: -ldflags "-X main.version=$(git describe)".
var version = ""

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "argusd: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log := cfg.Logger()
	slog.SetDefault(log)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	log.Info("starting argusd",
		"version", buildVersion(),
		"addr", cfg.Addr,
		"catalog", cfg.CatalogPath,
		"engine", cfg.ResolvedEngine())

	// Catalog and dependency graph.
	provider, err := catalog.NewProvider(ctx, catalog.NewFile(cfg.CatalogPath), log)
	if err != nil {
		return err
	}
	stopWatch := provider.WatchInterval(ctx, cfg.CatalogRefresh)
	defer stopWatch()

	// Telemetry providers.
	collector := buildCollector(cfg, log)

	// Reasoner.
	reasoner, err := buildReasoner(cfg, log)
	if err != nil {
		return err
	}

	incidents := store.NewMemoryIncidents()
	investigations := store.NewMemoryInvestigations()

	engine, err := investigator.NewEngine(investigator.Config{
		Reasoner:       reasoner,
		Catalog:        provider,
		Collector:      collector,
		Incidents:      incidents,
		Investigations: investigations,
		Log:            log,
		Timeout:        cfg.InvestigationTimeout,
	})
	if err != nil {
		return err
	}

	srv, err := api.New(api.Deps{
		Catalog:        provider,
		Engine:         engine,
		Incidents:      incidents,
		Investigations: investigations,
		Collector:      collector,
		Log:            log,
		RequestTimeout: cfg.RequestTimeout,
		Version:        buildVersion(),
	})
	if err != nil {
		return err
	}

	httpSrv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
		// No WriteTimeout: investigations are started in the background, so
		// handlers are short, but long polling could be added later.
		IdleTimeout: 2 * time.Minute,
		BaseContext: func(_ net.Listener) context.Context { return ctx },
	}

	errCh := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", cfg.Addr)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return fmt.Errorf("http server: %w", err)
	case <-ctx.Done():
		log.Info("shutdown signal received")
	}

	// Stop accepting requests, then let running investigations finish.
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cfg.ShutdownTimeout)
	defer cancel()
	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		log.Error("graceful shutdown failed", "error", err)
	}

	done := make(chan struct{})
	go func() {
		engine.Wait()
		close(done)
	}()
	select {
	case <-done:
		log.Info("all investigations finished, exiting")
	case <-shutdownCtx.Done():
		log.Warn("shutdown timeout reached while investigations were still running")
	}
	return nil
}

// buildCollector wires the telemetry providers. Replace the demo provider
// with real ones here: the Collector fields are plain interfaces.
func buildCollector(cfg config.Config, log *slog.Logger) *telemetry.Collector {
	collector := &telemetry.Collector{Log: log, Limit: 20}
	if cfg.DemoTelemetry {
		p := demo.New(cfg.DemoFaulty)
		collector.Metrics, collector.Logs, collector.Traces, collector.Changes = p, p, p, p
		log.Warn("using the synthetic demo telemetry provider; set ARGUS_DEMO_TELEMETRY=false once real backends are wired",
			"synthetic_root_cause", cfg.DemoFaulty)
	} else {
		log.Warn("no telemetry providers configured: investigations will reason from topology alone")
	}
	return collector
}

func buildReasoner(cfg config.Config, log *slog.Logger) (investigator.Reasoner, error) {
	switch engine := cfg.ResolvedEngine(); engine {
	case config.EngineClaude:
		return agent.New(agent.Config{
			APIKey:        cfg.AnthropicAPIKey,
			Model:         cfg.Model,
			Effort:        cfg.Effort,
			MaxTokens:     cfg.MaxTokens,
			MaxIterations: cfg.MaxIterations,
			BaseURL:       cfg.AnthropicBaseURL,
			Log:           log,
		})
	case config.EngineHeuristic:
		if cfg.Engine == config.EngineAuto {
			log.Warn("no model credentials found, falling back to the deterministic heuristic engine; set ANTHROPIC_API_KEY for the Claude agent")
		}
		return investigator.NewHeuristic(), nil
	default:
		return nil, fmt.Errorf("unsupported engine %q", engine)
	}
}

func buildVersion() string {
	if version != "" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, s := range info.Settings {
			if s.Key == "vcs.revision" && len(s.Value) >= 7 {
				return s.Value[:7]
			}
		}
	}
	return "dev"
}

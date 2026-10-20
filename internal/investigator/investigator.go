// Package investigator runs incident investigations. It owns the lifecycle -
// create a run, execute it, persist the result - while the actual reasoning is
// delegated to a Reasoner, of which there are two in this skeleton: a
// deterministic graph heuristic and a Claude-driven agent.
package investigator

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/AndrewKarpaty/Argus/internal/catalog"
	"github.com/AndrewKarpaty/Argus/internal/domain"
	"github.com/AndrewKarpaty/Argus/internal/graph"
	"github.com/AndrewKarpaty/Argus/internal/store"
	"github.com/AndrewKarpaty/Argus/internal/telemetry"
)

// Input is everything a Reasoner is given. The graph is a snapshot, so a run
// is not disturbed by a catalog reload halfway through.
type Input struct {
	Incident  domain.Incident
	Graph     *graph.Graph
	Collector *telemetry.Collector
}

// Window is the time range the investigation should examine.
func (in Input) Window() domain.TimeWindow { return in.Incident.Window() }

// Reasoner turns an incident plus a topology into ranked hypotheses.
type Reasoner interface {
	// Name identifies the engine in stored investigations.
	Name() string
	// Analyze is expected to honour ctx cancellation; it is run under a
	// per-investigation timeout.
	Analyze(ctx context.Context, in Input) (domain.Analysis, error)
}

// Engine orchestrates investigations.
type Engine struct {
	reasoner       Reasoner
	catalog        *catalog.Provider
	collector      *telemetry.Collector
	incidents      store.Incidents
	investigations store.Investigations
	log            *slog.Logger
	timeout        time.Duration

	wg sync.WaitGroup
}

// Config wires an Engine.
type Config struct {
	Reasoner       Reasoner
	Catalog        *catalog.Provider
	Collector      *telemetry.Collector
	Incidents      store.Incidents
	Investigations store.Investigations
	Log            *slog.Logger
	// Timeout bounds a single run. Defaults to 5 minutes.
	Timeout time.Duration
}

// NewEngine validates the wiring and returns an Engine.
func NewEngine(cfg Config) (*Engine, error) {
	switch {
	case cfg.Reasoner == nil:
		return nil, errors.New("investigator: reasoner is required")
	case cfg.Catalog == nil:
		return nil, errors.New("investigator: catalog provider is required")
	case cfg.Incidents == nil || cfg.Investigations == nil:
		return nil, errors.New("investigator: both stores are required")
	}
	if cfg.Log == nil {
		cfg.Log = slog.Default()
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 5 * time.Minute
	}
	return &Engine{
		reasoner:       cfg.Reasoner,
		catalog:        cfg.Catalog,
		collector:      cfg.Collector,
		incidents:      cfg.Incidents,
		investigations: cfg.Investigations,
		log:            cfg.Log,
		timeout:        cfg.Timeout,
	}, nil
}

// Engine returns the reasoner name, for /readyz and API responses.
func (e *Engine) EngineName() string { return e.reasoner.Name() }

// Start creates a queued investigation and runs it in the background. The
// returned record is the queued state; poll it by id for the result.
//
// The background run deliberately does not inherit the HTTP request context:
// an investigation must survive the client hanging up.
func (e *Engine) Start(ctx context.Context, incidentID string) (domain.Investigation, error) {
	inc, err := e.incidents.Get(ctx, incidentID)
	if err != nil {
		return domain.Investigation{}, err
	}
	iv := domain.Investigation{
		ID:         domain.NewID("inv"),
		IncidentID: inc.ID,
		Status:     domain.InvestigationQueued,
		Engine:     e.reasoner.Name(),
		StartedAt:  time.Now().UTC(),
	}
	if err := e.investigations.Create(ctx, iv); err != nil {
		return domain.Investigation{}, err
	}

	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		runCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), e.timeout)
		defer cancel()
		if _, err := e.execute(runCtx, inc, iv); err != nil {
			e.log.Error("investigation failed",
				"investigation", iv.ID, "incident", inc.ID, "error", err)
		}
	}()
	return iv, nil
}

// Run executes an investigation synchronously and returns the finished
// record. The CLI and tests use this; the HTTP API uses Start.
func (e *Engine) Run(ctx context.Context, incidentID string) (domain.Investigation, error) {
	inc, err := e.incidents.Get(ctx, incidentID)
	if err != nil {
		return domain.Investigation{}, err
	}
	iv := domain.Investigation{
		ID:         domain.NewID("inv"),
		IncidentID: inc.ID,
		Status:     domain.InvestigationQueued,
		Engine:     e.reasoner.Name(),
		StartedAt:  time.Now().UTC(),
	}
	if err := e.investigations.Create(ctx, iv); err != nil {
		return domain.Investigation{}, err
	}
	runCtx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()
	return e.execute(runCtx, inc, iv)
}

// execute runs the reasoner and persists the outcome. It returns the final
// record; the error is the reasoner's, already recorded on the record.
func (e *Engine) execute(ctx context.Context, inc domain.Incident, iv domain.Investigation) (domain.Investigation, error) {
	started := time.Now()
	iv.Status = domain.InvestigationRunning
	if err := e.investigations.Update(ctx, iv); err != nil {
		return iv, fmt.Errorf("mark running: %w", err)
	}
	e.markIncidentInvestigating(ctx, inc)

	g := e.catalog.Graph()
	e.log.Info("investigation started",
		"investigation", iv.ID, "incident", inc.ID, "engine", iv.Engine,
		"affected", inc.AffectedServices, "window", inc.Window().String())

	analysis, err := e.reasoner.Analyze(ctx, Input{Incident: inc, Graph: g, Collector: e.collector})
	finished := time.Now().UTC()
	iv.FinishedAt = &finished
	iv.Steps = analysis.Steps
	iv.Usage = analysis.Usage

	if err != nil {
		iv.Status = domain.InvestigationFailed
		iv.Error = err.Error()
	} else {
		iv.Status = domain.InvestigationCompleted
		iv.Summary = analysis.Summary
		iv.Hypotheses = analysis.Hypotheses
	}

	// Persist with a context that outlives a cancelled run, so a timed-out
	// investigation is still recorded as failed rather than left running.
	saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	if uerr := e.investigations.Update(saveCtx, iv); uerr != nil {
		return iv, errors.Join(err, fmt.Errorf("persist result: %w", uerr))
	}

	attrs := []any{
		"investigation", iv.ID, "incident", inc.ID, "engine", iv.Engine,
		"status", iv.Status, "duration", time.Since(started).Round(time.Millisecond),
		"hypotheses", len(iv.Hypotheses), "steps", len(iv.Steps),
	}
	if top, ok := iv.TopHypothesis(); ok {
		attrs = append(attrs, "top_suspect", top.RootCauseService, "confidence", top.Confidence)
	}
	if err != nil {
		e.log.Error("investigation finished with an error", append(attrs, "error", err)...)
	} else {
		e.log.Info("investigation finished", attrs...)
	}
	return iv, err
}

func (e *Engine) markIncidentInvestigating(ctx context.Context, inc domain.Incident) {
	if inc.Status != domain.IncidentOpen {
		return
	}
	inc.Status = domain.IncidentInvestigating
	inc.UpdatedAt = time.Now().UTC()
	if err := e.incidents.Update(ctx, inc); err != nil {
		e.log.Warn("could not mark incident as investigating", "incident", inc.ID, "error", err)
	}
}

// Wait blocks until every background investigation has finished. Call it
// during shutdown, after the HTTP server has stopped accepting requests.
func (e *Engine) Wait() { e.wg.Wait() }

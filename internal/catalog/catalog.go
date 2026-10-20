// Package catalog loads the service catalog - the services and the
// dependencies between them - and keeps a ready-to-query graph.Graph in
// memory. The file-backed source is the skeleton's default; a Kubernetes,
// OpenTelemetry service-map or Backstage source implements the same Source
// interface.
package catalog

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/AndrewKarpaty/Argus/internal/domain"
	"github.com/AndrewKarpaty/Argus/internal/graph"
)

// Snapshot is one load of the catalog.
type Snapshot struct {
	Services     []domain.Service    `json:"services"`
	Dependencies []domain.Dependency `json:"dependencies"`
	Source       string              `json:"source"`
	LoadedAt     time.Time           `json:"loaded_at"`
}

// Source is anything that can produce a catalog snapshot.
type Source interface {
	// Name identifies the source in logs and API responses.
	Name() string
	// Load returns the current catalog.
	Load(ctx context.Context) (Snapshot, error)
}

// Provider turns a Source into a graph that handlers can read concurrently.
// Refresh builds a new graph and swaps it atomically, so in-flight
// investigations keep working against the graph they started with.
type Provider struct {
	src Source
	log *slog.Logger

	mu       sync.RWMutex
	graph    *graph.Graph
	snapshot Snapshot
}

// NewProvider loads the catalog once and fails if it is unusable: starting up
// with a broken topology would make every investigation silently wrong.
func NewProvider(ctx context.Context, src Source, log *slog.Logger) (*Provider, error) {
	if log == nil {
		log = slog.Default()
	}
	p := &Provider{src: src, log: log}
	if err := p.Refresh(ctx); err != nil {
		return nil, err
	}
	return p, nil
}

// Refresh reloads the catalog from the source.
func (p *Provider) Refresh(ctx context.Context) error {
	snap, err := p.src.Load(ctx)
	if err != nil {
		return fmt.Errorf("catalog: load from %s: %w", p.src.Name(), err)
	}
	g, err := graph.New(snap.Services, snap.Dependencies)
	if err != nil {
		return fmt.Errorf("catalog: build graph from %s: %w", p.src.Name(), err)
	}
	if snap.LoadedAt.IsZero() {
		snap.LoadedAt = time.Now().UTC()
	}
	if snap.Source == "" {
		snap.Source = p.src.Name()
	}

	services, deps := g.Size()
	p.mu.Lock()
	p.graph, p.snapshot = g, snap
	p.mu.Unlock()

	attrs := []any{"source", snap.Source, "services", services, "dependencies", deps}
	if cycles := g.Cycles(); len(cycles) > 0 {
		// Not fatal: circular dependencies exist in the real world. They do
		// make root-cause attribution ambiguous, so say so loudly.
		attrs = append(attrs, "cycles", len(cycles), "first_cycle", cycles[0])
		p.log.Warn("catalog contains circular dependencies", attrs...)
	} else {
		p.log.Info("catalog loaded", attrs...)
	}
	return nil
}

// Graph returns the current graph. It is immutable; hold on to it for the
// duration of a request or an investigation.
func (p *Provider) Graph() *graph.Graph {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.graph
}

// Snapshot returns metadata about the current load.
func (p *Provider) Snapshot() Snapshot {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.snapshot
}

// WatchInterval starts a background refresh loop and returns a stop function.
// Interval <= 0 disables periodic refresh.
func (p *Provider) WatchInterval(ctx context.Context, interval time.Duration) func() {
	if interval <= 0 {
		return func() {}
	}
	ctx, cancel := context.WithCancel(ctx)
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if err := p.Refresh(ctx); err != nil {
					// Keep serving the previous good graph.
					p.log.Error("catalog refresh failed, keeping previous snapshot", "error", err)
				}
			}
		}
	}()
	return cancel
}

// Static is a Source backed by an in-memory snapshot, useful in tests.
type Static struct {
	Snap Snapshot
}

func (s Static) Name() string { return "static" }

func (s Static) Load(context.Context) (Snapshot, error) { return s.Snap, nil }

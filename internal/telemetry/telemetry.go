// Package telemetry defines the read-only interfaces Argus uses to pull
// evidence about a service during an incident, plus a Collector that fans the
// queries out across providers.
//
// The interfaces are intentionally narrow: an investigation asks "what looks
// wrong with this service in this window", not "run this PromQL". That keeps
// Prometheus, Datadog, Loki, Tempo and a demo generator interchangeable, and
// keeps query syntax out of the prompt.
package telemetry

import (
	"context"

	"github.com/AndrewKarpaty/Argus/internal/domain"
)

// Query scopes a telemetry lookup to one service and one time window.
type Query struct {
	ServiceID string
	Window    domain.TimeWindow
	// Limit caps the number of returned items (<= 0 means provider default).
	Limit int
}

// MetricsProvider surfaces metric anomalies: error-rate spikes, latency
// regressions, saturation. Implement against Prometheus, Datadog or Cloud
// Monitoring.
type MetricsProvider interface {
	Anomalies(ctx context.Context, q Query) ([]domain.Signal, error)
}

// LogsProvider surfaces aggregated error patterns rather than raw lines: a
// hundred identical stack traces are one piece of evidence, not a hundred.
type LogsProvider interface {
	ErrorPatterns(ctx context.Context, q Query) ([]domain.Signal, error)
}

// TracesProvider surfaces failing request paths. Traces observe the real
// topology, which makes them the best available check on the catalog.
type TracesProvider interface {
	FailingPaths(ctx context.Context, q Query) ([]domain.TracePath, error)
}

// ChangeProvider surfaces deploys, config edits and flag flips. Most
// incidents start with one of these, so it is usually the highest-value
// provider to wire up first.
type ChangeProvider interface {
	RecentChanges(ctx context.Context, q Query) ([]domain.Change, error)
}

// Bundle is the evidence gathered for a set of services in one window.
type Bundle struct {
	Window  domain.TimeWindow  `json:"window"`
	Signals []domain.Signal    `json:"signals"`
	Changes []domain.Change    `json:"changes"`
	Paths   []domain.TracePath `json:"paths"`
	// Errors holds per-provider failures. A partial bundle is still useful,
	// so collection never fails outright because one backend is down.
	Errors []string `json:"errors,omitempty"`
}

// SignalsFor returns the bundle's signals for one service.
func (b Bundle) SignalsFor(serviceID string) []domain.Signal {
	var out []domain.Signal
	for _, s := range b.Signals {
		if s.ServiceID == serviceID {
			out = append(out, s)
		}
	}
	return out
}

// ChangesFor returns the bundle's changes for one service.
func (b Bundle) ChangesFor(serviceID string) []domain.Change {
	var out []domain.Change
	for _, c := range b.Changes {
		if c.ServiceID == serviceID {
			out = append(out, c)
		}
	}
	return out
}

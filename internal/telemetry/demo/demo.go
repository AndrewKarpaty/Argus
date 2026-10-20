// Package demo provides a deterministic, synthetic telemetry backend so that
// the platform can be run, demoed and tested end to end before Prometheus,
// Loki, Tempo or a deploy tracker are wired up.
//
// It is not a mock in the test-double sense: it produces a coherent incident
// story (a saturated dependency plus the deploy that caused it) so the
// investigation path exercises the same code an operator will.
package demo

import (
	"context"
	"fmt"
	"hash/fnv"
	"time"

	"github.com/AndrewKarpaty/Argus/internal/domain"
	"github.com/AndrewKarpaty/Argus/internal/telemetry"
)

// Provider implements every telemetry interface over synthetic data.
type Provider struct {
	// Faulty is the service the synthetic story blames. Services that are not
	// Faulty only ever show mild, derived symptoms.
	Faulty string
	// DeployedService had a deploy shortly before the incident. Defaults to
	// Faulty when empty.
	DeployedService string
	// Now overrides the clock, for reproducible tests.
	Now func() time.Time
}

// New returns a demo provider whose synthetic root cause is faulty.
func New(faulty string) *Provider { return &Provider{Faulty: faulty} }

var (
	_ telemetry.MetricsProvider = (*Provider)(nil)
	_ telemetry.LogsProvider    = (*Provider)(nil)
	_ telemetry.TracesProvider  = (*Provider)(nil)
	_ telemetry.ChangeProvider  = (*Provider)(nil)
)

func (p *Provider) now() time.Time {
	if p.Now != nil {
		return p.Now()
	}
	return time.Now().UTC()
}

func (p *Provider) deployed() string {
	if p.DeployedService != "" {
		return p.DeployedService
	}
	return p.Faulty
}

// jitter maps a service id to a stable value in [0,1), so repeated runs
// against the same catalog produce identical numbers.
func jitter(serviceID string, salt string) float64 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(serviceID + "|" + salt))
	return float64(h.Sum32()%1000) / 1000
}

// Anomalies returns synthetic metric anomalies.
func (p *Provider) Anomalies(_ context.Context, q telemetry.Query) ([]domain.Signal, error) {
	at := midpoint(q.Window)
	if q.ServiceID == p.Faulty {
		return []domain.Signal{
			{
				ID:          q.ServiceID + "/saturation",
				Source:      domain.SourceMetric,
				ServiceID:   q.ServiceID,
				Title:       "connection pool saturation at 98%",
				Description: "In-use connections pinned at the pool maximum for the whole window; wait time p99 above 2s.",
				Severity:    domain.SeverityCritical,
				ObservedAt:  at,
				Value:       0.98,
				Unit:        "ratio",
				Labels:      map[string]string{"metric": "pool_in_use_ratio"},
			},
			{
				ID:          q.ServiceID + "/latency",
				Source:      domain.SourceMetric,
				ServiceID:   q.ServiceID,
				Title:       "p99 latency 14x baseline",
				Description: "p99 query latency rose from 12ms to 170ms at the start of the window.",
				Severity:    domain.SeverityHigh,
				ObservedAt:  at,
				Value:       170,
				Unit:        "ms",
				Labels:      map[string]string{"metric": "query_duration_p99"},
			},
		}, nil
	}

	// Everything else shows the mild, derived symptoms of a sick dependency.
	errRate := 0.02 + jitter(q.ServiceID, "err")*0.1
	sev := domain.SeverityMedium
	if errRate > 0.08 {
		sev = domain.SeverityHigh
	}
	return []domain.Signal{{
		ID:          q.ServiceID + "/error_rate",
		Source:      domain.SourceMetric,
		ServiceID:   q.ServiceID,
		Title:       fmt.Sprintf("error rate %.1f%%", errRate*100),
		Description: "5xx responses elevated above the weekly baseline.",
		Severity:    sev,
		ObservedAt:  at,
		Value:       errRate,
		Unit:        "ratio",
		Labels:      map[string]string{"metric": "http_5xx_ratio"},
	}}, nil
}

// ErrorPatterns returns synthetic aggregated log patterns.
func (p *Provider) ErrorPatterns(_ context.Context, q telemetry.Query) ([]domain.Signal, error) {
	at := midpoint(q.Window)
	if q.ServiceID == p.Faulty {
		return []domain.Signal{{
			ID:          q.ServiceID + "/log/pool",
			Source:      domain.SourceLog,
			ServiceID:   q.ServiceID,
			Title:       "FATAL: remaining connection slots are reserved",
			Description: "4,812 occurrences in the window, first seen 3 minutes after the deploy.",
			Severity:    domain.SeverityCritical,
			ObservedAt:  at,
			Value:       4812,
			Unit:        "events",
		}}, nil
	}
	count := 40 + jitter(q.ServiceID, "log")*900
	return []domain.Signal{{
		ID:          q.ServiceID + "/log/timeout",
		Source:      domain.SourceLog,
		ServiceID:   q.ServiceID,
		Title:       "context deadline exceeded while acquiring a connection",
		Description: fmt.Sprintf("%.0f occurrences in the window, all on dependency calls.", count),
		Severity:    domain.SeverityHigh,
		ObservedAt:  at,
		Value:       count,
		Unit:        "events",
	}}, nil
}

// FailingPaths returns a synthetic failing request path.
func (p *Provider) FailingPaths(_ context.Context, q telemetry.Query) ([]domain.TracePath, error) {
	if q.ServiceID == p.Faulty {
		return nil, nil
	}
	return []domain.TracePath{{
		ServiceIDs:  []string{q.ServiceID, p.Faulty},
		FailingHop:  p.Faulty,
		ErrorRate:   0.1 + jitter(q.ServiceID, "trace")*0.3,
		P99Millis:   900 + jitter(q.ServiceID, "p99")*2000,
		SampleCount: 500,
	}}, nil
}

// RecentChanges returns the synthetic deploy that starts the story.
func (p *Provider) RecentChanges(_ context.Context, q telemetry.Query) ([]domain.Change, error) {
	if q.ServiceID != p.deployed() {
		return nil, nil
	}
	at := q.Window.To.Add(-8 * time.Minute)
	if at.Before(q.Window.From) {
		at = q.Window.From
	}
	return []domain.Change{{
		ID:        q.ServiceID + "/deploy/demo",
		ServiceID: q.ServiceID,
		Kind:      domain.ChangeDeploy,
		At:        at,
		Actor:     "ci-bot",
		Ref:       "v2026.10.3 (a1b2c3d)",
		Summary:   "Raised the per-pod connection pool size from 10 to 40 and doubled the replica count.",
	}}, nil
}

func midpoint(w domain.TimeWindow) time.Time {
	if w.From.IsZero() || w.To.IsZero() {
		return time.Now().UTC()
	}
	return w.From.Add(w.Duration() / 2)
}

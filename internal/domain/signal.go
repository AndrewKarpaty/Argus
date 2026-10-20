package domain

import (
	"fmt"
	"time"
)

// TimeWindow is the half-open interval telemetry queries are scoped to.
type TimeWindow struct {
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
}

// NewWindow returns a window ending at to and starting lookback earlier.
func NewWindow(to time.Time, lookback time.Duration) TimeWindow {
	return TimeWindow{From: to.Add(-lookback), To: to}
}

// Duration is the length of the window.
func (w TimeWindow) Duration() time.Duration { return w.To.Sub(w.From) }

// Contains reports whether t falls inside the window.
func (w TimeWindow) Contains(t time.Time) bool {
	return !t.Before(w.From) && t.Before(w.To)
}

// Pad widens the window on both ends, which is what you usually want when
// correlating a deploy with the alert it caused.
func (w TimeWindow) Pad(d time.Duration) TimeWindow {
	return TimeWindow{From: w.From.Add(-d), To: w.To.Add(d)}
}

func (w TimeWindow) String() string {
	return fmt.Sprintf("%s .. %s", w.From.UTC().Format(time.RFC3339), w.To.UTC().Format(time.RFC3339))
}

// SignalSource is where an observation came from.
type SignalSource string

const (
	SourceAlert  SignalSource = "alert"
	SourceMetric SignalSource = "metric"
	SourceLog    SignalSource = "log"
	SourceTrace  SignalSource = "trace"
	SourceManual SignalSource = "manual"
)

// Severity is shared by incidents and signals.
type Severity string

const (
	SeverityCritical Severity = "critical"
	SeverityHigh     Severity = "high"
	SeverityMedium   Severity = "medium"
	SeverityLow      Severity = "low"
)

// Weight returns a ranking multiplier for the severity.
func (s Severity) Weight() float64 {
	switch s {
	case SeverityCritical:
		return 1.0
	case SeverityHigh:
		return 0.8
	case SeverityMedium:
		return 0.5
	default:
		return 0.3
	}
}

// Signal is a single observation attached to a service: a firing alert, a
// metric anomaly, a log error pattern or a trace failure.
type Signal struct {
	ID          string            `json:"id"`
	Source      SignalSource      `json:"source"`
	ServiceID   string            `json:"service_id"`
	Title       string            `json:"title"`
	Description string            `json:"description,omitempty"`
	Severity    Severity          `json:"severity,omitempty"`
	ObservedAt  time.Time         `json:"observed_at"`
	Value       float64           `json:"value,omitempty"`
	Unit        string            `json:"unit,omitempty"`
	Labels      map[string]string `json:"labels,omitempty"`
}

// ChangeKind classifies a mutation of the running system. Most incidents are
// preceded by one of these, so they are first-class evidence.
type ChangeKind string

const (
	ChangeDeploy      ChangeKind = "deploy"
	ChangeConfig      ChangeKind = "config"
	ChangeFeatureFlag ChangeKind = "feature_flag"
	ChangeInfra       ChangeKind = "infra"
	ChangeScaling     ChangeKind = "scaling"
)

// Change is something that happened to a service around the incident window.
type Change struct {
	ID        string     `json:"id"`
	ServiceID string     `json:"service_id"`
	Kind      ChangeKind `json:"kind"`
	At        time.Time  `json:"at"`
	Actor     string     `json:"actor,omitempty"`
	Ref       string     `json:"ref,omitempty"`
	Summary   string     `json:"summary"`
}

// TracePath is a sampled request path through the graph, with the hop that
// produced the error marked. Traces are the only source that observes the
// dependency edges directly rather than inferring them from the catalog.
type TracePath struct {
	ServiceIDs  []string `json:"service_ids"`
	FailingHop  string   `json:"failing_hop,omitempty"`
	ErrorRate   float64  `json:"error_rate"`
	P99Millis   float64  `json:"p99_millis,omitempty"`
	SampleCount int      `json:"sample_count"`
}

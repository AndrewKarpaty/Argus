package domain

import "time"

// InvestigationStatus is the lifecycle state of one investigation run.
type InvestigationStatus string

const (
	InvestigationQueued    InvestigationStatus = "queued"
	InvestigationRunning   InvestigationStatus = "running"
	InvestigationCompleted InvestigationStatus = "completed"
	InvestigationFailed    InvestigationStatus = "failed"
)

// Terminal reports whether no further transition is expected.
func (s InvestigationStatus) Terminal() bool {
	return s == InvestigationCompleted || s == InvestigationFailed
}

// EvidenceKind labels where a piece of supporting evidence came from, so the
// UI can group it and a human can judge it quickly.
type EvidenceKind string

const (
	EvidenceSignal    EvidenceKind = "signal"
	EvidenceChange    EvidenceKind = "change"
	EvidenceTopology  EvidenceKind = "topology"
	EvidenceTrace     EvidenceKind = "trace"
	EvidenceReasoning EvidenceKind = "reasoning"
)

// Evidence is one fact that supports (or weakens) a hypothesis. Every
// hypothesis must carry evidence: an unsupported root cause is a guess, and
// the platform is explicitly not in the business of guessing.
type Evidence struct {
	Kind      EvidenceKind `json:"kind"`
	ServiceID string       `json:"service_id,omitempty"`
	Summary   string       `json:"summary"`
	Ref       string       `json:"ref,omitempty"`
	At        *time.Time   `json:"at,omitempty"`
}

// Hypothesis is a candidate root cause with a propagation path explaining how
// the fault reached the services that are actually alerting.
type Hypothesis struct {
	ID               string     `json:"id"`
	Title            string     `json:"title"`
	RootCauseService string     `json:"root_cause_service"`
	Confidence       float64    `json:"confidence"`
	Rationale        string     `json:"rationale"`
	PropagationPath  []string   `json:"propagation_path,omitempty"`
	Evidence         []Evidence `json:"evidence,omitempty"`
	SuggestedActions []string   `json:"suggested_actions,omitempty"`
	RefutedBy        string     `json:"refuted_by,omitempty"`
}

// Step is one action an investigation took. The trail is kept so that a human
// reviewer can see which data the conclusion actually rests on.
type Step struct {
	Seq        int       `json:"seq"`
	Tool       string    `json:"tool"`
	Input      string    `json:"input,omitempty"`
	Output     string    `json:"output,omitempty"`
	Error      string    `json:"error,omitempty"`
	At         time.Time `json:"at"`
	DurationMS int64     `json:"duration_ms"`
}

// TokenUsage records what a model-backed run cost.
type TokenUsage struct {
	InputTokens      int64 `json:"input_tokens"`
	OutputTokens     int64 `json:"output_tokens"`
	CacheReadTokens  int64 `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens int64 `json:"cache_write_tokens,omitempty"`
}

// Analysis is what a Reasoner produces: ranked hypotheses plus the trail that
// led to them.
type Analysis struct {
	Summary    string       `json:"summary"`
	Hypotheses []Hypothesis `json:"hypotheses"`
	Steps      []Step       `json:"steps,omitempty"`
	Usage      *TokenUsage  `json:"usage,omitempty"`
}

// Investigation is one persisted run of a Reasoner against an incident.
type Investigation struct {
	ID         string              `json:"id"`
	IncidentID string              `json:"incident_id"`
	Status     InvestigationStatus `json:"status"`
	Engine     string              `json:"engine"`
	Summary    string              `json:"summary,omitempty"`
	Hypotheses []Hypothesis        `json:"hypotheses,omitempty"`
	Steps      []Step              `json:"steps,omitempty"`
	Usage      *TokenUsage         `json:"usage,omitempty"`
	Error      string              `json:"error,omitempty"`
	StartedAt  time.Time           `json:"started_at"`
	FinishedAt *time.Time          `json:"finished_at,omitempty"`
}

// TopHypothesis returns the highest-confidence hypothesis, if any.
func (iv Investigation) TopHypothesis() (Hypothesis, bool) {
	best := -1.0
	var out Hypothesis
	for _, h := range iv.Hypotheses {
		if h.Confidence > best {
			best, out = h.Confidence, h
		}
	}
	return out, best >= 0
}

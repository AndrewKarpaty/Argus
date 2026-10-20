package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/toolrunner"

	"github.com/AndrewKarpaty/Argus/internal/domain"
	"github.com/AndrewKarpaty/Argus/internal/graph"
	"github.com/AndrewKarpaty/Argus/internal/investigator"
)

// maxStepPayload bounds what is stored in the audit trail per step. The model
// sees the full result; the stored trail only has to be readable.
const maxStepPayload = 4000

// session is the per-investigation state behind the tools: the graph and
// telemetry the model may read, the trail of what it actually read, and the
// findings it reports at the end.
type session struct {
	in        investigator.Input
	log       *slog.Logger
	window    domain.TimeWindow
	mu        sync.Mutex
	steps     []domain.Step
	findings  *reportInput
	reportSeq int
}

func newSession(in investigator.Input, log *slog.Logger) *session {
	return &session{in: in, log: log, window: in.Window()}
}

// record appends one tool call to the audit trail.
func (s *session) record(tool string, input any, output string, err error, started time.Time) {
	inJSON, _ := json.Marshal(input)
	step := domain.Step{
		Tool:       tool,
		Input:      truncate(string(inJSON), maxStepPayload),
		Output:     truncate(output, maxStepPayload),
		At:         started.UTC(),
		DurationMS: time.Since(started).Milliseconds(),
	}
	if err != nil {
		step.Error = err.Error()
	}
	s.mu.Lock()
	step.Seq = len(s.steps) + 1
	s.steps = append(s.steps, step)
	s.mu.Unlock()

	if s.log != nil {
		s.log.Debug("agent tool call", "tool", tool, "input", step.Input,
			"duration_ms", step.DurationMS, "error", step.Error)
	}
}

// Steps returns the audit trail collected so far.
func (s *session) Steps() []domain.Step {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]domain.Step, len(s.steps))
	copy(out, s.steps)
	return out
}

// Findings returns what the model reported, if it reported anything.
func (s *session) Findings() (reportInput, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.findings == nil {
		return reportInput{}, false
	}
	return *s.findings, true
}

// --- tool inputs -----------------------------------------------------------

type topologyInput struct {
	ServiceIDs []string `json:"service_ids" jsonschema:"required,description=Service ids to centre the view on. Use the affected services if unsure."`
	Radius     int      `json:"radius,omitempty" jsonschema:"description=How many hops of neighbours to include. Defaults to 2, maximum 4."`
}

type dependenciesInput struct {
	ServiceID string `json:"service_id" jsonschema:"required,description=The service to inspect."`
	Direction string `json:"direction" jsonschema:"required,description=Either 'dependencies' (what this service calls - where its failures come from) or 'dependents' (who calls it - what breaks when it fails)."`
	MaxDepth  int    `json:"max_depth,omitempty" jsonschema:"description=Hop limit. Defaults to 3, 0 means unbounded."`
}

type blastRadiusInput struct {
	ServiceID string `json:"service_id" jsonschema:"required,description=The service assumed to be unhealthy."`
}

type suspectsInput struct {
	ServiceIDs []string `json:"service_ids" jsonschema:"required,description=The services showing symptoms. Pass all of them at once: the ranking is based on which upstream nodes explain the whole set."`
	MaxDepth   int      `json:"max_depth,omitempty" jsonschema:"description=How far upstream to look. Defaults to 4."`
}

type pathInput struct {
	From string `json:"from" jsonschema:"required,description=The calling service, usually one that is alerting."`
	To   string `json:"to" jsonschema:"required,description=The candidate root cause."`
}

type telemetryInput struct {
	ServiceID  string `json:"service_id" jsonschema:"required,description=The service to pull evidence for."`
	PadMinutes int    `json:"pad_minutes,omitempty" jsonschema:"description=Widen the incident window by this many minutes on both sides. Defaults to 5, maximum 120."`
}

type evidenceInput struct {
	Kind      string `json:"kind" jsonschema:"required,description=One of: signal, change, topology, trace, reasoning."`
	ServiceID string `json:"service_id,omitempty" jsonschema:"description=The service this evidence is about."`
	Summary   string `json:"summary" jsonschema:"required,description=What the evidence says, with the concrete numbers from the tool result it came from."`
	Ref       string `json:"ref,omitempty" jsonschema:"description=Signal id, change ref or other identifier."`
}

type hypothesisInput struct {
	Title            string          `json:"title" jsonschema:"required,description=One line naming the fault, e.g. 'Connection pool exhaustion on postgres-main'."`
	RootCauseService string          `json:"root_cause_service" jsonschema:"required,description=The exact catalog service id of the root cause."`
	Confidence       float64         `json:"confidence" jsonschema:"required,description=Between 0 and 1, calibrated as instructed."`
	Rationale        string          `json:"rationale" jsonschema:"required,description=Why this explains every affected service, and which alternatives you ruled out and why."`
	PropagationPath  []string        `json:"propagation_path,omitempty" jsonschema:"description=Service ids from an affected service to the root cause, in call order."`
	Evidence         []evidenceInput `json:"evidence" jsonschema:"required,description=The facts this rests on. At least one item."`
	SuggestedActions []string        `json:"suggested_actions,omitempty" jsonschema:"description=Concrete next steps for the on-call engineer, most useful first."`
}

type reportInput struct {
	Summary    string            `json:"summary" jsonschema:"required,description=Two or three sentences an on-call engineer can read in ten seconds: what broke, how it reached the alerting services, what to do first."`
	Hypotheses []hypothesisInput `json:"hypotheses" jsonschema:"required,description=One to three hypotheses, highest confidence first."`
}

// --- tool construction -----------------------------------------------------

// tools returns the tool surface handed to the model. The graph tools are the
// reason this platform exists: they let the model reason about topology
// instead of pattern-matching over log text.
func (s *session) tools() ([]anthropic.BetaTool, error) {
	var tools []anthropic.BetaTool
	add := func(t anthropic.BetaTool, err error) error {
		if err != nil {
			return err
		}
		tools = append(tools, t)
		return nil
	}

	if err := add(toolrunner.NewBetaToolFromJSONSchema(
		"get_service_topology",
		"Describe the dependency neighbourhood around one or more services: their metadata and the edges between them. Use it to learn the exact catalog service ids.",
		s.getServiceTopology,
	)); err != nil {
		return nil, err
	}
	if err := add(toolrunner.NewBetaToolFromJSONSchema(
		"list_dependencies",
		"List what a service transitively depends on, or who transitively depends on it, with hop distance, transport and criticality.",
		s.listDependencies,
	)); err != nil {
		return nil, err
	}
	if err := add(toolrunner.NewBetaToolFromJSONSchema(
		"blast_radius",
		"List every service that is at risk while the given service is unhealthy. Use it to check that a candidate root cause would hit exactly the services that are actually alerting - no more, no fewer.",
		s.blastRadius,
	)); err != nil {
		return nil, err
	}
	if err := add(toolrunner.NewBetaToolFromJSONSchema(
		"rank_suspects",
		"Rank the shared upstream dependencies of a set of symptomatic services by how much of the symptom set each one explains. The right first call for a multi-service incident.",
		s.rankSuspects,
	)); err != nil {
		return nil, err
	}
	if err := add(toolrunner.NewBetaToolFromJSONSchema(
		"dependency_path",
		"Return the shortest call path from one service to another, to verify that a propagation route you want to claim actually exists.",
		s.dependencyPath,
	)); err != nil {
		return nil, err
	}
	if err := add(toolrunner.NewBetaToolFromJSONSchema(
		"get_telemetry",
		"Pull the evidence for one service inside the incident window: metric anomalies, aggregated log error patterns, failing trace paths and recent deploys or config changes.",
		s.getTelemetry,
	)); err != nil {
		return nil, err
	}
	if err := add(toolrunner.NewBetaToolFromJSONSchema(
		"report_findings",
		"Submit the final ranked hypotheses. Call this exactly once, at the end. Anything not included here is discarded.",
		s.reportFindings,
	)); err != nil {
		return nil, err
	}
	return tools, nil
}

// --- tool handlers ---------------------------------------------------------

func (s *session) getServiceTopology(_ context.Context, in topologyInput) (anthropic.BetaToolResultBlockParamContentUnion, error) {
	started := time.Now()
	radius := clampInt(in.Radius, 1, 4, 2)
	sub := s.in.Graph.Subgraph(in.ServiceIDs, radius)
	services, deps := sub.Size()
	if services == 0 {
		err := fmt.Errorf("none of %v are in the catalog; known ids include %s",
			in.ServiceIDs, sampleIDs(s.in.Graph, 15))
		s.record("get_service_topology", in, "", err, started)
		return text(""), err
	}
	out := fmt.Sprintf("%d services, %d dependencies within %d hop(s):\n\n%s",
		services, deps, radius, sub.Describe())
	s.record("get_service_topology", in, out, nil, started)
	return text(out), nil
}

func (s *session) listDependencies(_ context.Context, in dependenciesInput) (anthropic.BetaToolResultBlockParamContentUnion, error) {
	started := time.Now()
	dir := graph.Direction(in.Direction)
	if !dir.Valid() {
		err := fmt.Errorf("direction must be %q or %q, got %q", graph.Dependencies, graph.Dependents, in.Direction)
		s.record("list_dependencies", in, "", err, started)
		return text(""), err
	}
	if err := s.requireService(in.ServiceID); err != nil {
		s.record("list_dependencies", in, "", err, started)
		return text(""), err
	}
	depth := clampInt(in.MaxDepth, 0, 10, 3)
	hops := s.in.Graph.Traverse(in.ServiceID, dir, depth)
	out := s.renderHops(fmt.Sprintf("%s of %s (max depth %d)", dir, in.ServiceID, depth), hops)
	s.record("list_dependencies", in, out, nil, started)
	return text(out), nil
}

func (s *session) blastRadius(_ context.Context, in blastRadiusInput) (anthropic.BetaToolResultBlockParamContentUnion, error) {
	started := time.Now()
	if err := s.requireService(in.ServiceID); err != nil {
		s.record("blast_radius", in, "", err, started)
		return text(""), err
	}
	hops := s.in.Graph.BlastRadius(in.ServiceID)
	header := fmt.Sprintf("services at risk while %s is unhealthy", in.ServiceID)
	out := s.renderHops(header, hops)

	// Point out the mismatch explicitly: the model should notice when a
	// candidate cannot explain who is actually alerting.
	affected := map[string]bool{}
	for _, id := range s.in.Incident.AffectedServices {
		affected[id] = true
	}
	var covered, missing []string
	for _, h := range hops {
		if affected[h.ServiceID] {
			covered = append(covered, h.ServiceID)
		}
	}
	for _, id := range s.in.Incident.AffectedServices {
		if id == in.ServiceID {
			covered = append(covered, id)
			continue
		}
		found := false
		for _, h := range hops {
			if h.ServiceID == id {
				found = true
				break
			}
		}
		if !found {
			missing = append(missing, id)
		}
	}
	out += fmt.Sprintf("\nAffected services inside this radius: %v\nAffected services NOT explained by it: %v\n", covered, missing)

	s.record("blast_radius", in, out, nil, started)
	return text(out), nil
}

func (s *session) rankSuspects(_ context.Context, in suspectsInput) (anthropic.BetaToolResultBlockParamContentUnion, error) {
	started := time.Now()
	opts := graph.DefaultSuspectOptions()
	if in.MaxDepth > 0 {
		opts.MaxDepth = clampInt(in.MaxDepth, 1, 10, 4)
	}
	suspects := s.in.Graph.Suspects(in.ServiceIDs, opts)
	if len(suspects) == 0 {
		err := fmt.Errorf("none of %v are in the catalog; known ids include %s",
			in.ServiceIDs, sampleIDs(s.in.Graph, 15))
		s.record("rank_suspects", in, "", err, started)
		return text(""), err
	}
	out := jsonResult(map[string]any{
		"affected": in.ServiceIDs,
		"note":     "coverage is the fraction of the affected services that depend on the candidate; score also weighs hop distance, dependency criticality, tier and node kind. This is a plausibility ranking, not a verdict - verify with get_telemetry.",
		"suspects": suspects,
	})
	s.record("rank_suspects", in, out, nil, started)
	return text(out), nil
}

func (s *session) dependencyPath(_ context.Context, in pathInput) (anthropic.BetaToolResultBlockParamContentUnion, error) {
	started := time.Now()
	for _, id := range []string{in.From, in.To} {
		if err := s.requireService(id); err != nil {
			s.record("dependency_path", in, "", err, started)
			return text(""), err
		}
	}
	path := s.in.Graph.Path(in.From, in.To)
	var out string
	if len(path) == 0 {
		out = fmt.Sprintf("No call path from %s to %s. %s does not depend on %s, so a fault in %s cannot explain symptoms in %s.",
			in.From, in.To, in.From, in.To, in.To, in.From)
	} else {
		out = fmt.Sprintf("%s (%d hops)", strings.Join(path, " -> "), len(path)-1)
	}
	s.record("dependency_path", in, out, nil, started)
	return text(out), nil
}

func (s *session) getTelemetry(ctx context.Context, in telemetryInput) (anthropic.BetaToolResultBlockParamContentUnion, error) {
	started := time.Now()
	if err := s.requireService(in.ServiceID); err != nil {
		s.record("get_telemetry", in, "", err, started)
		return text(""), err
	}
	if s.in.Collector == nil {
		out := "No telemetry backend is configured in this deployment. Reason from topology and the symptoms in the incident report, and keep confidence low."
		s.record("get_telemetry", in, out, nil, started)
		return text(out), nil
	}
	pad := time.Duration(clampInt(in.PadMinutes, 0, 120, 5)) * time.Minute
	window := s.window.Pad(pad)
	bundle := s.in.Collector.Gather(ctx, []string{in.ServiceID}, window)

	out := jsonResult(map[string]any{
		"service":         in.ServiceID,
		"window":          window.String(),
		"signals":         bundle.Signals,
		"changes":         bundle.Changes,
		"paths":           bundle.Paths,
		"provider_errors": bundle.Errors,
		"note":            "an empty result means no evidence was found for this service in this window, which is itself informative: it weakens the case for it being the root cause.",
	})
	s.record("get_telemetry", in, out, nil, started)
	return text(out), nil
}

// reportFindings captures the answer. It validates service ids so the model
// cannot blame a service that does not exist, and bounces anything malformed
// back as a tool error for the model to correct.
func (s *session) reportFindings(_ context.Context, in reportInput) (anthropic.BetaToolResultBlockParamContentUnion, error) {
	started := time.Now()
	if len(in.Hypotheses) == 0 {
		err := fmt.Errorf("at least one hypothesis is required")
		s.record("report_findings", in, "", err, started)
		return text(""), err
	}
	for i, h := range in.Hypotheses {
		if !s.in.Graph.Has(h.RootCauseService) {
			err := fmt.Errorf("hypothesis %d: %q is not a catalog service id; known ids include %s",
				i+1, h.RootCauseService, sampleIDs(s.in.Graph, 15))
			s.record("report_findings", in, "", err, started)
			return text(""), err
		}
		if len(h.Evidence) == 0 {
			err := fmt.Errorf("hypothesis %d (%s): evidence is required - an unsupported root cause is a guess",
				i+1, h.RootCauseService)
			s.record("report_findings", in, "", err, started)
			return text(""), err
		}
	}

	s.mu.Lock()
	captured := in
	s.findings = &captured
	s.reportSeq++
	repeat := s.reportSeq
	s.mu.Unlock()

	out := fmt.Sprintf("Recorded %d hypothesis/hypotheses. Stop here; do not call any more tools.", len(in.Hypotheses))
	if repeat > 1 {
		out = fmt.Sprintf("Replaced the previous findings with %d hypothesis/hypotheses. Stop here.", len(in.Hypotheses))
	}
	s.record("report_findings", in, out, nil, started)
	return text(out), nil
}

// --- helpers ---------------------------------------------------------------

func (s *session) requireService(id string) error {
	if s.in.Graph.Has(id) {
		return nil
	}
	return fmt.Errorf("%q is not a catalog service id; known ids include %s", id, sampleIDs(s.in.Graph, 15))
}

func (s *session) renderHops(header string, hops []graph.Hop) string {
	if len(hops) == 0 {
		return header + ": none"
	}
	type row struct {
		ServiceID   string `json:"service_id"`
		Kind        string `json:"kind"`
		Tier        string `json:"tier"`
		Depth       int    `json:"depth"`
		Via         string `json:"via"`
		Transport   string `json:"transport"`
		Criticality string `json:"criticality"`
		HardPath    bool   `json:"hard_path"`
	}
	rows := make([]row, 0, len(hops))
	for _, h := range hops {
		svc, _ := s.in.Graph.Service(h.ServiceID)
		rows = append(rows, row{
			ServiceID:   h.ServiceID,
			Kind:        string(svc.Kind),
			Tier:        string(svc.Tier),
			Depth:       h.Depth,
			Via:         h.Via,
			Transport:   string(h.Kind),
			Criticality: string(h.Criticality),
			HardPath:    h.HardPath,
		})
	}
	return header + ":\n" + jsonResult(rows)
}

func text(s string) anthropic.BetaToolResultBlockParamContentUnion {
	return anthropic.BetaToolResultBlockParamContentUnion{
		OfText: &anthropic.BetaTextBlockParam{Text: s},
	}
}

func jsonResult(v any) string {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Sprintf("could not encode result: %v", err)
	}
	return string(b)
}

func clampInt(v, min, max, fallback int) int {
	if v == 0 {
		return fallback
	}
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

func sampleIDs(g *graph.Graph, n int) string {
	services := g.Services()
	ids := make([]string, 0, n)
	for i, s := range services {
		if i >= n {
			ids = append(ids, "...")
			break
		}
		ids = append(ids, s.ID)
	}
	return strings.Join(ids, ", ")
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + fmt.Sprintf("... [truncated, %d bytes total]", len(s))
}

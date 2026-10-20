package investigator

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/AndrewKarpaty/Argus/internal/domain"
	"github.com/AndrewKarpaty/Argus/internal/graph"
	"github.com/AndrewKarpaty/Argus/internal/telemetry"
)

// Heuristic is a deterministic, model-free Reasoner. It ranks the shared
// upstream dependencies of the affected services and then weighs each
// candidate against the telemetry in the incident window.
//
// It exists for three reasons: it is the fallback when no model credentials
// are configured, it is the baseline the model-driven engine has to beat, and
// it makes the whole pipeline testable without a network call.
type Heuristic struct {
	// Suspects tunes the topology search.
	Suspects graph.SuspectOptions
	// MaxHypotheses caps the returned list.
	MaxHypotheses int
	// Now overrides the clock, for reproducible tests.
	Now func() time.Time
}

// NewHeuristic returns a Heuristic with sensible defaults.
func NewHeuristic() *Heuristic {
	return &Heuristic{Suspects: graph.DefaultSuspectOptions(), MaxHypotheses: 3}
}

// Name identifies the engine.
func (h *Heuristic) Name() string { return "heuristic" }

func (h *Heuristic) now() time.Time {
	if h.Now != nil {
		return h.Now()
	}
	return time.Now().UTC()
}

// Analyze implements Reasoner.
func (h *Heuristic) Analyze(ctx context.Context, in Input) (domain.Analysis, error) {
	opts := h.Suspects
	if opts.MaxDepth == 0 && opts.Limit == 0 {
		opts = graph.DefaultSuspectOptions()
	}
	maxHypotheses := h.MaxHypotheses
	if maxHypotheses <= 0 {
		maxHypotheses = 3
	}

	var steps []domain.Step
	record := func(tool, input, output string, started time.Time) {
		steps = append(steps, domain.Step{
			Seq:        len(steps) + 1,
			Tool:       tool,
			Input:      input,
			Output:     output,
			At:         started.UTC(),
			DurationMS: time.Since(started).Milliseconds(),
		})
	}

	// 1. Rank the shared upstream dependencies of the affected services.
	t0 := h.now()
	suspects := in.Graph.Suspects(in.Incident.AffectedServices, opts)
	record("graph.suspects",
		strings.Join(in.Incident.AffectedServices, ","),
		fmt.Sprintf("%d candidates", len(suspects)), t0)
	if len(suspects) == 0 {
		return domain.Analysis{
			Summary: fmt.Sprintf("None of the affected services (%s) are in the catalog, so no topology reasoning was possible. Add them to the catalog and re-run.",
				strings.Join(in.Incident.AffectedServices, ", ")),
			Steps: steps,
		}, nil
	}

	// 2. Pull telemetry for the affected services and the live candidates.
	window := in.Window()
	probe := candidateIDs(in.Incident.AffectedServices, suspects, maxHypotheses*2)
	var bundle telemetry.Bundle
	if in.Collector != nil {
		t1 := h.now()
		bundle = in.Collector.Gather(ctx, probe, window.Pad(5*time.Minute))
		record("telemetry.gather",
			fmt.Sprintf("%d services, window %s", len(probe), window),
			fmt.Sprintf("%d signals, %d changes, %d trace paths, %d provider errors",
				len(bundle.Signals), len(bundle.Changes), len(bundle.Paths), len(bundle.Errors)), t1)
	}
	if err := ctx.Err(); err != nil {
		return domain.Analysis{Steps: steps}, err
	}

	// 3. Turn the ranked candidates into hypotheses with evidence attached.
	t2 := h.now()
	top := suspects
	if len(top) > maxHypotheses {
		top = top[:maxHypotheses]
	}
	maxScore := suspects[0].Score
	if maxScore <= 0 {
		maxScore = 1
	}

	hypotheses := make([]domain.Hypothesis, 0, len(top))
	for _, s := range top {
		hypotheses = append(hypotheses, h.hypothesis(in, s, bundle, maxScore))
	}
	sort.SliceStable(hypotheses, func(i, j int) bool {
		return hypotheses[i].Confidence > hypotheses[j].Confidence
	})
	record("rank.hypotheses", fmt.Sprintf("%d candidates", len(top)),
		fmt.Sprintf("%d hypotheses", len(hypotheses)), t2)

	return domain.Analysis{
		Summary:    h.summary(in.Incident, hypotheses, bundle),
		Hypotheses: hypotheses,
		Steps:      steps,
	}, nil
}

// hypothesis builds one hypothesis and calibrates its confidence from the
// evidence actually found, so that a topologically plausible candidate with no
// supporting telemetry stays visibly uncertain.
func (h *Heuristic) hypothesis(in Input, s graph.Suspect, bundle telemetry.Bundle, maxScore float64) domain.Hypothesis {
	svc, _ := in.Graph.Service(s.ServiceID)
	name := svc.Name
	if name == "" {
		name = s.ServiceID
	}

	// Topology alone never justifies high confidence.
	confidence := 0.2 + 0.35*(s.Score/maxScore)
	evidence := []domain.Evidence{{
		Kind:      domain.EvidenceTopology,
		ServiceID: s.ServiceID,
		Summary: fmt.Sprintf("Explains %d of %d affected services (%s); %s",
			len(s.Explains), len(in.Incident.AffectedServices),
			strings.Join(s.Explains, ", "), s.Reason),
	}}

	// A change in the window is the strongest single signal there is.
	for _, c := range bundle.ChangesFor(s.ServiceID) {
		at := c.At
		confidence += 0.22
		evidence = append(evidence, domain.Evidence{
			Kind:      domain.EvidenceChange,
			ServiceID: c.ServiceID,
			Summary:   fmt.Sprintf("%s %s by %s: %s", c.Kind, c.Ref, c.Actor, c.Summary),
			Ref:       c.Ref,
			At:        &at,
		})
		break // one change is enough to move the needle; list the rest as-is
	}

	// Severe symptoms on the candidate itself.
	for _, sig := range bundle.SignalsFor(s.ServiceID) {
		switch sig.Severity {
		case domain.SeverityCritical:
			confidence += 0.12
		case domain.SeverityHigh:
			confidence += 0.06
		}
		at := sig.ObservedAt
		evidence = append(evidence, domain.Evidence{
			Kind:      domain.EvidenceSignal,
			ServiceID: sig.ServiceID,
			Summary:   fmt.Sprintf("[%s] %s", sig.Source, sig.Title),
			Ref:       sig.ID,
			At:        &at,
		})
	}

	// Traces that fail at this hop observe the propagation directly.
	for _, p := range bundle.Paths {
		if p.FailingHop != s.ServiceID {
			continue
		}
		confidence += 0.1
		evidence = append(evidence, domain.Evidence{
			Kind:      domain.EvidenceTrace,
			ServiceID: s.ServiceID,
			Summary: fmt.Sprintf("Traces through %s fail at this hop (error rate %.0f%%, %d samples)",
				strings.Join(p.ServiceIDs, " -> "), p.ErrorRate*100, p.SampleCount),
		})
		break
	}

	if confidence > 0.95 {
		confidence = 0.95
	}

	return domain.Hypothesis{
		ID:               domain.NewID("hyp"),
		Title:            h.title(svc, s),
		RootCauseService: s.ServiceID,
		Confidence:       round2(confidence),
		Rationale:        h.rationale(in, s, bundle, name),
		PropagationPath:  h.path(in, s),
		Evidence:         evidence,
		SuggestedActions: h.actions(in, s, bundle, svc),
	}
}

func (h *Heuristic) title(svc domain.Service, s graph.Suspect) string {
	name := svc.Name
	if name == "" {
		name = s.ServiceID
	}
	switch {
	case s.Affected && len(s.Explains) == 1:
		return fmt.Sprintf("%s is failing on its own", name)
	case svc.Kind == domain.KindExternal:
		return fmt.Sprintf("%s (external provider) is degraded", name)
	case svc.Kind == domain.KindDatabase || svc.Kind == domain.KindCache || svc.Kind == domain.KindQueue:
		return fmt.Sprintf("Shared %s %s is the common-mode failure", svc.Kind, name)
	default:
		return fmt.Sprintf("%s is the upstream root cause", name)
	}
}

func (h *Heuristic) rationale(in Input, s graph.Suspect, bundle telemetry.Bundle, name string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s is upstream of %d of the %d affected services (%s), %s.",
		name, len(s.Explains), len(in.Incident.AffectedServices),
		strings.Join(s.Explains, ", "), depthPhrase(s))
	if !s.HardPath {
		b.WriteString(" The path runs over soft dependencies only, so it can degrade the callers but is unlikely to take them down entirely.")
	}
	if changes := bundle.ChangesFor(s.ServiceID); len(changes) > 0 {
		c := changes[0]
		fmt.Fprintf(&b, " A %s landed on it at %s (%s), inside the incident window.",
			c.Kind, c.At.UTC().Format(time.RFC3339), c.Ref)
	}
	if sigs := bundle.SignalsFor(s.ServiceID); len(sigs) > 0 {
		fmt.Fprintf(&b, " Its own telemetry shows: %s.", sigs[0].Title)
	} else if in.Collector != nil {
		b.WriteString(" No telemetry of its own was found in the window, which weakens the case.")
	}
	return b.String()
}

func depthPhrase(s graph.Suspect) string {
	switch s.MinDepth {
	case 0:
		return "and is itself reporting symptoms"
	case 1:
		return "as a direct dependency"
	default:
		return fmt.Sprintf("%d hops upstream", s.MinDepth)
	}
}

// path returns the propagation route from an affected service down to the
// candidate, which is what an operator actually needs to see.
func (h *Heuristic) path(in Input, s graph.Suspect) []string {
	for _, affected := range in.Incident.AffectedServices {
		if p := in.Graph.Path(affected, s.ServiceID); len(p) > 0 {
			return p
		}
	}
	return nil
}

func (h *Heuristic) actions(in Input, s graph.Suspect, bundle telemetry.Bundle, svc domain.Service) []string {
	var out []string
	if changes := bundle.ChangesFor(s.ServiceID); len(changes) > 0 {
		out = append(out, fmt.Sprintf("Roll back %s on %s and watch whether the affected services recover", changes[0].Ref, s.ServiceID))
	}
	switch svc.Kind {
	case domain.KindDatabase:
		out = append(out, fmt.Sprintf("Check %s for connection saturation, lock waits and slow queries", s.ServiceID))
	case domain.KindCache:
		out = append(out, fmt.Sprintf("Check %s for evictions, memory pressure and hit-rate collapse", s.ServiceID))
	case domain.KindQueue:
		out = append(out, fmt.Sprintf("Check %s for consumer lag and partition skew", s.ServiceID))
	case domain.KindExternal:
		out = append(out, fmt.Sprintf("Check the %s status page and current error budget, then consider failing open", s.ServiceID))
	default:
		out = append(out, fmt.Sprintf("Check %s saturation, recent config changes and dependency timeouts", s.ServiceID))
	}
	if blast := in.Graph.BlastRadius(s.ServiceID); len(blast) > 0 {
		ids := make([]string, 0, len(blast))
		for _, hop := range blast {
			ids = append(ids, hop.ServiceID)
		}
		if len(ids) > 6 {
			ids = append(ids[:6], "...")
		}
		out = append(out, fmt.Sprintf("Expect impact on: %s", strings.Join(ids, ", ")))
	}
	if svc.Runbook != "" {
		out = append(out, "Runbook: "+svc.Runbook)
	}
	return out
}

func (h *Heuristic) summary(inc domain.Incident, hypotheses []domain.Hypothesis, bundle telemetry.Bundle) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d service(s) affected (%s) in window %s. ",
		len(inc.AffectedServices), strings.Join(inc.AffectedServices, ", "), inc.Window())
	if len(hypotheses) == 0 {
		b.WriteString("No candidate root cause could be derived from the topology.")
		return b.String()
	}
	top := hypotheses[0]
	fmt.Fprintf(&b, "Most likely root cause: %s (confidence %.2f). %s",
		top.RootCauseService, top.Confidence, top.Title+".")
	if len(top.PropagationPath) > 1 {
		fmt.Fprintf(&b, " Propagation: %s.", strings.Join(top.PropagationPath, " -> "))
	}
	if len(hypotheses) > 1 {
		alts := make([]string, 0, len(hypotheses)-1)
		for _, hyp := range hypotheses[1:] {
			alts = append(alts, fmt.Sprintf("%s (%.2f)", hyp.RootCauseService, hyp.Confidence))
		}
		fmt.Fprintf(&b, " Alternatives: %s.", strings.Join(alts, ", "))
	}
	if len(bundle.Errors) > 0 {
		fmt.Fprintf(&b, " Note: %d telemetry provider error(s), evidence may be incomplete.", len(bundle.Errors))
	}
	return b.String()
}

// candidateIDs is the de-duplicated list of services worth pulling telemetry
// for: the affected ones plus the strongest candidates.
func candidateIDs(affected []string, suspects []graph.Suspect, limit int) []string {
	seen := make(map[string]bool, len(affected)+limit)
	out := make([]string, 0, len(affected)+limit)
	add := func(id string) {
		if id == "" || seen[id] {
			return
		}
		seen[id] = true
		out = append(out, id)
	}
	for _, id := range affected {
		add(id)
	}
	for i, s := range suspects {
		if i >= limit {
			break
		}
		add(s.ServiceID)
	}
	return out
}

func round2(f float64) float64 { return float64(int64(f*100+0.5)) / 100 }

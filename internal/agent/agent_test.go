package agent_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AndrewKarpaty/Argus/internal/agent"
	"github.com/AndrewKarpaty/Argus/internal/catalog"
	"github.com/AndrewKarpaty/Argus/internal/domain"
	"github.com/AndrewKarpaty/Argus/internal/investigator"
	"github.com/AndrewKarpaty/Argus/internal/telemetry"
	"github.com/AndrewKarpaty/Argus/internal/telemetry/demo"
)

// fakeAPI serves a scripted sequence of Messages API responses, so the whole
// agent loop - tool schemas, tool execution, findings capture, usage
// accounting - is exercised without calling the real API.
type fakeAPI struct {
	t         *testing.T
	responses []string
	calls     int
	bodies    []map[string]any
}

func (f *fakeAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		f.t.Errorf("read request: %v", err)
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		f.t.Errorf("request is not JSON: %v", err)
	}
	f.bodies = append(f.bodies, body)

	if f.calls >= len(f.responses) {
		f.t.Errorf("unexpected request %d, only %d responses scripted", f.calls+1, len(f.responses))
		http.Error(w, "no more responses", http.StatusInternalServerError)
		return
	}
	resp := f.responses[f.calls]
	f.calls++
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, resp)
}

func message(id string, stopReason string, content string) string {
	return fmt.Sprintf(`{
		"id": %q,
		"type": "message",
		"role": "assistant",
		"model": "claude-opus-5-5",
		"content": [%s],
		"stop_reason": %q,
		"usage": {"input_tokens": 1000, "output_tokens": 50, "cache_read_input_tokens": 900}
	}`, id, content, stopReason)
}

func toolUse(id, name string, input any) string {
	raw, err := json.Marshal(input)
	if err != nil {
		panic(err)
	}
	return fmt.Sprintf(`{"type": "tool_use", "id": %q, "name": %q, "input": %s}`, id, name, raw)
}

func newInput(t *testing.T, affected ...string) investigator.Input {
	t.Helper()
	prov, err := catalog.NewProvider(context.Background(),
		catalog.NewFile(filepath.Join("..", "..", "configs", "services.yaml")), nil)
	if err != nil {
		t.Fatalf("catalog: %v", err)
	}
	tel := demo.New("postgres-main")
	now := time.Now().UTC()
	inc := domain.Incident{
		ID:               "inc_test",
		Title:            "Checkout and orders failing",
		Severity:         domain.SeverityCritical,
		AffectedServices: affected,
		StartedAt:        now.Add(-20 * time.Minute),
		DetectedAt:       now,
	}
	if err := inc.Validate(); err != nil {
		t.Fatalf("incident: %v", err)
	}
	return investigator.Input{
		Incident:  inc,
		Graph:     prov.Graph(),
		Collector: &telemetry.Collector{Metrics: tel, Logs: tel, Traces: tel, Changes: tel},
	}
}

func newClaude(t *testing.T, fake *fakeAPI) *agent.Claude {
	t.Helper()
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	c, err := agent.New(agent.Config{
		APIKey:        "test-key",
		BaseURL:       srv.URL,
		MaxIterations: 5,
	})
	if err != nil {
		t.Fatalf("agent.New: %v", err)
	}
	return c
}

func TestAgentLoopExecutesToolsAndCapturesFindings(t *testing.T) {
	report := map[string]any{
		"summary": "postgres-main ran out of connections after a pool-size deploy; checkout and orders fail on every query.",
		"hypotheses": []map[string]any{{
			"title":              "Connection pool exhaustion on postgres-main",
			"root_cause_service": "postgres-main",
			"confidence":         0.88,
			"rationale":          "Both affected services depend on it directly and a deploy landed inside the window.",
			"propagation_path":   []string{"checkout", "postgres-main"},
			"evidence": []map[string]any{
				{"kind": "change", "service_id": "postgres-main", "summary": "deploy v2026.10.3 raised pool size"},
				{"kind": "signal", "service_id": "postgres-main", "summary": "pool saturation at 98%"},
			},
			"suggested_actions": []string{"Roll back v2026.10.3"},
		}},
	}
	fake := &fakeAPI{t: t, responses: []string{
		message("msg_1", "tool_use", toolUse("toolu_1", "rank_suspects", map[string]any{
			"service_ids": []string{"checkout", "orders"},
		})),
		message("msg_2", "tool_use", toolUse("toolu_2", "get_telemetry", map[string]any{
			"service_id": "postgres-main",
		})),
		message("msg_3", "tool_use", toolUse("toolu_3", "report_findings", report)),
		message("msg_4", "end_turn", `{"type": "text", "text": "Reported."}`),
	}}

	analysis, err := newClaude(t, fake).Analyze(context.Background(), newInput(t, "checkout", "orders"))
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}

	if len(analysis.Hypotheses) != 1 {
		t.Fatalf("got %d hypotheses, want 1", len(analysis.Hypotheses))
	}
	h := analysis.Hypotheses[0]
	if h.RootCauseService != "postgres-main" || h.Confidence != 0.88 {
		t.Errorf("hypothesis = %s / %v", h.RootCauseService, h.Confidence)
	}
	if h.ID == "" {
		t.Error("hypothesis should be assigned an id")
	}
	if len(h.Evidence) != 2 || h.Evidence[0].Kind != domain.EvidenceChange {
		t.Errorf("evidence = %+v", h.Evidence)
	}
	if !strings.Contains(analysis.Summary, "postgres-main") {
		t.Errorf("summary = %q", analysis.Summary)
	}

	// Every tool call must land in the audit trail, in order.
	var tools []string
	for _, s := range analysis.Steps {
		tools = append(tools, s.Tool)
	}
	want := []string{"rank_suspects", "get_telemetry", "report_findings"}
	if len(tools) != len(want) {
		t.Fatalf("trail = %v, want %v", tools, want)
	}
	for i := range want {
		if tools[i] != want[i] || analysis.Steps[i].Seq != i+1 {
			t.Errorf("step %d = %s (seq %d), want %s", i, tools[i], analysis.Steps[i].Seq, want[i])
		}
	}
	if analysis.Steps[0].Output == "" {
		t.Error("the rank_suspects result should be recorded in the trail")
	}

	// Usage is summed across all four turns, not just the last one.
	if analysis.Usage == nil || analysis.Usage.InputTokens != 4000 || analysis.Usage.OutputTokens != 200 {
		t.Errorf("usage = %+v, want 4000 in / 200 out across 4 turns", analysis.Usage)
	}
	if analysis.Usage.CacheReadTokens != 3600 {
		t.Errorf("cache read tokens = %d, want 3600", analysis.Usage.CacheReadTokens)
	}

	// The request must carry the tools, the cached system prompt and the
	// incident's topology neighbourhood.
	first := fake.bodies[0]
	toolDefs, _ := first["tools"].([]any)
	if len(toolDefs) != 7 {
		t.Errorf("sent %d tool definitions, want 7", len(toolDefs))
	}
	system, _ := first["system"].([]any)
	if len(system) != 1 {
		t.Fatalf("system = %v", first["system"])
	}
	block, _ := system[0].(map[string]any)
	if _, ok := block["cache_control"]; !ok {
		t.Error("the system prompt should be cached: it is identical on every investigation")
	}
	outputCfg, _ := first["output_config"].(map[string]any)
	if outputCfg["effort"] != "high" {
		t.Errorf("effort = %v, want high", outputCfg["effort"])
	}
	messages, _ := first["messages"].([]any)
	userMsg, _ := messages[0].(map[string]any)
	if !strings.Contains(fmt.Sprint(userMsg["content"]), "postgres-main") {
		t.Error("the user message should contain the dependency neighbourhood")
	}
}

// A root cause the catalog does not know must be bounced back for the model to
// correct, not accepted into the result.
func TestAgentRejectsUnknownRootCause(t *testing.T) {
	bad := map[string]any{
		"summary": "it was the ghost",
		"hypotheses": []map[string]any{{
			"title":              "Ghost service failed",
			"root_cause_service": "ghost-service",
			"confidence":         0.9,
			"rationale":          "a hunch",
			"evidence":           []map[string]any{{"kind": "reasoning", "summary": "vibes"}},
		}},
	}
	good := map[string]any{
		"summary": "postgres-main is saturated.",
		"hypotheses": []map[string]any{{
			"title":              "Connection pool exhaustion on postgres-main",
			"root_cause_service": "postgres-main",
			"confidence":         0.7,
			"rationale":          "corrected",
			"evidence":           []map[string]any{{"kind": "signal", "summary": "pool saturation at 98%"}},
		}},
	}
	fake := &fakeAPI{t: t, responses: []string{
		message("msg_1", "tool_use", toolUse("toolu_1", "report_findings", bad)),
		message("msg_2", "tool_use", toolUse("toolu_2", "report_findings", good)),
		message("msg_3", "end_turn", `{"type": "text", "text": "Corrected."}`),
	}}

	analysis, err := newClaude(t, fake).Analyze(context.Background(), newInput(t, "checkout"))
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if len(analysis.Hypotheses) != 1 || analysis.Hypotheses[0].RootCauseService != "postgres-main" {
		t.Fatalf("hypotheses = %+v, want only the corrected one", analysis.Hypotheses)
	}
	if analysis.Steps[0].Error == "" {
		t.Error("the rejected report should be recorded as a failed step")
	}

	// The error has to reach the model, otherwise it cannot correct itself.
	second := fake.bodies[1]
	if !strings.Contains(fmt.Sprint(second["messages"]), "not a catalog service id") {
		t.Error("the validation error should be fed back as a tool result")
	}
}

// Hitting the iteration cap must not throw the run away.
func TestAgentWithoutFindingsReturnsPartialResult(t *testing.T) {
	fake := &fakeAPI{t: t, responses: []string{
		message("msg_1", "end_turn", `{"type": "text", "text": "I need more data to be sure."}`),
	}}
	analysis, err := newClaude(t, fake).Analyze(context.Background(), newInput(t, "checkout"))
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if len(analysis.Hypotheses) != 0 {
		t.Errorf("hypotheses = %+v, want none", analysis.Hypotheses)
	}
	if !strings.Contains(analysis.Summary, "without structured findings") ||
		!strings.Contains(analysis.Summary, "I need more data") {
		t.Errorf("summary should explain the gap and carry the model's words: %q", analysis.Summary)
	}
}

func TestAgentSurfacesRefusal(t *testing.T) {
	fake := &fakeAPI{t: t, responses: []string{
		`{"id":"msg_1","type":"message","role":"assistant","model":"claude-opus-5-5",
		  "content":[],"stop_reason":"refusal",
		  "stop_details":{"type":"refusal","category":"cyber","explanation":"declined"},
		  "usage":{"input_tokens":10,"output_tokens":1}}`,
	}}
	_, err := newClaude(t, fake).Analyze(context.Background(), newInput(t, "checkout"))
	if err == nil || !strings.Contains(err.Error(), "declined") {
		t.Fatalf("err = %v, want a refusal error", err)
	}
}

func TestEffortIsValidated(t *testing.T) {
	if _, err := agent.New(agent.Config{Effort: "turbo"}); err == nil {
		t.Error("an unknown effort level should be rejected at construction")
	}
	c, err := agent.New(agent.Config{APIKey: "x"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if c.Name() != "claude:"+agent.DefaultModel {
		t.Errorf("Name() = %q", c.Name())
	}
}

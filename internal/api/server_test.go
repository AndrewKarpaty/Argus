package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AndrewKarpaty/Argus/internal/api"
	"github.com/AndrewKarpaty/Argus/internal/catalog"
	"github.com/AndrewKarpaty/Argus/internal/domain"
	"github.com/AndrewKarpaty/Argus/internal/investigator"
	"github.com/AndrewKarpaty/Argus/internal/store"
	"github.com/AndrewKarpaty/Argus/internal/telemetry"
	"github.com/AndrewKarpaty/Argus/internal/telemetry/demo"
)

type harness struct {
	handler http.Handler
	engine  *investigator.Engine
}

func newHarness(t *testing.T) harness {
	t.Helper()
	ctx := context.Background()

	prov, err := catalog.NewProvider(ctx, catalog.NewFile(filepath.Join("..", "..", "configs", "services.yaml")), nil)
	if err != nil {
		t.Fatalf("catalog: %v", err)
	}
	tel := demo.New("postgres-main")
	collector := &telemetry.Collector{Metrics: tel, Logs: tel, Traces: tel, Changes: tel}
	incidents := store.NewMemoryIncidents()
	runs := store.NewMemoryInvestigations()

	engine, err := investigator.NewEngine(investigator.Config{
		Reasoner:       investigator.NewHeuristic(),
		Catalog:        prov,
		Collector:      collector,
		Incidents:      incidents,
		Investigations: runs,
		Timeout:        30 * time.Second,
	})
	if err != nil {
		t.Fatalf("engine: %v", err)
	}
	srv, err := api.New(api.Deps{
		Catalog:        prov,
		Engine:         engine,
		Incidents:      incidents,
		Investigations: runs,
		Collector:      collector,
		Version:        "test",
	})
	if err != nil {
		t.Fatalf("api.New: %v", err)
	}
	return harness{handler: srv.Handler(), engine: engine}
}

func (h harness) do(t *testing.T, method, path, body string) (*http.Response, map[string]any) {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	h.handler.ServeHTTP(rec, req)

	res := rec.Result()
	var parsed map[string]any
	raw, _ := io.ReadAll(res.Body)
	if len(raw) > 0 && strings.HasPrefix(res.Header.Get("Content-Type"), "application/json") {
		if err := json.Unmarshal(raw, &parsed); err != nil {
			t.Fatalf("%s %s: response is not JSON: %v\n%s", method, path, err, raw)
		}
	}
	res.Body = io.NopCloser(bytes.NewReader(raw))
	return res, parsed
}

func TestIncidentToInvestigationFlow(t *testing.T) {
	h := newHarness(t)

	res, body := h.do(t, http.MethodPost, "/api/v1/incidents", `{
		"title": "Checkout and orders failing",
		"severity": "critical",
		"affected_services": ["checkout", "orders"],
		"investigate": true
	}`)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, body = %v", res.StatusCode, body)
	}
	if res.Header.Get("X-Request-Id") == "" {
		t.Error("X-Request-Id header missing")
	}
	incident, _ := body["incident"].(map[string]any)
	queued, ok := body["investigation"].(map[string]any)
	if !ok {
		t.Fatalf("no investigation started: %v", body)
	}
	incidentID, _ := incident["id"].(string)
	investigationID, _ := queued["id"].(string)
	if incidentID == "" || investigationID == "" {
		t.Fatalf("missing ids: %v", body)
	}
	if got := res.Header.Get("Location"); got != "/api/v1/investigations/"+investigationID {
		t.Errorf("Location = %q", got)
	}

	h.engine.Wait()

	res, body = h.do(t, http.MethodGet, "/api/v1/investigations/"+investigationID, "")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %v", res.StatusCode, body)
	}
	iv, _ := body["investigation"].(map[string]any)
	if iv["status"] != string(domain.InvestigationCompleted) {
		t.Fatalf("status = %v, error = %v", iv["status"], iv["error"])
	}
	hypotheses, _ := iv["hypotheses"].([]any)
	if len(hypotheses) == 0 {
		t.Fatal("no hypotheses")
	}
	top, _ := hypotheses[0].(map[string]any)
	if top["root_cause_service"] != "postgres-main" {
		t.Errorf("root cause = %v, want postgres-main", top["root_cause_service"])
	}

	res, body = h.do(t, http.MethodGet, "/api/v1/incidents/"+incidentID, "")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", res.StatusCode)
	}
	if runs, _ := body["investigations"].([]any); len(runs) != 1 {
		t.Errorf("expected the incident to list its one investigation, got %v", body["investigations"])
	}
}

func TestAlertWebhookStartsInvestigation(t *testing.T) {
	h := newHarness(t)
	res, body := h.do(t, http.MethodPost, "/api/v1/webhooks/alerts", `{
		"alert_name": "HighErrorRate",
		"service_id": "payments",
		"severity": "critical",
		"summary": "5xx ratio above 10% for 5 minutes"
	}`)
	if res.StatusCode != http.StatusAccepted {
		t.Fatalf("status = %d, body = %v", res.StatusCode, body)
	}
	if _, ok := body["investigation"].(map[string]any); !ok {
		t.Errorf("an alert should start an investigation by default: %v", body)
	}
	h.engine.Wait()

	res, body = h.do(t, http.MethodPost, "/api/v1/webhooks/alerts", `{"alert_name": "x"}`)
	if res.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("missing service_id: status = %d, body = %v", res.StatusCode, body)
	}
}

func TestSuspectsEndpoint(t *testing.T) {
	h := newHarness(t)

	res, body := h.do(t, http.MethodGet, "/api/v1/suspects?service=checkout&service=orders", "")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %v", res.StatusCode, body)
	}
	suspects, _ := body["suspects"].([]any)
	if len(suspects) == 0 {
		t.Fatal("no suspects returned")
	}
	top, _ := suspects[0].(map[string]any)
	if top["service_id"] != "postgres-main" {
		t.Errorf("top suspect = %v, want postgres-main", top["service_id"])
	}
	if cov, _ := top["coverage"].(float64); cov != 1 {
		t.Errorf("coverage = %v, want 1", top["coverage"])
	}

	res, _ = h.do(t, http.MethodGet, "/api/v1/suspects", "")
	if res.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("no service given: status = %d, want 422", res.StatusCode)
	}

	res, body = h.do(t, http.MethodGet, "/api/v1/suspects?service=ghost", "")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", res.StatusCode)
	}
	if unknown, _ := body["unknown_services"].([]any); len(unknown) != 1 {
		t.Errorf("an unknown service should be reported back, got %v", body["unknown_services"])
	}
}

// The console reads the leading hypothesis straight off the list, so the list
// has to carry the newest investigation per row.
func TestIncidentListCarriesLatestInvestigation(t *testing.T) {
	h := newHarness(t)

	res, _ := h.do(t, http.MethodPost, "/api/v1/incidents", `{
		"title": "Checkout failing",
		"affected_services": ["checkout", "orders"],
		"investigate": true
	}`)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d", res.StatusCode)
	}
	h.engine.Wait()

	res, body := h.do(t, http.MethodGet, "/api/v1/incidents", "")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", res.StatusCode)
	}
	rows, _ := body["incidents"].([]any)
	if len(rows) != 1 {
		t.Fatalf("expected one incident, got %d", len(rows))
	}
	row, _ := rows[0].(map[string]any)
	if row["title"] != "Checkout failing" {
		t.Errorf("the incident fields should be inlined on the row, got %v", row)
	}
	iv, ok := row["latest_investigation"].(map[string]any)
	if !ok {
		t.Fatalf("latest_investigation missing from the row: %v", row)
	}
	if iv["status"] != string(domain.InvestigationCompleted) {
		t.Errorf("investigation status = %v", iv["status"])
	}
	hypotheses, _ := iv["hypotheses"].([]any)
	if len(hypotheses) == 0 {
		t.Error("the attached investigation should carry its hypotheses")
	}
}

func TestGraphEndpoints(t *testing.T) {
	h := newHarness(t)

	res, body := h.do(t, http.MethodGet, "/readyz", "")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("readyz status = %d", res.StatusCode)
	}
	if body["engine"] != "heuristic" || body["status"] != "ready" {
		t.Errorf("unexpected readyz body: %v", body)
	}

	res, body = h.do(t, http.MethodGet, "/api/v1/services/postgres-main/blast-radius", "")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("blast-radius status = %d", res.StatusCode)
	}
	if n, _ := body["affected_count"].(float64); n < 4 {
		t.Errorf("affected_count = %v, want at least 4 for the shared database", body["affected_count"])
	}

	res, body = h.do(t, http.MethodGet, "/api/v1/services/checkout/dependencies?direction=dependencies&depth=1", "")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("dependencies status = %d", res.StatusCode)
	}
	if hops, _ := body["hops"].([]any); len(hops) != 5 {
		t.Errorf("checkout has 5 direct dependencies, got %d", len(hops))
	}

	res, _ = h.do(t, http.MethodGet, "/api/v1/services/checkout/dependencies?direction=sideways", "")
	if res.StatusCode != http.StatusBadRequest {
		t.Errorf("bad direction: status = %d, want 400", res.StatusCode)
	}

	res, _ = h.do(t, http.MethodGet, "/api/v1/graph?format=mermaid&service=checkout&radius=1&highlight=checkout", "")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("mermaid status = %d", res.StatusCode)
	}
	raw, _ := io.ReadAll(res.Body)
	if !strings.Contains(string(raw), "flowchart LR") {
		t.Errorf("not mermaid output: %s", raw)
	}

	res, _ = h.do(t, http.MethodGet, "/api/v1/services/nope", "")
	if res.StatusCode != http.StatusNotFound {
		t.Errorf("unknown service: status = %d, want 404", res.StatusCode)
	}
}

func TestValidationAndNotFound(t *testing.T) {
	h := newHarness(t)

	for _, tc := range []struct {
		name, body string
		want       int
	}{
		{"not json", `{nope}`, http.StatusBadRequest},
		{"unknown field", `{"title":"x","affected_services":["checkout"],"colour":"red"}`, http.StatusBadRequest},
		{"no title", `{"affected_services":["checkout"]}`, http.StatusUnprocessableEntity},
		{"no services", `{"title":"x","affected_services":[]}`, http.StatusUnprocessableEntity},
	} {
		res, body := h.do(t, http.MethodPost, "/api/v1/incidents", tc.body)
		if res.StatusCode != tc.want {
			t.Errorf("%s: status = %d, want %d (%v)", tc.name, res.StatusCode, tc.want, body)
		}
	}

	res, body := h.do(t, http.MethodGet, "/api/v1/investigations/inv_doesnotexist", "")
	if res.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", res.StatusCode)
	}
	if body["request_id"] == "" {
		t.Error("errors should carry the request id")
	}
}

// An incident about a service nobody declared must be accepted and flagged,
// not silently investigated as if the topology were known.
func TestUnknownServiceIsFlagged(t *testing.T) {
	h := newHarness(t)
	res, body := h.do(t, http.MethodPost, "/api/v1/incidents", `{
		"title": "Mystery service down",
		"affected_services": ["ghost-service"]
	}`)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, body = %v", res.StatusCode, body)
	}
	unknown, _ := body["unknown_services"].([]any)
	if len(unknown) != 1 || unknown[0] != "ghost-service" {
		t.Errorf("unknown_services = %v, want [ghost-service]", body["unknown_services"])
	}
}

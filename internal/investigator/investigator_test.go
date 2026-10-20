package investigator_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/AndrewKarpaty/Argus/internal/catalog"
	"github.com/AndrewKarpaty/Argus/internal/domain"
	"github.com/AndrewKarpaty/Argus/internal/investigator"
	"github.com/AndrewKarpaty/Argus/internal/store"
	"github.com/AndrewKarpaty/Argus/internal/telemetry"
	"github.com/AndrewKarpaty/Argus/internal/telemetry/demo"
)

type fixture struct {
	engine    *investigator.Engine
	incidents store.Incidents
	runs      store.Investigations
}

func newFixture(t *testing.T, faulty string) fixture {
	t.Helper()
	ctx := context.Background()

	prov, err := catalog.NewProvider(ctx, catalog.NewFile(filepath.Join("..", "..", "configs", "services.yaml")), nil)
	if err != nil {
		t.Fatalf("catalog: %v", err)
	}
	tel := demo.New(faulty)
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
		t.Fatalf("NewEngine: %v", err)
	}
	return fixture{engine: engine, incidents: incidents, runs: runs}
}

func seedIncident(t *testing.T, f fixture, affected ...string) domain.Incident {
	t.Helper()
	now := time.Now().UTC()
	inc := domain.Incident{
		ID:               domain.NewID("inc"),
		Title:            "Checkout and orders returning 5xx",
		Severity:         domain.SeverityCritical,
		AffectedServices: affected,
		StartedAt:        now.Add(-20 * time.Minute),
		DetectedAt:       now,
	}
	if err := inc.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if err := f.incidents.Create(context.Background(), inc); err != nil {
		t.Fatalf("Create: %v", err)
	}
	return inc
}

func TestHeuristicFindsSharedDatabase(t *testing.T) {
	f := newFixture(t, "postgres-main")
	inc := seedIncident(t, f, "checkout", "orders")

	iv, err := f.engine.Run(context.Background(), inc.ID)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if iv.Status != domain.InvestigationCompleted {
		t.Fatalf("status = %s, error = %q", iv.Status, iv.Error)
	}
	if iv.Engine != "heuristic" {
		t.Errorf("engine = %q", iv.Engine)
	}
	top, ok := iv.TopHypothesis()
	if !ok {
		t.Fatal("no hypotheses produced")
	}
	if top.RootCauseService != "postgres-main" {
		t.Errorf("top suspect = %s, want postgres-main (the one with a deploy and critical signals)", top.RootCauseService)
	}
	if top.Confidence <= 0.5 {
		t.Errorf("confidence = %v, want > 0.5 given a deploy plus critical telemetry", top.Confidence)
	}
	if len(top.PropagationPath) < 2 {
		t.Errorf("propagation path = %v, want a route from an affected service", top.PropagationPath)
	}
	if len(top.SuggestedActions) == 0 {
		t.Error("no suggested actions")
	}

	var kinds []domain.EvidenceKind
	for _, e := range top.Evidence {
		kinds = append(kinds, e.Kind)
	}
	if !hasKind(kinds, domain.EvidenceTopology) || !hasKind(kinds, domain.EvidenceChange) || !hasKind(kinds, domain.EvidenceSignal) {
		t.Errorf("evidence kinds = %v, want topology, change and signal", kinds)
	}
	if len(iv.Steps) < 3 {
		t.Errorf("recorded %d steps, want the full trail", len(iv.Steps))
	}
	if iv.Summary == "" || iv.FinishedAt == nil {
		t.Error("summary or finish time missing")
	}
}

// A confident answer must not survive the absence of evidence: with telemetry
// unavailable the same topology should yield a visibly lower confidence.
func TestConfidenceDropsWithoutTelemetry(t *testing.T) {
	withTel := newFixture(t, "postgres-main")
	incA := seedIncident(t, withTel, "checkout", "orders")
	ivA, err := withTel.engine.Run(context.Background(), incA.ID)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	topA, _ := ivA.TopHypothesis()

	ctx := context.Background()
	prov, err := catalog.NewProvider(ctx, catalog.NewFile(filepath.Join("..", "..", "configs", "services.yaml")), nil)
	if err != nil {
		t.Fatalf("catalog: %v", err)
	}
	incidents := store.NewMemoryIncidents()
	bare, err := investigator.NewEngine(investigator.Config{
		Reasoner:       investigator.NewHeuristic(),
		Catalog:        prov,
		Incidents:      incidents,
		Investigations: store.NewMemoryInvestigations(),
	})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	f := fixture{engine: bare, incidents: incidents}
	incB := seedIncident(t, f, "checkout", "orders")
	ivB, err := bare.Run(ctx, incB.ID)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	topB, ok := ivB.TopHypothesis()
	if !ok {
		t.Fatal("topology alone should still produce a hypothesis")
	}
	if topB.Confidence >= topA.Confidence {
		t.Errorf("confidence without telemetry (%v) should be below confidence with it (%v)", topB.Confidence, topA.Confidence)
	}
}

func TestUnknownServiceIsReportedNotGuessed(t *testing.T) {
	f := newFixture(t, "postgres-main")
	inc := seedIncident(t, f, "not-in-catalog")

	iv, err := f.engine.Run(context.Background(), inc.ID)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if iv.Status != domain.InvestigationCompleted {
		t.Fatalf("status = %s", iv.Status)
	}
	if len(iv.Hypotheses) != 0 {
		t.Errorf("expected no hypotheses for an unknown service, got %d", len(iv.Hypotheses))
	}
	if iv.Summary == "" {
		t.Error("expected the summary to explain that the service is not in the catalog")
	}
}

func TestStartRunsInBackground(t *testing.T) {
	f := newFixture(t, "redis-cache")
	inc := seedIncident(t, f, "auth", "recommendations")

	queued, err := f.engine.Start(context.Background(), inc.ID)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if queued.Status != domain.InvestigationQueued {
		t.Errorf("status = %s, want queued", queued.Status)
	}
	f.engine.Wait()

	done, err := f.runs.Get(context.Background(), queued.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if done.Status != domain.InvestigationCompleted {
		t.Fatalf("status = %s, error = %q", done.Status, done.Error)
	}
	top, ok := done.TopHypothesis()
	if !ok {
		t.Fatal("no hypotheses")
	}
	// auth depends on redis-cache hard, recommendations softly: the shared
	// cache is the only node that explains both.
	if top.RootCauseService != "redis-cache" {
		t.Errorf("top suspect = %s, want redis-cache", top.RootCauseService)
	}

	updated, err := f.incidents.Get(context.Background(), inc.ID)
	if err != nil {
		t.Fatalf("Get incident: %v", err)
	}
	if updated.Status != domain.IncidentInvestigating {
		t.Errorf("incident status = %s, want investigating", updated.Status)
	}
}

func hasKind(kinds []domain.EvidenceKind, want domain.EvidenceKind) bool {
	for _, k := range kinds {
		if k == want {
			return true
		}
	}
	return false
}

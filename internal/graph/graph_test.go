package graph_test

import (
	"strings"
	"testing"

	"github.com/AndrewKarpaty/Argus/internal/domain"
	"github.com/AndrewKarpaty/Argus/internal/graph"
)

// testGraph mirrors the sample catalog in configs/services.yaml: a checkout
// flow over payments/inventory with a shared Postgres underneath.
func testGraph(t *testing.T) *graph.Graph {
	t.Helper()
	services := []domain.Service{
		{ID: "checkout", Kind: domain.KindService, Tier: domain.TierCritical},
		{ID: "orders", Kind: domain.KindService, Tier: domain.TierCritical},
		{ID: "payments", Kind: domain.KindService, Tier: domain.TierCritical},
		{ID: "inventory", Kind: domain.KindService, Tier: domain.TierHigh},
		{ID: "recommendations", Kind: domain.KindService, Tier: domain.TierStandard},
		{ID: "postgres-main", Kind: domain.KindDatabase, Tier: domain.TierCritical},
		{ID: "redis", Kind: domain.KindCache, Tier: domain.TierStandard},
		{ID: "kafka", Kind: domain.KindQueue, Tier: domain.TierHigh},
	}
	deps := []domain.Dependency{
		{From: "checkout", To: "payments", Kind: domain.DepGRPC},
		{From: "checkout", To: "inventory", Kind: domain.DepGRPC},
		{From: "checkout", To: "recommendations", Kind: domain.DepHTTP, Criticality: domain.CriticalitySoft},
		{From: "orders", To: "payments", Kind: domain.DepGRPC},
		{From: "orders", To: "postgres-main", Kind: domain.DepSQL},
		{From: "payments", To: "postgres-main", Kind: domain.DepSQL},
		{From: "payments", To: "kafka", Kind: domain.DepQueue},
		{From: "inventory", To: "postgres-main", Kind: domain.DepSQL},
		{From: "inventory", To: "redis", Kind: domain.DepCache, Criticality: domain.CriticalitySoft},
		{From: "recommendations", To: "redis", Kind: domain.DepCache, Criticality: domain.CriticalitySoft},
	}
	g, err := graph.New(services, deps)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return g
}

func TestNewRejectsUnknownService(t *testing.T) {
	_, err := graph.New(
		[]domain.Service{{ID: "a"}},
		[]domain.Dependency{{From: "a", To: "ghost"}},
	)
	if err == nil {
		t.Fatal("expected an error for an edge pointing at an unknown service")
	}
}

func TestNewRejectsDuplicateAndSelfLoop(t *testing.T) {
	if _, err := graph.New([]domain.Service{{ID: "a"}, {ID: "a"}}, nil); err == nil {
		t.Error("expected an error for a duplicate service id")
	}
	if _, err := graph.New([]domain.Service{{ID: "a"}}, []domain.Dependency{{From: "a", To: "a"}}); err == nil {
		t.Error("expected an error for a self-loop")
	}
}

func TestTraverseDepthAndHardPath(t *testing.T) {
	g := testGraph(t)

	hops := g.Upstream("checkout", 0)
	got := map[string]graph.Hop{}
	for _, h := range hops {
		got[h.ServiceID] = h
	}

	for _, want := range []struct {
		id    string
		depth int
		hard  bool
	}{
		{"payments", 1, true},
		{"inventory", 1, true},
		{"recommendations", 1, false}, // soft edge from checkout
		{"postgres-main", 2, true},
		{"kafka", 2, true},
		{"redis", 2, false}, // only reachable over soft cache edges
	} {
		h, ok := got[want.id]
		if !ok {
			t.Errorf("%s: not reached from checkout", want.id)
			continue
		}
		if h.Depth != want.depth {
			t.Errorf("%s: depth = %d, want %d", want.id, h.Depth, want.depth)
		}
		if h.HardPath != want.hard {
			t.Errorf("%s: hardPath = %v, want %v", want.id, h.HardPath, want.hard)
		}
	}

	if shallow := g.Upstream("checkout", 1); len(shallow) != 3 {
		t.Errorf("depth-limited traversal returned %d hops, want 3", len(shallow))
	}
}

func TestBlastRadius(t *testing.T) {
	g := testGraph(t)
	radius := map[string]int{}
	for _, h := range g.BlastRadius("postgres-main") {
		radius[h.ServiceID] = h.Depth
	}
	for _, id := range []string{"orders", "payments", "inventory", "checkout"} {
		if _, ok := radius[id]; !ok {
			t.Errorf("%s missing from the blast radius of postgres-main", id)
		}
	}
	if radius["payments"] != 1 || radius["checkout"] != 2 {
		t.Errorf("unexpected depths: payments=%d checkout=%d", radius["payments"], radius["checkout"])
	}
	if len(g.BlastRadius("checkout")) != 0 {
		t.Error("checkout is an entry point, its blast radius should be empty")
	}
}

func TestPath(t *testing.T) {
	g := testGraph(t)
	p := g.Path("checkout", "postgres-main")
	if len(p) != 3 || p[0] != "checkout" || p[2] != "postgres-main" {
		t.Fatalf("Path(checkout, postgres-main) = %v, want a 3-node path between them", p)
	}
	if p := g.Path("postgres-main", "checkout"); p != nil {
		t.Errorf("dependency paths are directed, got %v", p)
	}
	if p := g.Path("checkout", "checkout"); len(p) != 1 {
		t.Errorf("Path to self = %v, want a single node", p)
	}
}

func TestSuspectsRanksSharedUpstream(t *testing.T) {
	g := testGraph(t)
	suspects := g.Suspects([]string{"checkout", "orders"}, graph.DefaultSuspectOptions())
	if len(suspects) == 0 {
		t.Fatal("no suspects returned")
	}

	byID := map[string]graph.Suspect{}
	for _, s := range suspects {
		byID[s.ServiceID] = s
	}
	for _, id := range []string{"payments", "postgres-main"} {
		s, ok := byID[id]
		if !ok {
			t.Fatalf("%s missing from suspects", id)
		}
		if s.Coverage != 1 {
			t.Errorf("%s: coverage = %v, want 1 (both affected services depend on it)", id, s.Coverage)
		}
	}

	// A shared datastore or a shared service must outrank a node that only
	// explains one of the two symptoms.
	top := suspects[0].ServiceID
	if top != "postgres-main" && top != "payments" {
		t.Errorf("top suspect = %s, want postgres-main or payments", top)
	}
	if byID["redis"].Coverage >= byID["payments"].Coverage {
		t.Error("redis explains fewer symptoms than payments but did not rank lower on coverage")
	}
	if byID["redis"].HardPath {
		t.Error("redis is only reachable over soft edges, hardPath should be false")
	}

	// Single-service incidents still produce its own upstream chain.
	single := g.Suspects([]string{"inventory"}, graph.DefaultSuspectOptions())
	if len(single) < 3 {
		t.Errorf("single-service incident returned %d suspects, want at least 3", len(single))
	}
}

func TestSuspectsIgnoresUnknownServices(t *testing.T) {
	g := testGraph(t)
	if s := g.Suspects([]string{"ghost"}, graph.DefaultSuspectOptions()); s != nil {
		t.Errorf("unknown service produced suspects: %v", s)
	}
}

func TestTopoOrderAndCycles(t *testing.T) {
	g := testGraph(t)
	order, err := g.TopoOrder()
	if err != nil {
		t.Fatalf("TopoOrder: %v", err)
	}
	pos := map[string]int{}
	for i, id := range order {
		pos[id] = i
	}
	if pos["checkout"] > pos["payments"] || pos["payments"] > pos["postgres-main"] {
		t.Errorf("callers must precede callees, got %v", order)
	}
	if c := g.Cycles(); len(c) != 0 {
		t.Errorf("acyclic catalog reported cycles: %v", c)
	}

	cyclic, err := graph.New(
		[]domain.Service{{ID: "a"}, {ID: "b"}, {ID: "c"}, {ID: "d"}},
		[]domain.Dependency{
			{From: "a", To: "b"}, {From: "b", To: "c"}, {From: "c", To: "a"},
			{From: "d", To: "a"},
		},
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	cycles := cyclic.Cycles()
	if len(cycles) != 1 || len(cycles[0]) != 3 {
		t.Fatalf("Cycles() = %v, want one component of 3 nodes", cycles)
	}
	if _, err := cyclic.TopoOrder(); err == nil {
		t.Error("expected TopoOrder to fail on a cyclic graph")
	}
}

func TestSubgraph(t *testing.T) {
	g := testGraph(t)
	sub := g.Subgraph([]string{"payments"}, 1)
	services, _ := sub.Size()
	if !sub.Has("payments") || !sub.Has("postgres-main") || !sub.Has("checkout") || !sub.Has("orders") || !sub.Has("kafka") {
		t.Errorf("subgraph is missing a direct neighbour of payments: %v", sub.Services())
	}
	if sub.Has("redis") {
		t.Error("redis is two hops from payments and should not be in a radius-1 subgraph")
	}
	if services != 5 {
		t.Errorf("subgraph has %d services, want 5", services)
	}
}

func TestMermaidAndDescribe(t *testing.T) {
	g := testGraph(t)
	m := g.Mermaid("checkout")
	if !strings.Contains(m, "flowchart LR") || !strings.Contains(m, "class checkout affected") || !strings.Contains(m, "-.->") {
		t.Errorf("unexpected mermaid output:\n%s", m)
	}
	d := g.Describe()
	if !strings.Contains(d, "postgres-main") || !strings.Contains(d, "depends on:") {
		t.Errorf("unexpected describe output:\n%s", d)
	}
	if d != g.Describe() {
		t.Error("Describe must be deterministic so the prompt prefix stays cacheable")
	}
}

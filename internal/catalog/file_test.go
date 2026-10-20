package catalog_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/AndrewKarpaty/Argus/internal/catalog"
)

func TestFileLoadsRepoCatalog(t *testing.T) {
	path := filepath.Join("..", "..", "configs", "services.yaml")
	src := catalog.NewFile(path)

	p, err := catalog.NewProvider(context.Background(), src, nil)
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	g := p.Graph()
	services, deps := g.Size()
	if services == 0 || deps == 0 {
		t.Fatalf("empty catalog: %d services, %d dependencies", services, deps)
	}
	if !g.Has("checkout") || !g.Has("postgres-main") {
		t.Error("the sample catalog should define checkout and postgres-main")
	}
	if cycles := g.Cycles(); len(cycles) != 0 {
		t.Errorf("the sample catalog should be acyclic, got %v", cycles)
	}
	if snap := p.Snapshot(); snap.Source == "" || snap.LoadedAt.IsZero() {
		t.Errorf("snapshot metadata not populated: %+v", snap)
	}
}

func TestFileRejectsUnknownFieldsAndBadTopology(t *testing.T) {
	dir := t.TempDir()

	typo := filepath.Join(dir, "typo.yaml")
	mustWrite(t, typo, "services:\n  - id: a\n    teem: platform\n")
	if _, err := catalog.NewFile(typo).Load(context.Background()); err == nil {
		t.Error("a misspelled catalog key should be rejected")
	}

	dangling := filepath.Join(dir, "dangling.yaml")
	mustWrite(t, dangling, "services:\n  - id: a\ndependencies:\n  - from: a\n    to: ghost\n")
	if _, err := catalog.NewProvider(context.Background(), catalog.NewFile(dangling), nil); err == nil {
		t.Error("a dependency on an undeclared service should be rejected")
	}

	empty := filepath.Join(dir, "empty.yaml")
	mustWrite(t, empty, "services: []\n")
	if _, err := catalog.NewFile(empty).Load(context.Background()); err == nil {
		t.Error("an empty catalog should be rejected")
	}

	if _, err := catalog.NewFile(filepath.Join(dir, "missing.yaml")).Load(context.Background()); err == nil {
		t.Error("a missing catalog file should be reported")
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

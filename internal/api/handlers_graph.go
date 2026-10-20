package api

import (
	"net/http"
	"strconv"

	"github.com/AndrewKarpaty/Argus/internal/graph"
)

func (s *Server) handleListServices(w http.ResponseWriter, r *http.Request) {
	g := s.deps.Catalog.Graph()
	services := g.Services()
	snap := s.deps.Catalog.Snapshot()
	writeJSON(w, r, http.StatusOK, map[string]any{
		"source":    snap.Source,
		"loaded_at": snap.LoadedAt,
		"count":     len(services),
		"services":  services,
	})
}

func (s *Server) handleGetService(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	g := s.deps.Catalog.Graph()
	svc, ok := g.Service(id)
	if !ok {
		writeError(w, r, http.StatusNotFound, "unknown service", nil)
		return
	}
	writeJSON(w, r, http.StatusOK, map[string]any{
		"service":      svc,
		"dependencies": g.Neighbours(id, graph.Dependencies),
		"dependents":   g.Neighbours(id, graph.Dependents),
	})
}

// handleServiceDependencies walks the graph from one service.
//
//	GET /api/v1/services/{id}/dependencies?direction=dependents&depth=2
func (s *Server) handleServiceDependencies(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	g := s.deps.Catalog.Graph()
	if !g.Has(id) {
		writeError(w, r, http.StatusNotFound, "unknown service", nil)
		return
	}

	dir := graph.Dependencies
	if raw := r.URL.Query().Get("direction"); raw != "" {
		dir = graph.Direction(raw)
		if !dir.Valid() {
			writeError(w, r, http.StatusBadRequest,
				"direction must be 'dependencies' or 'dependents'", nil)
			return
		}
	}
	depth, err := intQuery(r, "depth", 0)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "depth must be a non-negative integer", err)
		return
	}

	hops := g.Traverse(id, dir, depth)
	writeJSON(w, r, http.StatusOK, map[string]any{
		"service":   id,
		"direction": dir,
		"depth":     depth,
		"count":     len(hops),
		"hops":      hops,
	})
}

// handleBlastRadius answers "what is at risk while this service is unhealthy".
func (s *Server) handleBlastRadius(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	g := s.deps.Catalog.Graph()
	if !g.Has(id) {
		writeError(w, r, http.StatusNotFound, "unknown service", nil)
		return
	}
	hops := g.BlastRadius(id)
	critical := 0
	for _, h := range hops {
		if svc, ok := g.Service(h.ServiceID); ok && svc.Tier == "critical" {
			critical++
		}
	}
	writeJSON(w, r, http.StatusOK, map[string]any{
		"service":           id,
		"affected_count":    len(hops),
		"critical_affected": critical,
		"affected":          hops,
	})
}

// handleGraph returns the whole graph, or a neighbourhood of it.
//
//	GET /api/v1/graph?format=mermaid&service=checkout&radius=2&highlight=checkout
func (s *Server) handleGraph(w http.ResponseWriter, r *http.Request) {
	g := s.deps.Catalog.Graph()
	q := r.URL.Query()

	if seeds := q["service"]; len(seeds) > 0 {
		radius, err := intQuery(r, "radius", 2)
		if err != nil {
			writeError(w, r, http.StatusBadRequest, "radius must be a non-negative integer", err)
			return
		}
		for _, id := range seeds {
			if !g.Has(id) {
				writeError(w, r, http.StatusNotFound, "unknown service: "+id, nil)
				return
			}
		}
		g = g.Subgraph(seeds, radius)
	}

	switch q.Get("format") {
	case "mermaid":
		writeText(w, http.StatusOK, g.Mermaid(q["highlight"]...))
	case "text":
		writeText(w, http.StatusOK, g.Describe())
	case "", "json":
		services, deps := g.Size()
		writeJSON(w, r, http.StatusOK, map[string]any{
			"services":     g.Services(),
			"dependencies": g.Edges(),
			"cycles":       g.Cycles(),
			"counts":       map[string]int{"services": services, "dependencies": deps},
		})
	default:
		writeError(w, r, http.StatusBadRequest, "format must be json, mermaid or text", nil)
	}
}

// handleCatalogRefresh reloads the catalog from its source.
func (s *Server) handleCatalogRefresh(w http.ResponseWriter, r *http.Request) {
	if err := s.deps.Catalog.Refresh(r.Context()); err != nil {
		// The previous good graph is still being served, so this is a 422
		// rather than a 500: the request was fine, the catalog is not.
		writeError(w, r, http.StatusUnprocessableEntity, "catalog reload failed, keeping the previous snapshot", err)
		return
	}
	snap := s.deps.Catalog.Snapshot()
	services, deps := s.deps.Catalog.Graph().Size()
	writeJSON(w, r, http.StatusOK, map[string]any{
		"source":       snap.Source,
		"loaded_at":    snap.LoadedAt,
		"services":     services,
		"dependencies": deps,
	})
}

func intQuery(r *http.Request, key string, fallback int) (int, error) {
	raw := r.URL.Query().Get(key)
	if raw == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return fallback, err
	}
	if n < 0 {
		return fallback, strconv.ErrRange
	}
	return n, nil
}

// Package api exposes the platform over HTTP. The router is the standard
// library's: Go's ServeMux handles method-and-pattern routing, so a third
// party router would earn nothing here.
package api

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/AndrewKarpaty/Argus/internal/catalog"
	"github.com/AndrewKarpaty/Argus/internal/investigator"
	"github.com/AndrewKarpaty/Argus/internal/store"
	"github.com/AndrewKarpaty/Argus/internal/telemetry"
)

// Deps are the collaborators the HTTP layer needs.
type Deps struct {
	Catalog        *catalog.Provider
	Engine         *investigator.Engine
	Incidents      store.Incidents
	Investigations store.Investigations
	Collector      *telemetry.Collector
	Log            *slog.Logger
	// RequestTimeout bounds a single HTTP handler. Defaults to 30s.
	RequestTimeout time.Duration
	// Version is reported by the health endpoints.
	Version string
	// UI serves the web console at the paths the API does not claim. Nil
	// leaves the server API-only.
	UI http.Handler
}

// Server holds the HTTP handlers.
type Server struct {
	deps Deps
	log  *slog.Logger
}

// New validates the dependencies and returns a Server.
func New(deps Deps) (*Server, error) {
	switch {
	case deps.Catalog == nil:
		return nil, errors.New("api: catalog provider is required")
	case deps.Engine == nil:
		return nil, errors.New("api: investigation engine is required")
	case deps.Incidents == nil || deps.Investigations == nil:
		return nil, errors.New("api: both stores are required")
	}
	if deps.Log == nil {
		deps.Log = slog.Default()
	}
	if deps.RequestTimeout <= 0 {
		deps.RequestTimeout = 30 * time.Second
	}
	if deps.Version == "" {
		deps.Version = "dev"
	}
	return &Server{deps: deps, log: deps.Log}, nil
}

// Handler returns the fully wired HTTP handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// Health. Deliberately outside the timeout and access-log noise budget:
	// they are cheap and are hit constantly by orchestrators.
	mux.HandleFunc("GET /healthz", s.handleHealthz)
	mux.HandleFunc("GET /readyz", s.handleReadyz)

	// Catalog and topology.
	mux.HandleFunc("GET /api/v1/services", s.handleListServices)
	mux.HandleFunc("GET /api/v1/services/{id}", s.handleGetService)
	mux.HandleFunc("GET /api/v1/services/{id}/dependencies", s.handleServiceDependencies)
	mux.HandleFunc("GET /api/v1/services/{id}/blast-radius", s.handleBlastRadius)
	mux.HandleFunc("GET /api/v1/graph", s.handleGraph)
	mux.HandleFunc("GET /api/v1/suspects", s.handleSuspects)
	mux.HandleFunc("POST /api/v1/catalog/refresh", s.handleCatalogRefresh)

	// Incidents and investigations.
	mux.HandleFunc("POST /api/v1/incidents", s.handleCreateIncident)
	mux.HandleFunc("GET /api/v1/incidents", s.handleListIncidents)
	mux.HandleFunc("GET /api/v1/incidents/{id}", s.handleGetIncident)
	mux.HandleFunc("POST /api/v1/incidents/{id}/investigate", s.handleInvestigate)
	mux.HandleFunc("GET /api/v1/incidents/{id}/investigations", s.handleListInvestigations)
	mux.HandleFunc("GET /api/v1/investigations/{id}", s.handleGetInvestigation)

	// Ingestion. One webhook shape per alerting system; this is the generic one.
	mux.HandleFunc("POST /api/v1/webhooks/alerts", s.handleAlertWebhook)

	// The console takes everything left over, so its client-side routes
	// resolve on a hard refresh. API patterns above are more specific and so
	// keep winning.
	if s.deps.UI != nil {
		mux.Handle("/", s.deps.UI)
	}

	var h http.Handler = mux
	h = withTimeout(s.deps.RequestTimeout, h)
	h = withObservability(s.log, h)
	return h
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, r, http.StatusOK, map[string]string{
		"status":  "ok",
		"version": s.deps.Version,
	})
}

// handleReadyz reports whether the platform can actually investigate: a graph
// is loaded and a reasoner is wired.
func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
	g := s.deps.Catalog.Graph()
	if g == nil {
		writeError(w, r, http.StatusServiceUnavailable, "catalog not loaded", nil)
		return
	}
	services, deps := g.Size()
	snap := s.deps.Catalog.Snapshot()

	var providers []string
	if s.deps.Collector != nil {
		providers = s.deps.Collector.Available()
	}
	writeJSON(w, r, http.StatusOK, map[string]any{
		"status":              "ready",
		"version":             s.deps.Version,
		"engine":              s.deps.Engine.EngineName(),
		"catalog_source":      snap.Source,
		"catalog_loaded_at":   snap.LoadedAt,
		"services":            services,
		"dependencies":        deps,
		"telemetry_providers": providers,
	})
}

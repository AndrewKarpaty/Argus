package api

import (
	"net/http"
	"time"

	"github.com/AndrewKarpaty/Argus/internal/domain"
	"github.com/AndrewKarpaty/Argus/internal/store"
)

// createIncidentRequest is the public shape for opening an incident. It is
// kept separate from domain.Incident so that server-owned fields (id,
// timestamps, status) cannot be set by a client.
type createIncidentRequest struct {
	Title            string          `json:"title"`
	Description      string          `json:"description,omitempty"`
	Severity         domain.Severity `json:"severity,omitempty"`
	AffectedServices []string        `json:"affected_services"`
	Signals          []domain.Signal `json:"signals,omitempty"`
	StartedAt        *time.Time      `json:"started_at,omitempty"`
	DetectedAt       *time.Time      `json:"detected_at,omitempty"`
	// Investigate starts an investigation immediately after creation, which
	// is what an alerting integration usually wants.
	Investigate bool `json:"investigate,omitempty"`
}

func (s *Server) handleCreateIncident(w http.ResponseWriter, r *http.Request) {
	var req createIncidentRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid request body", err)
		return
	}

	now := time.Now().UTC()
	inc := domain.Incident{
		ID:               domain.NewID("inc"),
		Title:            req.Title,
		Description:      req.Description,
		Severity:         req.Severity,
		Status:           domain.IncidentOpen,
		AffectedServices: req.AffectedServices,
		Signals:          req.Signals,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if req.StartedAt != nil {
		inc.StartedAt = req.StartedAt.UTC()
	}
	if req.DetectedAt != nil {
		inc.DetectedAt = req.DetectedAt.UTC()
	}
	if err := inc.Validate(); err != nil {
		writeError(w, r, http.StatusUnprocessableEntity, "invalid incident", err)
		return
	}

	// Services that are not in the catalog are accepted but called out: the
	// investigation cannot reason about topology for them.
	unknown := s.unknownServices(inc.AffectedServices)

	if err := s.deps.Incidents.Create(r.Context(), inc); err != nil {
		writeStoreError(w, r, err)
		return
	}
	loggerFrom(r).Info("incident created",
		"incident", inc.ID, "severity", inc.Severity,
		"affected", inc.AffectedServices, "unknown_services", unknown)

	body := map[string]any{"incident": inc}
	if len(unknown) > 0 {
		body["warning"] = "these affected services are not in the catalog, so no topology reasoning is possible for them"
		body["unknown_services"] = unknown
	}

	if req.Investigate {
		iv, err := s.deps.Engine.Start(r.Context(), inc.ID)
		if err != nil {
			// The incident exists; report it and let the client retry the
			// investigation separately.
			body["investigation_error"] = err.Error()
			writeJSON(w, r, http.StatusCreated, body)
			return
		}
		body["investigation"] = iv
		w.Header().Set("Location", "/api/v1/investigations/"+iv.ID)
	}
	writeJSON(w, r, http.StatusCreated, body)
}

func (s *Server) handleListIncidents(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, err := intQuery(r, "limit", 50)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "limit must be a non-negative integer", err)
		return
	}
	filter := store.IncidentFilter{
		Status:  domain.IncidentStatus(q.Get("status")),
		Service: q.Get("service"),
		Limit:   limit,
	}
	incidents, err := s.deps.Incidents.List(r.Context(), filter)
	if err != nil {
		writeStoreError(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, map[string]any{
		"count":     len(incidents),
		"incidents": incidents,
	})
}

func (s *Server) handleGetIncident(w http.ResponseWriter, r *http.Request) {
	inc, err := s.deps.Incidents.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeStoreError(w, r, err)
		return
	}
	runs, err := s.deps.Investigations.ListByIncident(r.Context(), inc.ID)
	if err != nil {
		writeStoreError(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, map[string]any{
		"incident":       inc,
		"window":         inc.Window(),
		"investigations": runs,
	})
}

// handleInvestigate starts an investigation and returns 202 with the queued
// record: the run outlives the request, so poll the investigation by id.
func (s *Server) handleInvestigate(w http.ResponseWriter, r *http.Request) {
	iv, err := s.deps.Engine.Start(r.Context(), r.PathValue("id"))
	if err != nil {
		writeStoreError(w, r, err)
		return
	}
	w.Header().Set("Location", "/api/v1/investigations/"+iv.ID)
	writeJSON(w, r, http.StatusAccepted, map[string]any{"investigation": iv})
}

func (s *Server) handleListInvestigations(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := s.deps.Incidents.Get(r.Context(), id); err != nil {
		writeStoreError(w, r, err)
		return
	}
	runs, err := s.deps.Investigations.ListByIncident(r.Context(), id)
	if err != nil {
		writeStoreError(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, map[string]any{
		"count":          len(runs),
		"investigations": runs,
	})
}

func (s *Server) handleGetInvestigation(w http.ResponseWriter, r *http.Request) {
	iv, err := s.deps.Investigations.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeStoreError(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, map[string]any{"investigation": iv})
}

// alertWebhookRequest is a deliberately generic alert payload: one alert, the
// service it fired for, and whatever labels the sender has. Mapping
// Alertmanager, Datadog or PagerDuty onto it belongs in a small adapter per
// system rather than in one sprawling handler.
type alertWebhookRequest struct {
	AlertName string            `json:"alert_name"`
	ServiceID string            `json:"service_id"`
	Severity  domain.Severity   `json:"severity,omitempty"`
	Summary   string            `json:"summary,omitempty"`
	StartsAt  *time.Time        `json:"starts_at,omitempty"`
	Labels    map[string]string `json:"labels,omitempty"`
	// Investigate defaults to true: an alert that nobody looks at is the
	// reason this platform exists.
	Investigate *bool `json:"investigate,omitempty"`
}

func (s *Server) handleAlertWebhook(w http.ResponseWriter, r *http.Request) {
	var req alertWebhookRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid request body", err)
		return
	}
	if req.AlertName == "" || req.ServiceID == "" {
		writeError(w, r, http.StatusUnprocessableEntity, "alert_name and service_id are required", nil)
		return
	}

	now := time.Now().UTC()
	startsAt := now
	if req.StartsAt != nil {
		startsAt = req.StartsAt.UTC()
	}
	severity := req.Severity
	if severity == "" {
		severity = domain.SeverityHigh
	}

	inc := domain.Incident{
		ID:               domain.NewID("inc"),
		Title:            req.AlertName + " on " + req.ServiceID,
		Description:      req.Summary,
		Severity:         severity,
		Status:           domain.IncidentOpen,
		AffectedServices: []string{req.ServiceID},
		StartedAt:        startsAt,
		DetectedAt:       now,
		CreatedAt:        now,
		UpdatedAt:        now,
		Signals: []domain.Signal{{
			ID:          domain.NewID("sig"),
			Source:      domain.SourceAlert,
			ServiceID:   req.ServiceID,
			Title:       req.AlertName,
			Description: req.Summary,
			Severity:    severity,
			ObservedAt:  startsAt,
			Labels:      req.Labels,
		}},
	}
	if err := inc.Validate(); err != nil {
		writeError(w, r, http.StatusUnprocessableEntity, "invalid alert", err)
		return
	}
	if err := s.deps.Incidents.Create(r.Context(), inc); err != nil {
		writeStoreError(w, r, err)
		return
	}
	loggerFrom(r).Info("alert ingested",
		"incident", inc.ID, "alert", req.AlertName, "service", req.ServiceID)

	body := map[string]any{"incident": inc}
	if unknown := s.unknownServices(inc.AffectedServices); len(unknown) > 0 {
		body["unknown_services"] = unknown
	}
	if req.Investigate == nil || *req.Investigate {
		iv, err := s.deps.Engine.Start(r.Context(), inc.ID)
		if err != nil {
			body["investigation_error"] = err.Error()
		} else {
			body["investigation"] = iv
			w.Header().Set("Location", "/api/v1/investigations/"+iv.ID)
		}
	}
	writeJSON(w, r, http.StatusAccepted, body)
}

func (s *Server) unknownServices(ids []string) []string {
	g := s.deps.Catalog.Graph()
	var unknown []string
	for _, id := range ids {
		if !g.Has(id) {
			unknown = append(unknown, id)
		}
	}
	return unknown
}

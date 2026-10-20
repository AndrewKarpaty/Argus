package domain

import (
	"fmt"
	"strings"
	"time"
)

// IncidentStatus is the lifecycle state of an incident.
type IncidentStatus string

const (
	IncidentOpen          IncidentStatus = "open"
	IncidentInvestigating IncidentStatus = "investigating"
	IncidentMitigated     IncidentStatus = "mitigated"
	IncidentResolved      IncidentStatus = "resolved"
)

// DefaultLookback is how far back telemetry is pulled when an incident does
// not say when it started.
const DefaultLookback = 30 * time.Minute

// Incident is the unit of work of the platform: a set of symptoms observed on
// one or more services at a point in time.
type Incident struct {
	ID               string         `json:"id"`
	Title            string         `json:"title"`
	Description      string         `json:"description,omitempty"`
	Severity         Severity       `json:"severity"`
	Status           IncidentStatus `json:"status"`
	AffectedServices []string       `json:"affected_services"`
	Signals          []Signal       `json:"signals,omitempty"`
	StartedAt        time.Time      `json:"started_at"`
	DetectedAt       time.Time      `json:"detected_at"`
	ResolvedAt       *time.Time     `json:"resolved_at,omitempty"`
	CreatedAt        time.Time      `json:"created_at"`
	UpdatedAt        time.Time      `json:"updated_at"`
}

// Validate fills in defaults and reports structural problems.
func (i *Incident) Validate() error {
	i.Title = strings.TrimSpace(i.Title)
	if i.Title == "" {
		return fmt.Errorf("incident: title is required")
	}
	if len(i.AffectedServices) == 0 {
		return fmt.Errorf("incident: at least one affected service is required")
	}
	for _, id := range i.AffectedServices {
		if strings.TrimSpace(id) == "" {
			return fmt.Errorf("incident: affected service id must not be empty")
		}
	}
	if i.Severity == "" {
		i.Severity = SeverityHigh
	}
	if i.Status == "" {
		i.Status = IncidentOpen
	}
	if i.DetectedAt.IsZero() {
		i.DetectedAt = time.Now().UTC()
	}
	if i.StartedAt.IsZero() {
		i.StartedAt = i.DetectedAt
	}
	if i.StartedAt.After(i.DetectedAt) {
		return fmt.Errorf("incident: started_at must not be after detected_at")
	}
	return nil
}

// Window is the interval an investigation should look at. It starts shortly
// before the incident began, because the cause precedes the symptom.
func (i Incident) Window() TimeWindow {
	to := i.DetectedAt
	if to.IsZero() {
		to = time.Now().UTC()
	}
	from := i.StartedAt
	if from.IsZero() || !from.Before(to) {
		from = to.Add(-DefaultLookback)
	}
	// Always give the window enough room to contain the triggering change.
	if to.Sub(from) < DefaultLookback {
		from = to.Add(-DefaultLookback)
	}
	return TimeWindow{From: from, To: to}
}

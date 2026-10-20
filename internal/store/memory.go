package store

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"sync"

	"github.com/AndrewKarpaty/Argus/internal/domain"
)

// MemoryIncidents is a goroutine-safe in-memory Incidents store. It is the
// default for local development; its contents are lost on restart.
type MemoryIncidents struct {
	mu    sync.RWMutex
	items map[string]domain.Incident
}

var _ Incidents = (*MemoryIncidents)(nil)

// NewMemoryIncidents returns an empty incident store.
func NewMemoryIncidents() *MemoryIncidents {
	return &MemoryIncidents{items: make(map[string]domain.Incident)}
}

// Create stores a new incident.
func (m *MemoryIncidents) Create(ctx context.Context, inc domain.Incident) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if inc.ID == "" {
		return fmt.Errorf("store: incident id is required")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.items[inc.ID]; exists {
		return fmt.Errorf("store: incident %s: %w", inc.ID, ErrConflict)
	}
	m.items[inc.ID] = inc
	return nil
}

// Get returns one incident.
func (m *MemoryIncidents) Get(ctx context.Context, id string) (domain.Incident, error) {
	if err := ctx.Err(); err != nil {
		return domain.Incident{}, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	inc, ok := m.items[id]
	if !ok {
		return domain.Incident{}, fmt.Errorf("store: incident %s: %w", id, ErrNotFound)
	}
	return inc, nil
}

// List returns matching incidents, newest first.
func (m *MemoryIncidents) List(ctx context.Context, f IncidentFilter) ([]domain.Incident, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	out := make([]domain.Incident, 0, len(m.items))
	for _, inc := range m.items {
		if f.Status != "" && inc.Status != f.Status {
			continue
		}
		if f.Service != "" && !slices.Contains(inc.AffectedServices, f.Service) {
			continue
		}
		out = append(out, inc)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].DetectedAt.Equal(out[j].DetectedAt) {
			return out[i].DetectedAt.After(out[j].DetectedAt)
		}
		return out[i].ID < out[j].ID
	})
	if f.Limit > 0 && len(out) > f.Limit {
		out = out[:f.Limit]
	}
	return out, nil
}

// Update replaces an existing incident.
func (m *MemoryIncidents) Update(ctx context.Context, inc domain.Incident) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.items[inc.ID]; !ok {
		return fmt.Errorf("store: incident %s: %w", inc.ID, ErrNotFound)
	}
	m.items[inc.ID] = inc
	return nil
}

// MemoryInvestigations is a goroutine-safe in-memory Investigations store.
type MemoryInvestigations struct {
	mu    sync.RWMutex
	items map[string]domain.Investigation
}

var _ Investigations = (*MemoryInvestigations)(nil)

// NewMemoryInvestigations returns an empty investigation store.
func NewMemoryInvestigations() *MemoryInvestigations {
	return &MemoryInvestigations{items: make(map[string]domain.Investigation)}
}

// Create stores a new investigation.
func (m *MemoryInvestigations) Create(ctx context.Context, iv domain.Investigation) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if iv.ID == "" {
		return fmt.Errorf("store: investigation id is required")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.items[iv.ID]; exists {
		return fmt.Errorf("store: investigation %s: %w", iv.ID, ErrConflict)
	}
	m.items[iv.ID] = iv
	return nil
}

// Get returns one investigation.
func (m *MemoryInvestigations) Get(ctx context.Context, id string) (domain.Investigation, error) {
	if err := ctx.Err(); err != nil {
		return domain.Investigation{}, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	iv, ok := m.items[id]
	if !ok {
		return domain.Investigation{}, fmt.Errorf("store: investigation %s: %w", id, ErrNotFound)
	}
	return iv, nil
}

// ListByIncident returns the runs for one incident, newest first.
func (m *MemoryInvestigations) ListByIncident(ctx context.Context, incidentID string) ([]domain.Investigation, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []domain.Investigation
	for _, iv := range m.items {
		if iv.IncidentID == incidentID {
			out = append(out, iv)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].StartedAt.Equal(out[j].StartedAt) {
			return out[i].StartedAt.After(out[j].StartedAt)
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

// Update replaces an existing investigation.
func (m *MemoryInvestigations) Update(ctx context.Context, iv domain.Investigation) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.items[iv.ID]; !ok {
		return fmt.Errorf("store: investigation %s: %w", iv.ID, ErrNotFound)
	}
	m.items[iv.ID] = iv
	return nil
}

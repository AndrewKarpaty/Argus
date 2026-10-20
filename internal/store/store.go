// Package store defines persistence for incidents and investigations, plus an
// in-memory implementation. The interfaces are what the rest of the platform
// depends on, so swapping in Postgres is a new file here and a wiring change
// in cmd/argusd - nothing else.
package store

import (
	"context"
	"errors"

	"github.com/AndrewKarpaty/Argus/internal/domain"
)

// ErrNotFound is returned when a record does not exist.
var ErrNotFound = errors.New("not found")

// ErrConflict is returned when a record already exists.
var ErrConflict = errors.New("already exists")

// IncidentFilter narrows a List call.
type IncidentFilter struct {
	Status  domain.IncidentStatus
	Service string
	Limit   int
}

// Incidents persists incidents.
type Incidents interface {
	Create(ctx context.Context, inc domain.Incident) error
	Get(ctx context.Context, id string) (domain.Incident, error)
	List(ctx context.Context, f IncidentFilter) ([]domain.Incident, error)
	Update(ctx context.Context, inc domain.Incident) error
}

// Investigations persists investigation runs.
type Investigations interface {
	Create(ctx context.Context, iv domain.Investigation) error
	Get(ctx context.Context, id string) (domain.Investigation, error)
	ListByIncident(ctx context.Context, incidentID string) ([]domain.Investigation, error)
	Update(ctx context.Context, iv domain.Investigation) error
}

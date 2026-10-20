// Package domain holds the core types of the Argus platform: the service
// catalog, the dependency graph edges, incidents, signals and the artefacts
// an investigation produces. It has no dependencies on transport, storage or
// LLM packages so that every other layer can import it freely.
package domain

import (
	"fmt"
	"strings"
)

// ServiceKind describes what sort of node a catalog entry is. Datastores and
// queues participate in the dependency graph exactly like services do: an
// overloaded Postgres is a perfectly ordinary root cause.
type ServiceKind string

const (
	KindService  ServiceKind = "service"
	KindDatabase ServiceKind = "database"
	KindQueue    ServiceKind = "queue"
	KindCache    ServiceKind = "cache"
	KindExternal ServiceKind = "external"
)

// Tier encodes how much user-visible damage an outage of this node causes.
// It is used to rank suspects when several candidates explain a symptom
// equally well.
type Tier string

const (
	TierCritical Tier = "critical"
	TierHigh     Tier = "high"
	TierStandard Tier = "standard"
)

// Weight returns a ranking multiplier for the tier.
func (t Tier) Weight() float64 {
	switch t {
	case TierCritical:
		return 1.3
	case TierHigh:
		return 1.15
	default:
		return 1.0
	}
}

// Service is one node of the dependency graph.
type Service struct {
	ID          string            `json:"id" yaml:"id"`
	Name        string            `json:"name" yaml:"name"`
	Kind        ServiceKind       `json:"kind" yaml:"kind"`
	Tier        Tier              `json:"tier" yaml:"tier"`
	Team        string            `json:"team,omitempty" yaml:"team,omitempty"`
	Repo        string            `json:"repo,omitempty" yaml:"repo,omitempty"`
	Runbook     string            `json:"runbook,omitempty" yaml:"runbook,omitempty"`
	Description string            `json:"description,omitempty" yaml:"description,omitempty"`
	Labels      map[string]string `json:"labels,omitempty" yaml:"labels,omitempty"`
}

// Validate fills in defaults and reports structural problems.
func (s *Service) Validate() error {
	s.ID = strings.TrimSpace(s.ID)
	if s.ID == "" {
		return fmt.Errorf("service: id is required")
	}
	if s.Name == "" {
		s.Name = s.ID
	}
	if s.Kind == "" {
		s.Kind = KindService
	}
	if s.Tier == "" {
		s.Tier = TierStandard
	}
	return nil
}

// DependencyKind is the transport over which one node reaches another.
type DependencyKind string

const (
	DepHTTP     DependencyKind = "http"
	DepGRPC     DependencyKind = "grpc"
	DepSQL      DependencyKind = "sql"
	DepQueue    DependencyKind = "queue"
	DepCache    DependencyKind = "cache"
	DepExternal DependencyKind = "external"
)

// Criticality says what happens to the caller when the callee is down. A hard
// dependency propagates the outage; a soft one only degrades the caller. This
// is the single most useful piece of catalog metadata when deciding whether a
// candidate upstream can explain the symptom at all.
type Criticality string

const (
	// CriticalityHard means the caller fails when the callee fails.
	CriticalityHard Criticality = "hard"
	// CriticalitySoft means the caller degrades but keeps serving.
	CriticalitySoft Criticality = "soft"
)

// Weight returns a ranking multiplier for the criticality.
func (c Criticality) Weight() float64 {
	if c == CriticalitySoft {
		return 0.6
	}
	return 1.0
}

// Dependency is a directed edge: From calls To.
type Dependency struct {
	From        string         `json:"from" yaml:"from"`
	To          string         `json:"to" yaml:"to"`
	Kind        DependencyKind `json:"kind" yaml:"kind"`
	Criticality Criticality    `json:"criticality" yaml:"criticality"`
	Description string         `json:"description,omitempty" yaml:"description,omitempty"`
}

// Validate fills in defaults and reports structural problems.
func (d *Dependency) Validate() error {
	d.From = strings.TrimSpace(d.From)
	d.To = strings.TrimSpace(d.To)
	if d.From == "" || d.To == "" {
		return fmt.Errorf("dependency: both from and to are required")
	}
	if d.From == d.To {
		return fmt.Errorf("dependency %s: self-loops are not allowed", d.From)
	}
	if d.Kind == "" {
		d.Kind = DepHTTP
	}
	if d.Criticality == "" {
		d.Criticality = CriticalityHard
	}
	return nil
}

func (d Dependency) String() string {
	return fmt.Sprintf("%s --%s/%s--> %s", d.From, d.Kind, d.Criticality, d.To)
}

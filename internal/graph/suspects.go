package graph

import (
	"fmt"
	"sort"
	"strings"

	"github.com/AndrewKarpaty/Argus/internal/domain"
)

// Suspect is a node that could explain the symptoms seen on a set of affected
// services, because those services (transitively) depend on it.
type Suspect struct {
	ServiceID string `json:"service_id"`
	// Coverage is the fraction of affected services that depend on this node.
	Coverage float64 `json:"coverage"`
	// Explains lists the affected services reachable from this node.
	Explains []string `json:"explains"`
	// MinDepth is the shortest hop distance to any affected service.
	MinDepth int `json:"min_depth"`
	// HardPath is true when at least one affected service reaches this node
	// over hard dependencies only.
	HardPath bool `json:"hard_path"`
	// Affected is true when this node is itself one of the affected services.
	Affected bool `json:"affected"`
	// Score ranks suspects: coverage first, then topology quality.
	Score float64 `json:"score"`
	// Reason is a short human-readable justification of the score.
	Reason string `json:"reason"`
}

// SuspectOptions tunes the search.
type SuspectOptions struct {
	// MaxDepth bounds how far upstream to look (<= 0 means unbounded).
	MaxDepth int
	// Limit caps the number of returned suspects (<= 0 means no cap).
	Limit int
	// IncludeSoft keeps candidates reachable only over soft dependencies.
	// They are kept by default but ranked below hard-path candidates.
	IncludeSoft bool
}

// DefaultSuspectOptions is a reasonable starting point for incident triage:
// four hops upstream covers most realistic propagation chains.
func DefaultSuspectOptions() SuspectOptions {
	return SuspectOptions{MaxDepth: 4, Limit: 10, IncludeSoft: true}
}

// Suspects ranks the shared upstream dependencies of the affected services.
//
// The idea behind the ranking: when several services alert at once, the cause
// is usually a node they all depend on, and the fewer hops away it is the more
// likely it is. A node that explains every symptom over hard dependencies
// outranks one that explains a single symptom over a soft one. Affected
// services are themselves candidates, because the origin of an outage is
// frequently one of the services that is visibly on fire.
func (g *Graph) Suspects(affected []string, opts SuspectOptions) []Suspect {
	known := make([]string, 0, len(affected))
	isAffected := make(map[string]bool, len(affected))
	for _, id := range affected {
		if g.Has(id) && !isAffected[id] {
			isAffected[id] = true
			known = append(known, id)
		}
	}
	if len(known) == 0 {
		return nil
	}

	type agg struct {
		explains map[string]bool
		minDepth int
		hard     bool
	}
	cand := make(map[string]*agg)
	touch := func(id, by string, depth int, hard bool) {
		a, ok := cand[id]
		if !ok {
			a = &agg{explains: map[string]bool{}, minDepth: depth}
			cand[id] = a
		}
		a.explains[by] = true
		if depth < a.minDepth {
			a.minDepth = depth
		}
		a.hard = a.hard || hard
	}

	for _, origin := range known {
		// The affected service explains itself at distance zero.
		touch(origin, origin, 0, true)
		for _, h := range g.Traverse(origin, Dependencies, opts.MaxDepth) {
			if !opts.IncludeSoft && !h.HardPath {
				continue
			}
			touch(h.ServiceID, origin, h.Depth, h.HardPath)
		}
	}

	out := make([]Suspect, 0, len(cand))
	for id, a := range cand {
		explains := make([]string, 0, len(a.explains))
		for s := range a.explains {
			explains = append(explains, s)
		}
		sort.Strings(explains)

		coverage := float64(len(explains)) / float64(len(known))
		s := Suspect{
			ServiceID: id,
			Coverage:  coverage,
			Explains:  explains,
			MinDepth:  a.minDepth,
			HardPath:  a.hard,
			Affected:  isAffected[id],
		}
		s.Score, s.Reason = g.scoreSuspect(s, len(known))
		out = append(out, s)
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].ServiceID < out[j].ServiceID
	})
	if opts.Limit > 0 && len(out) > opts.Limit {
		out = out[:opts.Limit]
	}
	return out
}

// scoreSuspect combines topology with catalog metadata. The weights are
// deliberately simple and explainable: this score is triage input for a human
// or a model, not a verdict.
func (g *Graph) scoreSuspect(s Suspect, affectedCount int) (float64, string) {
	score := s.Coverage
	var why []string
	if affectedCount > 1 && s.Coverage == 1 {
		score *= 1.5
		why = append(why, "explains every affected service")
	} else if affectedCount > 1 {
		why = append(why, fmt.Sprintf("explains %d/%d affected services", len(s.Explains), affectedCount))
	}

	// Closer causes are likelier than distant ones, but depth 0 (the affected
	// service itself) gets no bonus: being on fire is not evidence of being
	// the origin of the fire.
	switch {
	case s.MinDepth == 1:
		score *= 1.2
		why = append(why, "direct dependency of an affected service")
	case s.MinDepth > 1:
		score *= 1.0 / (1.0 + 0.15*float64(s.MinDepth-1))
		why = append(why, fmt.Sprintf("%d hops upstream", s.MinDepth))
	default:
		why = append(why, "itself reporting symptoms")
	}

	if !s.HardPath {
		score *= 0.6
		why = append(why, "reachable only over soft dependencies")
	}
	if svc, ok := g.Service(s.ServiceID); ok {
		score *= svc.Tier.Weight()
		if svc.Tier == domain.TierCritical {
			why = append(why, "critical tier")
		}
		// A shared datastore or queue is a classic common-mode failure.
		switch svc.Kind {
		case domain.KindDatabase, domain.KindQueue, domain.KindCache:
			score *= 1.1
			why = append(why, fmt.Sprintf("shared %s", svc.Kind))
		case domain.KindExternal:
			why = append(why, "external provider")
		}
	}
	return round(score), strings.Join(why, "; ")
}

func round(f float64) float64 {
	return float64(int64(f*1000+0.5)) / 1000
}

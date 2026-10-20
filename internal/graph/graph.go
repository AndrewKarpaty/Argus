// Package graph holds the service dependency graph and the traversals an
// incident investigation needs: who depends on whom, what the blast radius of
// a node is, and which shared upstream nodes could explain a set of symptoms
// observed at the same time.
//
// A Graph is immutable once built, so it is safe to share across goroutines.
// Reload the catalog by building a new Graph and swapping the pointer.
package graph

import (
	"fmt"
	"sort"

	"github.com/AndrewKarpaty/Argus/internal/domain"
)

// Direction selects which way a traversal follows the edges.
type Direction string

const (
	// Dependencies follows From -> To: what this service calls, i.e. where
	// its own failures can come from.
	Dependencies Direction = "dependencies"
	// Dependents follows To -> From: who calls this service, i.e. what
	// breaks when it fails.
	Dependents Direction = "dependents"
)

// Valid reports whether d is a known direction.
func (d Direction) Valid() bool { return d == Dependencies || d == Dependents }

// Graph is a directed multigraph over catalog nodes.
type Graph struct {
	services map[string]domain.Service
	out      map[string][]domain.Dependency // keyed by From
	in       map[string][]domain.Dependency // keyed by To
	ids      []string                       // sorted, for deterministic output
	edges    []domain.Dependency
}

// New validates the catalog and builds the graph. Edges that reference an
// unknown service are rejected rather than silently dropped: a catalog that
// lies about its topology is worse than no catalog at all.
func New(services []domain.Service, deps []domain.Dependency) (*Graph, error) {
	g := &Graph{
		services: make(map[string]domain.Service, len(services)),
		out:      make(map[string][]domain.Dependency),
		in:       make(map[string][]domain.Dependency),
	}
	for _, s := range services {
		if err := s.Validate(); err != nil {
			return nil, err
		}
		if _, dup := g.services[s.ID]; dup {
			return nil, fmt.Errorf("graph: duplicate service id %q", s.ID)
		}
		g.services[s.ID] = s
		g.ids = append(g.ids, s.ID)
	}
	sort.Strings(g.ids)

	seen := make(map[string]struct{}, len(deps))
	for _, d := range deps {
		if err := d.Validate(); err != nil {
			return nil, err
		}
		if _, ok := g.services[d.From]; !ok {
			return nil, fmt.Errorf("graph: dependency %s references unknown service %q", d, d.From)
		}
		if _, ok := g.services[d.To]; !ok {
			return nil, fmt.Errorf("graph: dependency %s references unknown service %q", d, d.To)
		}
		key := d.From + "\x00" + d.To + "\x00" + string(d.Kind)
		if _, dup := seen[key]; dup {
			continue // tolerate a repeated edge, it carries no new information
		}
		seen[key] = struct{}{}
		g.out[d.From] = append(g.out[d.From], d)
		g.in[d.To] = append(g.in[d.To], d)
		g.edges = append(g.edges, d)
	}
	for id := range g.out {
		sort.Slice(g.out[id], func(a, b int) bool { return g.out[id][a].To < g.out[id][b].To })
	}
	for id := range g.in {
		sort.Slice(g.in[id], func(a, b int) bool { return g.in[id][a].From < g.in[id][b].From })
	}
	return g, nil
}

// Size reports the node and edge counts.
func (g *Graph) Size() (services, dependencies int) { return len(g.services), len(g.edges) }

// Service looks up one node.
func (g *Graph) Service(id string) (domain.Service, bool) {
	s, ok := g.services[id]
	return s, ok
}

// Has reports whether the node exists.
func (g *Graph) Has(id string) bool {
	_, ok := g.services[id]
	return ok
}

// Services returns every node, ordered by id.
func (g *Graph) Services() []domain.Service {
	out := make([]domain.Service, 0, len(g.ids))
	for _, id := range g.ids {
		out = append(out, g.services[id])
	}
	return out
}

// Edges returns every dependency edge in insertion order.
func (g *Graph) Edges() []domain.Dependency {
	out := make([]domain.Dependency, len(g.edges))
	copy(out, g.edges)
	return out
}

// Neighbours returns the direct edges of id in the given direction.
func (g *Graph) Neighbours(id string, dir Direction) []domain.Dependency {
	var src []domain.Dependency
	if dir == Dependents {
		src = g.in[id]
	} else {
		src = g.out[id]
	}
	out := make([]domain.Dependency, len(src))
	copy(out, src)
	return out
}

// Peer returns the node at the far end of edge e when traversing in dir.
func Peer(e domain.Dependency, dir Direction) string {
	if dir == Dependents {
		return e.From
	}
	return e.To
}

// Hop is a node reached by a traversal, with the distance it was reached at
// and the edge that led to it.
type Hop struct {
	ServiceID   string                `json:"service_id"`
	Depth       int                   `json:"depth"`
	Via         string                `json:"via"`
	Kind        domain.DependencyKind `json:"kind"`
	Criticality domain.Criticality    `json:"criticality"`
	// HardPath is true when every edge from the origin to this node is a hard
	// dependency, i.e. a failure here can fully explain a failure there.
	HardPath bool `json:"hard_path"`
}

// Traverse walks the graph from id in the given direction, breadth first, up
// to maxDepth (<= 0 means unbounded). The origin itself is not returned.
// Results are ordered by depth and then by service id.
func (g *Graph) Traverse(id string, dir Direction, maxDepth int) []Hop {
	if !g.Has(id) {
		return nil
	}
	type queued struct {
		id       string
		depth    int
		hardPath bool
	}
	visited := map[string]bool{id: true}
	hardTo := map[string]bool{id: true}
	queue := []queued{{id: id, depth: 0, hardPath: true}}
	var hops []Hop

	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if maxDepth > 0 && cur.depth >= maxDepth {
			continue
		}
		for _, e := range g.Neighbours(cur.id, dir) {
			peer := Peer(e, dir)
			hard := cur.hardPath && e.Criticality == domain.CriticalityHard
			if visited[peer] {
				// Keep the strongest known path: a hard route to an already
				// seen node is more informative than a soft one.
				if hard && !hardTo[peer] {
					hardTo[peer] = true
					for i := range hops {
						if hops[i].ServiceID == peer {
							hops[i].HardPath = true
							break
						}
					}
				}
				continue
			}
			visited[peer] = true
			hardTo[peer] = hard
			hops = append(hops, Hop{
				ServiceID:   peer,
				Depth:       cur.depth + 1,
				Via:         cur.id,
				Kind:        e.Kind,
				Criticality: e.Criticality,
				HardPath:    hard,
			})
			queue = append(queue, queued{id: peer, depth: cur.depth + 1, hardPath: hard})
		}
	}
	sort.SliceStable(hops, func(a, b int) bool {
		if hops[a].Depth != hops[b].Depth {
			return hops[a].Depth < hops[b].Depth
		}
		return hops[a].ServiceID < hops[b].ServiceID
	})
	return hops
}

// BlastRadius returns every node that transitively depends on id, i.e. what is
// at risk while id is unhealthy.
func (g *Graph) BlastRadius(id string) []Hop { return g.Traverse(id, Dependents, 0) }

// Upstream returns every node id transitively depends on, i.e. the candidate
// sources of its failures.
func (g *Graph) Upstream(id string, maxDepth int) []Hop {
	return g.Traverse(id, Dependencies, maxDepth)
}

// Path returns the shortest dependency path from -> ... -> to, following the
// From->To direction. It returns nil when no such path exists.
func (g *Graph) Path(from, to string) []string {
	if !g.Has(from) || !g.Has(to) {
		return nil
	}
	if from == to {
		return []string{from}
	}
	prev := map[string]string{from: ""}
	queue := []string{from}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, e := range g.out[cur] {
			if _, seen := prev[e.To]; seen {
				continue
			}
			prev[e.To] = cur
			if e.To == to {
				return buildPath(prev, from, to)
			}
			queue = append(queue, e.To)
		}
	}
	return nil
}

func buildPath(prev map[string]string, from, to string) []string {
	var rev []string
	for cur := to; cur != ""; cur = prev[cur] {
		rev = append(rev, cur)
		if cur == from {
			break
		}
	}
	for i, j := 0, len(rev)-1; i < j; i, j = i+1, j-1 {
		rev[i], rev[j] = rev[j], rev[i]
	}
	return rev
}

// TopoOrder returns the nodes ordered so that every service appears before
// the services it depends on. It fails when the graph contains a cycle.
func (g *Graph) TopoOrder() ([]string, error) {
	indeg := make(map[string]int, len(g.ids))
	for _, id := range g.ids {
		indeg[id] = len(g.in[id])
	}
	var ready []string
	for _, id := range g.ids {
		if indeg[id] == 0 {
			ready = append(ready, id)
		}
	}
	var order []string
	for len(ready) > 0 {
		sort.Strings(ready)
		cur := ready[0]
		ready = ready[1:]
		order = append(order, cur)
		for _, e := range g.out[cur] {
			indeg[e.To]--
			if indeg[e.To] == 0 {
				ready = append(ready, e.To)
			}
		}
	}
	if len(order) != len(g.ids) {
		return nil, fmt.Errorf("graph: topological order undefined, %d nodes are part of a cycle", len(g.ids)-len(order))
	}
	return order, nil
}

// Cycles returns the strongly connected components with more than one node,
// plus any self-reachable single node. Circular dependencies make root-cause
// reasoning ambiguous, so they are worth surfacing to the user explicitly.
func (g *Graph) Cycles() [][]string {
	index := make(map[string]int)
	low := make(map[string]int)
	onStack := make(map[string]bool)
	var stack []string
	counter := 0
	var out [][]string

	var strongConnect func(v string)
	strongConnect = func(v string) {
		index[v] = counter
		low[v] = counter
		counter++
		stack = append(stack, v)
		onStack[v] = true

		for _, e := range g.out[v] {
			w := e.To
			if _, ok := index[w]; !ok {
				strongConnect(w)
				if low[w] < low[v] {
					low[v] = low[w]
				}
			} else if onStack[w] {
				if index[w] < low[v] {
					low[v] = index[w]
				}
			}
		}

		if low[v] == index[v] {
			var comp []string
			for {
				w := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				onStack[w] = false
				comp = append(comp, w)
				if w == v {
					break
				}
			}
			if len(comp) > 1 {
				sort.Strings(comp)
				out = append(out, comp)
			}
		}
	}

	for _, id := range g.ids {
		if _, ok := index[id]; !ok {
			strongConnect(id)
		}
	}
	sort.Slice(out, func(a, b int) bool { return out[a][0] < out[b][0] })
	return out
}

// Subgraph returns the graph induced by seeds plus everything within radius
// hops of them in either direction. This is what gets handed to the model: a
// 200-service catalog does not fit a useful prompt, the relevant neighbourhood
// does.
func (g *Graph) Subgraph(seeds []string, radius int) *Graph {
	keep := make(map[string]bool)
	for _, s := range seeds {
		if !g.Has(s) {
			continue
		}
		keep[s] = true
		for _, dir := range []Direction{Dependencies, Dependents} {
			for _, h := range g.Traverse(s, dir, radius) {
				keep[h.ServiceID] = true
			}
		}
	}
	var services []domain.Service
	for _, id := range g.ids {
		if keep[id] {
			services = append(services, g.services[id])
		}
	}
	var deps []domain.Dependency
	for _, e := range g.edges {
		if keep[e.From] && keep[e.To] {
			deps = append(deps, e)
		}
	}
	sub, err := New(services, deps)
	if err != nil {
		// Cannot happen: an induced subgraph of a valid graph is valid.
		return g
	}
	return sub
}

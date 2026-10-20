package graph

import (
	"fmt"
	"sort"
	"strings"

	"github.com/AndrewKarpaty/Argus/internal/domain"
)

// Mermaid renders the graph as a Mermaid flowchart, for dashboards and
// incident write-ups. Hard dependencies are solid arrows, soft ones dotted.
func (g *Graph) Mermaid(highlight ...string) string {
	hot := make(map[string]bool, len(highlight))
	for _, id := range highlight {
		hot[id] = true
	}
	var b strings.Builder
	b.WriteString("flowchart LR\n")
	for _, id := range g.ids {
		s := g.services[id]
		shapeL, shapeR := "[", "]"
		switch s.Kind {
		case domain.KindDatabase, domain.KindCache:
			shapeL, shapeR = "[(", ")]"
		case domain.KindQueue:
			shapeL, shapeR = "[/", "/]"
		case domain.KindExternal:
			shapeL, shapeR = "{{", "}}"
		}
		fmt.Fprintf(&b, "  %s%s%q%s\n", mermaidID(id), shapeL, s.Name, shapeR)
	}
	for _, e := range g.edges {
		arrow := "-->"
		if e.Criticality == domain.CriticalitySoft {
			arrow = "-.->"
		}
		fmt.Fprintf(&b, "  %s %s|%s| %s\n", mermaidID(e.From), arrow, e.Kind, mermaidID(e.To))
	}
	if len(hot) > 0 {
		b.WriteString("  classDef affected fill:#fee,stroke:#c00,stroke-width:2px;\n")
		var ids []string
		for id := range hot {
			if g.Has(id) {
				ids = append(ids, mermaidID(id))
			}
		}
		sort.Strings(ids)
		if len(ids) > 0 {
			fmt.Fprintf(&b, "  class %s affected;\n", strings.Join(ids, ","))
		}
	}
	return b.String()
}

func mermaidID(id string) string {
	return strings.NewReplacer("-", "_", ".", "_", "/", "_", " ", "_").Replace(id)
}

// Describe renders a compact text view of the graph for an LLM prompt. It is
// deliberately terse and stable: the same catalog must produce byte-identical
// output so that the prompt prefix stays cacheable.
func (g *Graph) Describe() string {
	var b strings.Builder
	for _, id := range g.ids {
		s := g.services[id]
		fmt.Fprintf(&b, "- %s (%s, tier=%s", s.ID, s.Kind, s.Tier)
		if s.Team != "" {
			fmt.Fprintf(&b, ", team=%s", s.Team)
		}
		b.WriteString(")")
		if s.Description != "" {
			fmt.Fprintf(&b, ": %s", s.Description)
		}
		b.WriteString("\n")
		deps := g.out[id]
		if len(deps) == 0 {
			b.WriteString("    depends on: -\n")
			continue
		}
		parts := make([]string, 0, len(deps))
		for _, e := range deps {
			parts = append(parts, fmt.Sprintf("%s [%s/%s]", e.To, e.Kind, e.Criticality))
		}
		fmt.Fprintf(&b, "    depends on: %s\n", strings.Join(parts, ", "))
	}
	return b.String()
}

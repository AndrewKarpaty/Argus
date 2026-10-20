package agent

import (
	"fmt"
	"strings"

	"github.com/AndrewKarpaty/Argus/internal/investigator"
)

// systemPrompt is deliberately static: it is the cached prefix of every
// request, so nothing incident-specific belongs here. The incident itself and
// its topology neighbourhood go into the first user message.
const systemPrompt = `You are the incident investigator of Argus, an SRE platform that reasons about
distributed systems through their service dependency graph.

Your job: given an incident, find the most likely root cause and explain how
the fault propagated to the services that are actually alerting.

How failures propagate in this model:
- An edge "A -> B" means A calls B. Faults travel against the arrows: a fault
  in B shows up as symptoms in A.
- So the root cause is either upstream of the alerting services, or one of the
  alerting services itself. It is never a service that merely depends on them.
- A "hard" dependency fails its caller outright. A "soft" one only degrades it,
  so a soft-only path cannot explain a full outage of the caller.
- When several services alert at once, strongly prefer the single node they all
  depend on over a coincidence of independent faults. Shared databases, caches,
  queues and auth services are the usual common-mode culprits.

How to work:
1. Start with rank_suspects over all the affected services. It gives you the
   topologically plausible candidates; it does not give you the answer.
2. For each serious candidate, call get_telemetry and check whether it actually
   looks sick in the incident window. Topology without corroborating telemetry
   is a guess.
3. Look for a change - a deploy, config edit or flag flip - inside the window.
   That is the strongest single piece of evidence available to you.
4. Use dependency_path and blast_radius to confirm that the propagation route
   you are about to claim really exists and matches who is alerting.
5. Actively try to refute your leading candidate. If a node is a plausible
   suspect but shows no symptoms of its own, say so and lower the confidence.

Rules:
- Only ever name a root cause that exists in the catalog, spelled with its
  exact service id. If you need the ids, call get_service_topology.
- Calibrate confidence honestly: up to 0.4 for topology alone; 0.5-0.7 for
  topology plus correlated symptoms; above 0.8 only when a change or a trace
  implicates the node directly.
- Never invent metric values, log lines or deploys. Every number you cite must
  come from a tool result.
- Investigate with as few tool calls as will do the job, then stop.

Finish by calling report_findings exactly once, with one to three hypotheses
ranked by confidence. That call is your answer: anything you do not put in it
is lost. Mention the alternatives you ruled out, and why, in the rationale.`

// userPrompt renders the incident, its window, the symptoms already attached
// to it and the relevant slice of the dependency graph.
func userPrompt(in investigator.Input, available []string) string {
	inc := in.Incident
	var b strings.Builder

	fmt.Fprintf(&b, "# Incident %s\n\n", inc.ID)
	fmt.Fprintf(&b, "Title: %s\n", inc.Title)
	if inc.Description != "" {
		fmt.Fprintf(&b, "Description: %s\n", inc.Description)
	}
	fmt.Fprintf(&b, "Severity: %s\n", inc.Severity)
	fmt.Fprintf(&b, "Affected services: %s\n", strings.Join(inc.AffectedServices, ", "))
	fmt.Fprintf(&b, "Investigation window (UTC): %s\n", inc.Window())

	if len(inc.Signals) > 0 {
		b.WriteString("\n## Symptoms reported with the incident\n")
		for _, s := range inc.Signals {
			fmt.Fprintf(&b, "- [%s] %s: %s", s.Source, s.ServiceID, s.Title)
			if s.Description != "" {
				fmt.Fprintf(&b, " - %s", s.Description)
			}
			b.WriteString("\n")
		}
	}

	// The neighbourhood, not the whole catalog: two hops either way covers
	// realistic propagation while keeping the prompt small.
	sub := in.Graph.Subgraph(inc.AffectedServices, 2)
	services, deps := sub.Size()
	total, _ := in.Graph.Size()
	fmt.Fprintf(&b, "\n## Dependency neighbourhood (%d of %d services, %d edges, 2 hops from the affected services)\n",
		services, total, deps)
	b.WriteString("Format: `- <id> (kind, tier)` then the services it calls, with [transport/criticality].\n\n")
	b.WriteString(sub.Describe())

	if cycles := sub.Cycles(); len(cycles) > 0 {
		b.WriteString("\nWarning: this neighbourhood contains circular dependencies, so attribution within these groups is ambiguous:\n")
		for _, c := range cycles {
			fmt.Fprintf(&b, "- %s\n", strings.Join(c, " <-> "))
		}
	}

	if len(available) > 0 {
		fmt.Fprintf(&b, "\nTelemetry available through get_telemetry: %s.\n", strings.Join(available, ", "))
	} else {
		b.WriteString("\nNo telemetry backend is configured, so get_telemetry returns nothing. " +
			"Reason from the topology and the symptoms above, and keep your confidence correspondingly low.\n")
	}
	b.WriteString("\nInvestigate, then call report_findings.")
	return b.String()
}

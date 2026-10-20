// Command argusctl runs an investigation, or inspects the dependency graph,
// from the command line - without starting the server. It is the fastest way
// to see what the platform concludes about a given set of symptoms.
//
//	argusctl investigate -service checkout -service orders
//	argusctl graph -service payments -format mermaid
//	argusctl suspects -service checkout -service orders
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/AndrewKarpaty/Argus/internal/agent"
	"github.com/AndrewKarpaty/Argus/internal/catalog"
	"github.com/AndrewKarpaty/Argus/internal/config"
	"github.com/AndrewKarpaty/Argus/internal/domain"
	"github.com/AndrewKarpaty/Argus/internal/graph"
	"github.com/AndrewKarpaty/Argus/internal/investigator"
	"github.com/AndrewKarpaty/Argus/internal/store"
	"github.com/AndrewKarpaty/Argus/internal/telemetry"
	"github.com/AndrewKarpaty/Argus/internal/telemetry/demo"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			os.Exit(2)
		}
		fmt.Fprintf(os.Stderr, "argusctl: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		usage()
		return errors.New("a subcommand is required")
	}
	switch args[0] {
	case "investigate":
		return cmdInvestigate(args[1:])
	case "graph":
		return cmdGraph(args[1:])
	case "suspects":
		return cmdSuspects(args[1:])
	case "help", "-h", "--help":
		usage()
		return nil
	default:
		usage()
		return fmt.Errorf("unknown subcommand %q", args[0])
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `argusctl - inspect the dependency graph and run investigations locally

Usage:
  argusctl investigate -service <id> [-service <id>...] [flags]
  argusctl graph [-service <id>...] [-format json|text|mermaid] [flags]
  argusctl suspects -service <id> [-service <id>...] [flags]

Run a subcommand with -h for its flags.
`)
}

// serviceList collects a repeatable -service flag.
type serviceList []string

func (s *serviceList) String() string { return strings.Join(*s, ",") }

func (s *serviceList) Set(v string) error {
	for _, part := range strings.Split(v, ",") {
		if part = strings.TrimSpace(part); part != "" {
			*s = append(*s, part)
		}
	}
	return nil
}

// commonFlags are shared by every subcommand.
type commonFlags struct {
	catalogPath string
	services    serviceList
	asJSON      bool
	verbose     bool
}

func (c *commonFlags) register(fs *flag.FlagSet) {
	fs.StringVar(&c.catalogPath, "catalog", "configs/services.yaml", "path to the service catalog")
	fs.Var(&c.services, "service", "service id (repeat, or comma-separate, for several)")
	fs.BoolVar(&c.asJSON, "json", false, "print raw JSON instead of a human-readable report")
	fs.BoolVar(&c.verbose, "v", false, "log what the investigation does, including every tool call")
}

func (c *commonFlags) logger() *slog.Logger {
	level := slog.LevelWarn
	if c.verbose {
		level = slog.LevelDebug
	}
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
}

func (c *commonFlags) loadGraph(ctx context.Context, log *slog.Logger) (*catalog.Provider, error) {
	return catalog.NewProvider(ctx, catalog.NewFile(c.catalogPath), log)
}

func cmdInvestigate(args []string) error {
	fs := flag.NewFlagSet("investigate", flag.ExitOnError)
	var common commonFlags
	common.register(fs)
	title := fs.String("title", "", "incident title (defaults to a description of the affected services)")
	engineName := fs.String("engine", "", "reasoner: auto, claude or heuristic (default: ARGUS_ENGINE, or auto)")
	faulty := fs.String("demo-faulty", "postgres-main", "which service the synthetic telemetry blames; empty disables demo telemetry")
	lookback := fs.Duration("lookback", 30*time.Minute, "how far back the incident window reaches")
	timeout := fs.Duration("timeout", 5*time.Minute, "investigation timeout")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if len(common.services) == 0 {
		fs.Usage()
		return errors.New("at least one -service is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout+30*time.Second)
	defer cancel()

	log := common.logger()
	provider, err := common.loadGraph(ctx, log)
	if err != nil {
		return err
	}

	collector := &telemetry.Collector{Log: log, Limit: 20}
	if *faulty != "" {
		p := demo.New(*faulty)
		collector.Metrics, collector.Logs, collector.Traces, collector.Changes = p, p, p, p
	}

	reasoner, err := pickReasoner(*engineName, log)
	if err != nil {
		return err
	}

	incidents := store.NewMemoryIncidents()
	investigations := store.NewMemoryInvestigations()
	engine, err := investigator.NewEngine(investigator.Config{
		Reasoner:       reasoner,
		Catalog:        provider,
		Collector:      collector,
		Incidents:      incidents,
		Investigations: investigations,
		Log:            log,
		Timeout:        *timeout,
	})
	if err != nil {
		return err
	}

	now := time.Now().UTC()
	incidentTitle := *title
	if incidentTitle == "" {
		incidentTitle = "Symptoms reported on " + strings.Join(common.services, ", ")
	}
	inc := domain.Incident{
		ID:               domain.NewID("inc"),
		Title:            incidentTitle,
		Severity:         domain.SeverityHigh,
		AffectedServices: common.services,
		StartedAt:        now.Add(-*lookback),
		DetectedAt:       now,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if err := inc.Validate(); err != nil {
		return err
	}
	if err := incidents.Create(ctx, inc); err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "investigating %s with engine %q...\n",
		strings.Join(common.services, ", "), reasoner.Name())
	iv, err := engine.Run(ctx, inc.ID)
	if err != nil {
		return err
	}
	if common.asJSON {
		return printJSON(iv)
	}
	printInvestigation(iv)
	return nil
}

func pickReasoner(name string, log *slog.Logger) (investigator.Reasoner, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	if name != "" {
		cfg.Engine = config.Engine(strings.ToLower(name))
	}
	switch engine := cfg.ResolvedEngine(); engine {
	case config.EngineClaude:
		return agent.New(agent.Config{
			APIKey:        cfg.AnthropicAPIKey,
			Model:         cfg.Model,
			Effort:        cfg.Effort,
			MaxTokens:     cfg.MaxTokens,
			MaxIterations: cfg.MaxIterations,
			BaseURL:       cfg.AnthropicBaseURL,
			Log:           log,
		})
	case config.EngineHeuristic:
		return investigator.NewHeuristic(), nil
	default:
		return nil, fmt.Errorf("unsupported engine %q", engine)
	}
}

func cmdGraph(args []string) error {
	fs := flag.NewFlagSet("graph", flag.ExitOnError)
	var common commonFlags
	common.register(fs)
	format := fs.String("format", "text", "output format: text, mermaid or json")
	radius := fs.Int("radius", 2, "hops of neighbours to include around -service")
	blast := fs.Bool("blast-radius", false, "instead of the graph, list what breaks when -service is unhealthy")
	if err := fs.Parse(args); err != nil {
		return err
	}

	ctx := context.Background()
	provider, err := common.loadGraph(ctx, common.logger())
	if err != nil {
		return err
	}
	g := provider.Graph()

	if *blast {
		if len(common.services) == 0 {
			return errors.New("-blast-radius needs at least one -service")
		}
		for _, id := range common.services {
			if !g.Has(id) {
				return fmt.Errorf("unknown service %q", id)
			}
			hops := g.BlastRadius(id)
			if common.asJSON {
				if err := printJSON(map[string]any{"service": id, "affected": hops}); err != nil {
					return err
				}
				continue
			}
			fmt.Printf("%s is depended on by %d service(s):\n", id, len(hops))
			for _, h := range hops {
				fmt.Printf("  %-18s depth=%d via=%-14s %s/%s hard_path=%v\n",
					h.ServiceID, h.Depth, h.Via, h.Kind, h.Criticality, h.HardPath)
			}
		}
		return nil
	}

	if len(common.services) > 0 {
		for _, id := range common.services {
			if !g.Has(id) {
				return fmt.Errorf("unknown service %q", id)
			}
		}
		g = g.Subgraph(common.services, *radius)
	}

	switch *format {
	case "mermaid":
		fmt.Print(g.Mermaid(common.services...))
	case "text":
		services, deps := g.Size()
		fmt.Printf("%d services, %d dependencies\n\n%s", services, deps, g.Describe())
		if cycles := g.Cycles(); len(cycles) > 0 {
			fmt.Printf("\ncircular dependencies:\n")
			for _, c := range cycles {
				fmt.Printf("  %s\n", strings.Join(c, " <-> "))
			}
		}
	case "json":
		return printJSON(map[string]any{
			"services":     g.Services(),
			"dependencies": g.Edges(),
			"cycles":       g.Cycles(),
		})
	default:
		return fmt.Errorf("unknown format %q", *format)
	}
	return nil
}

func cmdSuspects(args []string) error {
	fs := flag.NewFlagSet("suspects", flag.ExitOnError)
	var common commonFlags
	common.register(fs)
	depth := fs.Int("depth", 4, "how far upstream to search")
	limit := fs.Int("limit", 10, "how many candidates to print")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if len(common.services) == 0 {
		fs.Usage()
		return errors.New("at least one -service is required")
	}

	provider, err := common.loadGraph(context.Background(), common.logger())
	if err != nil {
		return err
	}
	suspects := provider.Graph().Suspects(common.services, graph.SuspectOptions{
		MaxDepth: *depth, Limit: *limit, IncludeSoft: true,
	})
	if len(suspects) == 0 {
		return fmt.Errorf("none of %s are in the catalog", strings.Join(common.services, ", "))
	}
	if common.asJSON {
		return printJSON(suspects)
	}
	fmt.Printf("shared upstream candidates for %s:\n\n", strings.Join(common.services, ", "))
	fmt.Printf("%-18s %7s %8s %5s %-9s %s\n", "SERVICE", "SCORE", "COVERAGE", "HOPS", "HARD-PATH", "WHY")
	for _, s := range suspects {
		fmt.Printf("%-18s %7.3f %7.0f%% %5d %-9v %s\n",
			s.ServiceID, s.Score, s.Coverage*100, s.MinDepth, s.HardPath, s.Reason)
	}
	return nil
}

func printInvestigation(iv domain.Investigation) {
	fmt.Printf("\n=== investigation %s (%s, engine %s) ===\n", iv.ID, iv.Status, iv.Engine)
	if iv.Error != "" {
		fmt.Printf("error: %s\n", iv.Error)
	}
	if iv.Summary != "" {
		fmt.Printf("\n%s\n", iv.Summary)
	}
	for i, h := range iv.Hypotheses {
		fmt.Printf("\n--- hypothesis %d: %s (confidence %.2f) ---\n", i+1, h.Title, h.Confidence)
		fmt.Printf("root cause: %s\n", h.RootCauseService)
		if len(h.PropagationPath) > 1 {
			fmt.Printf("propagation: %s\n", strings.Join(h.PropagationPath, " -> "))
		}
		fmt.Printf("rationale: %s\n", h.Rationale)
		if len(h.Evidence) > 0 {
			fmt.Println("evidence:")
			for _, e := range h.Evidence {
				fmt.Printf("  [%s] %s\n", e.Kind, e.Summary)
			}
		}
		if len(h.SuggestedActions) > 0 {
			fmt.Println("next steps:")
			for _, a := range h.SuggestedActions {
				fmt.Printf("  - %s\n", a)
			}
		}
	}
	if len(iv.Steps) > 0 {
		fmt.Printf("\n--- trail (%d steps) ---\n", len(iv.Steps))
		for _, s := range iv.Steps {
			status := ""
			if s.Error != "" {
				status = " ERROR: " + s.Error
			}
			fmt.Printf("  %2d. %-22s %5dms %s%s\n", s.Seq, s.Tool, s.DurationMS, firstLine(s.Input), status)
		}
	}
	if iv.Usage != nil && (iv.Usage.InputTokens > 0 || iv.Usage.OutputTokens > 0) {
		fmt.Printf("\ntokens: %d in, %d out, %d cache read\n",
			iv.Usage.InputTokens, iv.Usage.OutputTokens, iv.Usage.CacheReadTokens)
	}
	fmt.Println()
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 80 {
		s = s[:80] + "..."
	}
	return s
}

func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

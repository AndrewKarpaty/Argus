// Package agent implements the Claude-driven investigator: the reasoner that
// explores the dependency graph and the telemetry through tools, instead of
// being handed a fixed context and asked to guess.
//
// It satisfies investigator.Reasoner, so it is interchangeable with the
// deterministic heuristic engine.
package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"github.com/AndrewKarpaty/Argus/internal/domain"
	"github.com/AndrewKarpaty/Argus/internal/investigator"
)

// DefaultModel is the model every investigation uses unless configured
// otherwise.
const DefaultModel = "claude-opus-5-5"

// Config configures the Claude reasoner.
type Config struct {
	// APIKey is optional: when empty the SDK resolves credentials from the
	// environment (ANTHROPIC_API_KEY, or a profile from `ant auth login`).
	APIKey string
	// Model defaults to DefaultModel.
	Model string
	// Effort is one of low, medium, high, xhigh, max. Incident investigation
	// is multi-step tool use where a wrong answer is expensive, so this
	// defaults to high rather than the model's own default.
	Effort string
	// MaxTokens bounds one response. Defaults to 16000.
	MaxTokens int64
	// MaxIterations bounds the tool-use loop, which is the real cost control
	// here. Defaults to 12; 0 would mean unbounded.
	MaxIterations int
	// BaseURL overrides the API endpoint (for a gateway or a test server).
	BaseURL string
	Log     *slog.Logger
}

func (c *Config) applyDefaults() {
	if c.Model == "" {
		c.Model = DefaultModel
	}
	if c.Effort == "" {
		c.Effort = string(anthropic.BetaOutputConfigEffortHigh)
	}
	if c.MaxTokens <= 0 {
		c.MaxTokens = 16000
	}
	if c.MaxIterations <= 0 {
		c.MaxIterations = 12
	}
	if c.Log == nil {
		c.Log = slog.Default()
	}
}

// Claude is an investigator.Reasoner backed by the Claude API.
type Claude struct {
	client anthropic.Client
	cfg    Config
	effort anthropic.BetaOutputConfigEffort
}

var _ investigator.Reasoner = (*Claude)(nil)

// New returns a Claude reasoner.
func New(cfg Config) (*Claude, error) {
	cfg.applyDefaults()

	effort, err := parseEffort(cfg.Effort)
	if err != nil {
		return nil, err
	}

	var opts []option.RequestOption
	if cfg.APIKey != "" {
		opts = append(opts, option.WithAPIKey(cfg.APIKey))
	}
	if cfg.BaseURL != "" {
		opts = append(opts, option.WithBaseURL(cfg.BaseURL))
	}
	return &Claude{client: anthropic.NewClient(opts...), cfg: cfg, effort: effort}, nil
}

func parseEffort(s string) (anthropic.BetaOutputConfigEffort, error) {
	switch anthropic.BetaOutputConfigEffort(strings.ToLower(s)) {
	case anthropic.BetaOutputConfigEffortLow:
		return anthropic.BetaOutputConfigEffortLow, nil
	case anthropic.BetaOutputConfigEffortMedium:
		return anthropic.BetaOutputConfigEffortMedium, nil
	case anthropic.BetaOutputConfigEffortHigh:
		return anthropic.BetaOutputConfigEffortHigh, nil
	case anthropic.BetaOutputConfigEffortXhigh:
		return anthropic.BetaOutputConfigEffortXhigh, nil
	case anthropic.BetaOutputConfigEffortMax:
		return anthropic.BetaOutputConfigEffortMax, nil
	default:
		return "", fmt.Errorf("agent: unknown effort %q, want low, medium, high, xhigh or max", s)
	}
}

// Name identifies the engine in stored investigations.
func (c *Claude) Name() string { return "claude:" + c.cfg.Model }

// Analyze runs the agentic investigation loop.
func (c *Claude) Analyze(ctx context.Context, in investigator.Input) (domain.Analysis, error) {
	if in.Graph == nil {
		return domain.Analysis{}, errors.New("agent: a dependency graph is required")
	}
	log := c.cfg.Log.With("incident", in.Incident.ID, "model", c.cfg.Model)
	sess := newSession(in, log)

	tools, err := sess.tools()
	if err != nil {
		return domain.Analysis{}, fmt.Errorf("agent: build tools: %w", err)
	}

	var available []string
	if in.Collector != nil {
		available = in.Collector.Available()
	}

	runner := c.client.Beta.Messages.NewToolRunner(tools, anthropic.BetaToolRunnerParams{
		BetaMessageNewParams: anthropic.BetaMessageNewParams{
			Model:     anthropic.Model(c.cfg.Model),
			MaxTokens: c.cfg.MaxTokens,
			// The system prompt and the tool definitions are identical across
			// every investigation, so cache them: the per-incident content all
			// lives in the user message after this breakpoint.
			System: []anthropic.BetaTextBlockParam{{
				Text:         systemPrompt,
				CacheControl: anthropic.NewBetaCacheControlEphemeralParam(),
			}},
			OutputConfig: anthropic.BetaOutputConfigParam{Effort: c.effort},
			Messages: []anthropic.BetaMessageParam{
				anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock(userPrompt(in, available))),
			},
		},
		MaxIterations: c.cfg.MaxIterations,
	})

	// Iterate rather than RunToCompletion so that token usage can be summed
	// across every turn of the loop, not just the last one.
	var usage domain.TokenUsage
	var last *anthropic.BetaMessage
	turns := 0
	for msg, err := range runner.All(ctx) {
		if err != nil {
			return domain.Analysis{Steps: sess.Steps(), Usage: &usage}, fmt.Errorf("agent: investigation loop: %w", err)
		}
		turns++
		usage.InputTokens += msg.Usage.InputTokens
		usage.OutputTokens += msg.Usage.OutputTokens
		usage.CacheReadTokens += msg.Usage.CacheReadInputTokens
		usage.CacheWriteTokens += msg.Usage.CacheCreationInputTokens
		last = msg
	}
	if last == nil {
		return domain.Analysis{Steps: sess.Steps(), Usage: &usage}, errors.New("agent: the model returned no response")
	}
	if last.StopReason == anthropic.BetaStopReasonRefusal {
		return domain.Analysis{Steps: sess.Steps(), Usage: &usage},
			fmt.Errorf("agent: the request was declined by a safety classifier (%s): %s",
				last.StopDetails.Category, last.StopDetails.Explanation)
	}

	log.Info("agent loop finished",
		"turns", turns, "tool_calls", len(sess.Steps()),
		"stop_reason", last.StopReason,
		"input_tokens", usage.InputTokens, "output_tokens", usage.OutputTokens,
		"cache_read_tokens", usage.CacheReadTokens)

	analysis := domain.Analysis{Steps: sess.Steps(), Usage: &usage}
	report, ok := sess.Findings()
	if !ok {
		// The loop ended without the terminal tool call - usually the
		// iteration cap. Hand back whatever the model did say rather than
		// discarding the run, and make the gap explicit.
		analysis.Summary = fmt.Sprintf(
			"The investigation ended after %d turns without structured findings (stop reason: %s). Model's last message: %s",
			turns, last.StopReason, finalText(last))
		log.Warn("agent produced no structured findings",
			"turns", turns, "stop_reason", last.StopReason, "max_iterations", c.cfg.MaxIterations)
		return analysis, nil
	}

	analysis.Summary = report.Summary
	analysis.Hypotheses = toHypotheses(report)
	return analysis, nil
}

// toHypotheses converts the reported findings into domain types, clamping the
// model's confidence into range.
func toHypotheses(report reportInput) []domain.Hypothesis {
	out := make([]domain.Hypothesis, 0, len(report.Hypotheses))
	for _, h := range report.Hypotheses {
		confidence := h.Confidence
		if confidence < 0 {
			confidence = 0
		}
		if confidence > 1 {
			confidence = 1
		}
		evidence := make([]domain.Evidence, 0, len(h.Evidence))
		for _, e := range h.Evidence {
			evidence = append(evidence, domain.Evidence{
				Kind:      evidenceKind(e.Kind),
				ServiceID: e.ServiceID,
				Summary:   e.Summary,
				Ref:       e.Ref,
			})
		}
		out = append(out, domain.Hypothesis{
			ID:               domain.NewID("hyp"),
			Title:            h.Title,
			RootCauseService: h.RootCauseService,
			Confidence:       confidence,
			Rationale:        h.Rationale,
			PropagationPath:  h.PropagationPath,
			Evidence:         evidence,
			SuggestedActions: h.SuggestedActions,
		})
	}
	return out
}

func evidenceKind(s string) domain.EvidenceKind {
	switch domain.EvidenceKind(strings.ToLower(s)) {
	case domain.EvidenceSignal:
		return domain.EvidenceSignal
	case domain.EvidenceChange:
		return domain.EvidenceChange
	case domain.EvidenceTopology:
		return domain.EvidenceTopology
	case domain.EvidenceTrace:
		return domain.EvidenceTrace
	default:
		return domain.EvidenceReasoning
	}
}

func finalText(msg *anthropic.BetaMessage) string {
	var parts []string
	for _, block := range msg.Content {
		if b, ok := block.AsAny().(anthropic.BetaTextBlock); ok && strings.TrimSpace(b.Text) != "" {
			parts = append(parts, strings.TrimSpace(b.Text))
		}
	}
	if len(parts) == 0 {
		return "(no text)"
	}
	return strings.Join(parts, "\n")
}

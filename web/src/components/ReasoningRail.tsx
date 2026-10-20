import type { Step, TokenUsage } from '../api/types'
import { duration, thousands } from '../lib/format'

/**
 * "How it got there": every tool call the investigation made, in order.
 *
 * This sits beside the verdict rather than under it on purpose - a conclusion
 * an operator cannot audit is not worth much at 3am.
 */
export function ReasoningRail({
  steps,
  usage,
  footer,
}: {
  steps: Step[]
  usage?: TokenUsage
  footer?: React.ReactNode
}) {
  return (
    <aside
      className="card card-pad col"
      style={{ gap: 16, flex: '1 1 340px', minWidth: 0, maxWidth: 420 }}
      aria-label="Investigation trail"
    >
      <div className="col" style={{ gap: 4 }}>
        <h3>How it got there</h3>
        <p className="small muted" style={{ margin: 0 }}>
          Every tool call, in order. Nothing in the verdict comes from anywhere else.
        </p>
      </div>

      {steps.length === 0 ? (
        <p className="small muted" style={{ margin: 0 }}>
          No steps recorded yet.
        </p>
      ) : (
        <ol className="col" style={{ gap: 0, margin: 0, padding: 0, listStyle: 'none' }}>
          {steps.map((step, i) => (
            <li key={step.seq} className="row" style={{ gap: 12, alignItems: 'stretch' }}>
              <div className="col" style={{ flex: '0 0 auto', width: 18, alignItems: 'center' }}>
                <span
                  className="dot"
                  style={{
                    width: 9,
                    height: 9,
                    marginTop: 5,
                    background: step.error ? 'var(--critical)' : toolColor(step.tool),
                  }}
                />
                {i < steps.length - 1 && <span className="trail-line" />}
              </div>

              <div
                className="col"
                style={{ flex: '1 1 auto', minWidth: 0, gap: 3, paddingBottom: i < steps.length - 1 ? 16 : 0 }}
              >
                <div className="row-wrap" style={{ gap: 8, justifyContent: 'space-between' }}>
                  <span
                    className="mono"
                    style={{ fontSize: 13, fontWeight: 600, color: 'var(--accent-ink)' }}
                  >
                    {step.tool}
                  </span>
                  <span className="tiny muted num">{duration(step.duration_ms)}</span>
                </div>

                {step.input && <div className="mono tiny dim">{shorten(step.input, 90)}</div>}

                {step.error ? (
                  <div className="small" style={{ color: 'var(--critical-ink)' }}>
                    {step.error}
                  </div>
                ) : (
                  step.output && <StepOutput output={step.output} />
                )}
              </div>
            </li>
          ))}
        </ol>
      )}

      {usage && (usage.input_tokens > 0 || usage.output_tokens > 0) && (
        <div className="tiny muted num" style={{ paddingTop: 14, borderTop: '1px solid var(--line)' }}>
          {thousands(usage.input_tokens)} input · {thousands(usage.output_tokens)} output
          {usage.cache_read_tokens ? ` · ${thousands(usage.cache_read_tokens)} cache read` : ''} tokens
        </div>
      )}

      {footer && <div style={{ paddingTop: 14, borderTop: '1px solid var(--line)' }}>{footer}</div>}
    </aside>
  )
}

/**
 * Tool results are raw JSON, which is the right thing to send the model and
 * the wrong thing to put in front of a human. Show the gist; keep the full
 * text one click away.
 */
function StepOutput({ output }: { output: string }) {
  const gist = firstLine(output)
  const hasMore = output.length > gist.length

  if (!hasMore) {
    return <div className="small">{gist}</div>
  }
  return (
    <details>
      <summary className="small" style={{ cursor: 'pointer' }}>
        {gist}
      </summary>
      <pre
        className="mono tiny"
        style={{
          margin: '8px 0 0',
          padding: 10,
          maxHeight: 220,
          overflow: 'auto',
          background: 'var(--raised)',
          border: '1px solid var(--line)',
          borderRadius: 6,
          whiteSpace: 'pre-wrap',
          wordBreak: 'break-word',
        }}
      >
        {output}
      </pre>
    </details>
  )
}

function firstLine(s: string): string {
  const line = s.split('\n', 1)[0] ?? s
  return line.length > 120 ? line.slice(0, 120) + '…' : line
}

function shorten(s: string, max: number): string {
  return s.length > max ? s.slice(0, max) + '…' : s
}

/** Colour the dot by what the step looked at, matching the evidence palette. */
function toolColor(tool: string): string {
  if (tool.includes('telemetry')) return 'var(--ev-signal)'
  if (tool.includes('report')) return 'var(--good)'
  if (tool.includes('rank') || tool.includes('hypoth')) return 'var(--ev-reasoning)'
  return 'var(--ev-topology)'
}

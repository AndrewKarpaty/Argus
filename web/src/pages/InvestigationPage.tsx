import { useIncident, useInvestigation, useStartInvestigation } from '../api/hooks'
import type { Hypothesis } from '../api/types'
import { HypothesisCard } from '../components/HypothesisCard'
import { ReasoningRail } from '../components/ReasoningRail'
import {
  ErrorBanner,
  Empty,
  InvestigationState,
  Loading,
  SeverityPill,
  StatTile,
} from '../components/primitives'
import { ArrowLeftIcon, RefreshIcon } from '../components/icons'
import { clockUTC, elapsed } from '../lib/format'
import { Link, useRouter } from '../lib/router'

export function InvestigationPage({ id }: { id: string }) {
  const { navigate } = useRouter()
  const query = useInvestigation(id)
  const iv = query.data?.investigation
  const incidentQuery = useIncident(iv?.incident_id ?? null)
  const incident = incidentQuery.data?.incident
  const restart = useStartInvestigation()

  if (query.isLoading) return <Loading lines={3} height={140} />
  if (query.isError) return <ErrorBanner error={query.error} />
  if (!iv) return <Empty title="Investigation not found" />

  const running = iv.status === 'queued' || iv.status === 'running'
  const hypotheses = [...(iv.hypotheses ?? [])].sort(byConfidence)
  const affected = incident?.affected_services ?? []
  const steps = iv.steps ?? []
  const window = incidentQuery.data?.window

  return (
    <>
      <Link to="/" className="small" style={{ alignSelf: 'flex-start', display: 'inline-flex', gap: 6 }}>
        <ArrowLeftIcon />
        All incidents
      </Link>

      <div className="col" style={{ gap: 12 }}>
        <span className="eyebrow">INVESTIGATION · {iv.incident_id}</span>
        <h1>{incident?.title ?? 'Investigation'}</h1>
        <div className="row-wrap" style={{ gap: 8 }}>
          {incident && <SeverityPill severity={incident.severity} />}
          <span className="pill">
            <InvestigationState status={iv.status} />
          </span>
          {window && (
            <span className="small muted num">
              window {clockUTC(window.from)} → {clockUTC(window.to)} UTC
            </span>
          )}
          <span className="mono tiny muted">{iv.engine}</span>
        </div>
      </div>

      {iv.status === 'failed' && (
        <ErrorBanner error={new Error(iv.error || 'The investigation failed')}>
          The incident is unchanged — you can start another run below.
        </ErrorBanner>
      )}

      <div
        className="grid-4"
        style={{ gap: 1, background: 'var(--line)', border: '1px solid var(--line)', borderRadius: 10, overflow: 'hidden' }}
      >
        <StatTile
          label="HYPOTHESES"
          value={hypotheses.length || (running ? '…' : 0)}
          foot={running ? 'still testing' : 'ranked by confidence'}
        />
        <StatTile
          label="TOOL CALLS"
          value={steps.length || (running ? '…' : 0)}
          foot={running ? 'in progress' : toolBreakdown(steps.map((s) => s.tool))}
        />
        <StatTile
          label={running ? 'ELAPSED' : 'TIME TO VERDICT'}
          value={elapsed(iv.started_at, iv.finished_at)}
          foot={iv.finished_at ? 'converged' : 'running'}
        />
        <StatTile
          label="AFFECTED"
          value={affected.length}
          foot={affected.join(', ') || '—'}
        />
      </div>

      {iv.summary && (
        <div className="callout col" style={{ gap: 8 }}>
          <span className="eyebrow">VERDICT</span>
          <p style={{ margin: 0, fontSize: 16, lineHeight: '25px' }}>{iv.summary}</p>
        </div>
      )}

      <div className="row-wrap" style={{ gap: 20, alignItems: 'flex-start' }}>
        <div className="col" style={{ flex: '999 1 520px', minWidth: 0, gap: 16 }}>
          <div className="row" style={{ gap: 10, alignItems: 'baseline' }}>
            <h2>Hypotheses</h2>
            <span className="small muted">
              {running ? 'forming as the evidence comes in' : 'ranked by confidence'}
            </span>
          </div>

          {hypotheses.length === 0 ? (
            <Empty title={running ? 'Still investigating' : 'No hypothesis was reported'}>
              {running
                ? 'Candidates appear here as soon as the run reports them. The trail on the right updates live.'
                : 'The run finished without structured findings. The trail on the right shows how far it got.'}
            </Empty>
          ) : (
            hypotheses.map((h, i) => (
              <HypothesisCard
                key={h.id}
                hypothesis={h}
                rank={i + 1}
                affected={affected}
                onFocusService={(serviceId) =>
                  navigate(
                    `/topology?focus=${encodeURIComponent(serviceId)}&cause=${encodeURIComponent(serviceId)}`,
                  )
                }
              />
            ))
          )}
        </div>

        <ReasoningRail
          steps={steps}
          usage={iv.usage}
          footer={
            <button
              type="button"
              className="btn"
              style={{ width: '100%' }}
              disabled={running || restart.isPending}
              onClick={() =>
                restart.mutate(iv.incident_id, {
                  onSuccess: (res) => navigate(`/investigations/${res.investigation.id}`),
                })
              }
            >
              <RefreshIcon />
              {restart.isPending ? 'Starting…' : 'Re-investigate'}
            </button>
          }
        />
      </div>
    </>
  )
}

function byConfidence(a: Hypothesis, b: Hypothesis): number {
  return b.confidence - a.confidence
}

/** toolBreakdown turns the trail into "graph 3 · telemetry 3 · report 1". */
function toolBreakdown(tools: string[]): string {
  let graph = 0
  let telemetry = 0
  let report = 0
  for (const t of tools) {
    if (t.includes('telemetry')) telemetry++
    else if (t.includes('report')) report++
    else graph++
  }
  const parts: string[] = []
  if (graph) parts.push(`graph ${graph}`)
  if (telemetry) parts.push(`telemetry ${telemetry}`)
  if (report) parts.push(`report ${report}`)
  return parts.join(' · ') || '—'
}

import { useMemo, useState } from 'react'
import { useGraph, useIncidents, useReady } from '../api/hooks'
import type { IncidentRow } from '../api/types'
import { NewIncidentForm } from '../components/NewIncidentForm'
import { ErrorBanner, Empty, InvestigationState, Loading, SeverityPill, StatTile } from '../components/primitives'
import { PlusIcon } from '../components/icons'
import { elapsed, relativeTime } from '../lib/format'
import { Link, useRouter } from '../lib/router'

export function IncidentsPage() {
  const { navigate } = useRouter()
  const incidents = useIncidents()
  const graph = useGraph()
  const ready = useReady()
  const [composing, setComposing] = useState(false)

  const rows = incidents.data?.incidents ?? []
  const counts = useMemo(() => {
    const dayAgo = Date.now() - 86_400_000
    return {
      open: rows.filter((i) => i.status === 'open').length,
      investigating: rows.filter((i) => i.status === 'investigating').length,
      settled: rows.filter(
        (i) => (i.status === 'resolved' || i.status === 'mitigated') && Date.parse(i.updated_at) >= dayAgo,
      ).length,
      criticalOpen: rows.filter((i) => i.severity === 'critical' && i.status !== 'resolved').length,
    }
  }, [rows])

  return (
    <>
      <div className="spread">
        <div className="col" style={{ gap: 6 }}>
          <span className="eyebrow">DEPENDENCY-AWARE INVESTIGATION</span>
          <h1>Incidents</h1>
          <p className="small muted" style={{ margin: 0 }}>
            Every incident is investigated against the service dependency graph, and every conclusion
            carries the evidence it rests on.
          </p>
        </div>
        {!composing && (
          <button type="button" className="btn btn-primary" onClick={() => setComposing(true)}>
            <PlusIcon />
            New incident
          </button>
        )}
      </div>

      <div className="grid-4">
        <StatTile
          label="OPEN"
          value={counts.open}
          foot={counts.criticalOpen > 0 ? `${counts.criticalOpen} critical` : 'none critical'}
        />
        <StatTile label="INVESTIGATING" value={counts.investigating} foot="running right now" />
        <StatTile label="SETTLED · 24 H" value={counts.settled} foot="resolved or mitigated" />
        <StatTile
          label="CATALOG"
          value={ready.data?.services ?? '—'}
          foot={ready.data ? `${ready.data.dependencies} dependencies` : 'loading'}
        />
      </div>

      {composing && (
        <NewIncidentForm
          services={graph.data?.services ?? []}
          onClose={() => setComposing(false)}
          onCreated={(incidentId, investigationId) => {
            setComposing(false)
            navigate(investigationId ? `/investigations/${investigationId}` : `/?incident=${incidentId}`)
          }}
        />
      )}

      {incidents.isError && <ErrorBanner error={incidents.error}>Is argusd running?</ErrorBanner>}

      {incidents.isLoading ? (
        <Loading lines={2} height={120} />
      ) : rows.length === 0 ? (
        <Empty title="No incidents yet">
          Open one above, or post an alert to <code>POST /api/v1/webhooks/alerts</code> and Argus will
          open and investigate it for you.
        </Empty>
      ) : (
        <div className="card" style={{ overflow: 'hidden' }}>
          <div
            className="spread"
            style={{ padding: '16px 20px', borderBottom: '1px solid var(--line)', alignItems: 'center' }}
          >
            <h2 style={{ fontSize: 16, lineHeight: '22px' }}>Open and recent</h2>
            <span className="tiny muted num">{rows.length} shown</span>
          </div>
          <div className="table-scroll">
            <table>
              <thead>
                <tr>
                  <th scope="col">SEVERITY</th>
                  <th scope="col">INCIDENT</th>
                  <th scope="col">INVESTIGATION</th>
                  <th scope="col">DETECTED</th>
                  <th scope="col">LEADING HYPOTHESIS</th>
                </tr>
              </thead>
              <tbody>
                {rows.map((row) => (
                  <IncidentTableRow key={row.id} row={row} />
                ))}
              </tbody>
            </table>
          </div>
        </div>
      )}
    </>
  )
}

function IncidentTableRow({ row }: { row: IncidentRow }) {
  const iv = row.latest_investigation
  const top = leadingHypothesis(row)

  return (
    <tr>
      <td style={{ whiteSpace: 'nowrap' }}>
        <SeverityPill severity={row.severity} />
      </td>

      <td>
        {iv ? (
          <Link to={`/investigations/${iv.id}`} style={{ fontWeight: 600, color: 'var(--ink)' }}>
            {row.title}
          </Link>
        ) : (
          <span style={{ fontWeight: 600 }}>{row.title}</span>
        )}
        <div className="row-wrap" style={{ gap: 6, marginTop: 6 }}>
          {row.affected_services.map((id) => (
            <span key={id} className="chip">
              {id}
            </span>
          ))}
        </div>
      </td>

      <td style={{ whiteSpace: 'nowrap' }}>
        {iv ? (
          <>
            <InvestigationState status={iv.status} />
            <div className="tiny muted num" style={{ marginTop: 4 }}>
              {iv.status === 'completed'
                ? `${iv.hypotheses?.length ?? 0} hypotheses · ${elapsed(iv.started_at, iv.finished_at)}`
                : iv.status === 'failed'
                  ? (iv.error ?? 'failed')
                  : `${iv.steps?.length ?? 0} steps so far`}
            </div>
          </>
        ) : (
          <span className="small muted">not started</span>
        )}
      </td>

      <td className="num muted" style={{ whiteSpace: 'nowrap' }}>
        {relativeTime(row.detected_at)}
      </td>

      <td>
        {top ? (
          <div className="col" style={{ gap: 6 }}>
            <span className="mono" style={{ fontSize: 13, fontWeight: 600 }}>
              {top.root_cause_service}
            </span>
            <div className="row" style={{ gap: 8 }}>
              <div className="meter" style={{ width: 120, height: 6 }}>
                <span style={{ width: `${Math.round(top.confidence * 100)}%` }} />
              </div>
              <span className="small num" style={{ fontWeight: 600 }}>
                {top.confidence.toFixed(2)}
              </span>
            </div>
          </div>
        ) : iv && (iv.status === 'running' || iv.status === 'queued') ? (
          <span className="small muted">gathering evidence…</span>
        ) : (
          <span className="small muted">—</span>
        )}
      </td>
    </tr>
  )
}

function leadingHypothesis(row: IncidentRow) {
  const hs = row.latest_investigation?.hypotheses ?? []
  if (hs.length === 0) return null
  return hs.reduce((best, h) => (h.confidence > best.confidence ? h : best), hs[0])
}

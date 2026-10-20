import { useEffect, useMemo, useState } from 'react'
import { useBlastRadius, useGraph, useIncidents, useSuspects } from '../api/hooks'
import { DependencyGraph, GraphLegend } from '../components/DependencyGraph'
import { SuspectsTable } from '../components/SuspectsTable'
import { ErrorBanner, Loading, Tag } from '../components/primitives'
import { layoutGraph, type GraphView } from '../lib/graphLayout'
import { useRouter } from '../lib/router'

const VIEWS: { id: GraphView; label: string; hint: string }[] = [
  { id: 'impact', label: 'Impact', hint: 'everything that depends on this service' },
  { id: 'dependencies', label: 'Dependencies', hint: 'everything this service depends on' },
  { id: 'all', label: 'Whole catalog', hint: 'every service in the catalog' },
]

export function TopologyPage() {
  const { query, navigate } = useRouter()
  const graph = useGraph()
  const incidents = useIncidents()

  const services = graph.data?.services ?? []
  const dependencies = graph.data?.dependencies ?? []

  // The focus and the named cause travel in the URL, so a link from an
  // investigation lands on exactly the picture that investigation describes.
  const urlFocus = query.get('focus')
  const cause = query.get('cause')
  const urlView = query.get('view') as GraphView | null

  const [view, setView] = useState<GraphView>(urlView ?? 'impact')
  const [focus, setFocus] = useState<string | null>(urlFocus)
  const [typed, setTyped] = useState(urlFocus ?? '')

  // Default the focus to whatever the newest open incident blames, so the page
  // opens on something useful rather than on the whole catalog.
  const openIncident = useMemo(
    () => (incidents.data?.incidents ?? []).find((i) => i.status === 'open' || i.status === 'investigating'),
    [incidents.data],
  )
  const suggestedFocus = cause ?? leadingCause(openIncident) ?? null

  useEffect(() => {
    if (!focus && suggestedFocus) {
      setFocus(suggestedFocus)
      setTyped(suggestedFocus)
    }
  }, [focus, suggestedFocus])

  const alerting = useMemo(() => {
    const ids = new Set<string>()
    for (const inc of incidents.data?.incidents ?? []) {
      if (inc.status === 'open' || inc.status === 'investigating') {
        for (const id of inc.affected_services) ids.add(id)
      }
    }
    return [...ids]
  }, [incidents.data])

  const layout = useMemo(
    () => layoutGraph({ services, dependencies, focus, view, alerting, cause }),
    [services, dependencies, focus, view, alerting, cause],
  )

  const blast = useBlastRadius(focus)
  const selected = services.find((s) => s.id === focus)

  // Correlation defaults to the services that are actually alerting.
  const [picked, setPicked] = useState<string[]>([])
  const correlateOn = picked.length > 0 ? picked : alerting
  const suspects = useSuspects(correlateOn)

  const setFocusAndUrl = (id: string) => {
    setFocus(id)
    setTyped(id)
    const next = new URLSearchParams()
    next.set('focus', id)
    if (cause) next.set('cause', cause)
    if (view !== 'impact') next.set('view', view)
    navigate(`/topology?${next.toString()}`, { replace: true })
  }

  if (graph.isLoading) return <Loading lines={2} height={200} />
  if (graph.isError) return <ErrorBanner error={graph.error}>Is argusd running?</ErrorBanner>

  const hasSoft = dependencies.some((d) => d.criticality === 'soft')

  return (
    <>
      <div className="spread">
        <div className="col" style={{ gap: 6 }}>
          <span className="eyebrow">SERVICE DEPENDENCY GRAPH</span>
          <h1>Topology</h1>
        </div>
        <form
          className="field"
          onSubmit={(e) => {
            e.preventDefault()
            if (services.some((s) => s.id === typed)) setFocusAndUrl(typed)
          }}
        >
          <label htmlFor="focus" className="label">
            FOCUS ON SERVICE
          </label>
          <input
            id="focus"
            list="argus-services"
            value={typed}
            onChange={(e) => {
              setTyped(e.target.value)
              if (services.some((s) => s.id === e.target.value)) setFocusAndUrl(e.target.value)
            }}
            placeholder="postgres-main"
            className="mono"
            style={{ width: 260 }}
          />
          <datalist id="argus-services">
            {services.map((s) => (
              <option key={s.id} value={s.id} />
            ))}
          </datalist>
        </form>
      </div>

      <div className="row-wrap" style={{ gap: 20, alignItems: 'flex-start' }}>
        <div className="card card-pad col" style={{ flex: '999 1 600px', minWidth: 0, gap: 16 }}>
          <div className="spread" style={{ alignItems: 'center' }}>
            <div className="col" style={{ gap: 3 }}>
              <h3>{titleFor(view, focus)}</h3>
              <p className="tiny muted" style={{ margin: 0 }}>
                Callers on the left, data stores on the right. Faults travel <b>against</b> the arrows.
              </p>
            </div>
            <div className="seg" role="group" aria-label="Graph view">
              {VIEWS.map((v) => (
                <button
                  key={v.id}
                  type="button"
                  aria-pressed={view === v.id}
                  title={v.hint}
                  onClick={() => setView(v.id)}
                >
                  {v.label}
                </button>
              ))}
            </div>
          </div>

          <DependencyGraph layout={layout} focus={focus} onSelect={setFocusAndUrl} />

          <GraphLegend hasFault={!!cause} hasSoft={hasSoft && view === 'all'} />

          {layout.hidden.length > 0 && (
            <div className="row-wrap tiny dim" style={{ gap: 8 }}>
              <span>
                {layout.hidden.length} of {services.length} services have no dependency path to{' '}
                <span className="mono">{focus}</span> and are hidden:{' '}
                <span className="mono">{layout.hidden.slice(0, 6).join(', ')}</span>
                {layout.hidden.length > 6 ? ', …' : ''}
              </span>
              <button type="button" className="btn btn-sm" onClick={() => setView('all')}>
                show whole catalog
              </button>
            </div>
          )}
        </div>

        <div className="card card-pad col" style={{ flex: '1 1 320px', minWidth: 0, maxWidth: 376, gap: 16 }}>
          {!selected ? (
            <p className="small muted" style={{ margin: 0 }}>
              Pick a service in the graph to inspect it.
            </p>
          ) : (
            <>
              <div className="col" style={{ gap: 8 }}>
                <div className="mono" style={{ fontSize: 17, lineHeight: '22px', fontWeight: 600 }}>
                  {selected.id}
                </div>
                <div className="row-wrap" style={{ gap: 6 }}>
                  <Tag>{selected.kind.toUpperCase()}</Tag>
                  <Tag>TIER {selected.tier.toUpperCase()}</Tag>
                  {selected.team && <Tag>TEAM {selected.team.toUpperCase()}</Tag>}
                </div>
                {selected.description && <p className="small muted" style={{ margin: 0 }}>{selected.description}</p>}
                {selected.runbook && (
                  <a className="small" href={selected.runbook} target="_blank" rel="noreferrer">
                    Runbook ↗
                  </a>
                )}
              </div>

              <div
                className="row"
                style={{ gap: 1, background: 'var(--line)', border: '1px solid var(--line)', borderRadius: 8, overflow: 'hidden' }}
              >
                <div style={{ flex: '1 1 0', padding: '11px 13px', background: 'var(--raised)' }}>
                  <div className="label">DEPENDS ON</div>
                  <div className="num" style={{ fontSize: 22, lineHeight: '28px', fontWeight: 600 }}>
                    {dependencies.filter((d) => d.from === selected.id).length}
                  </div>
                </div>
                <div style={{ flex: '1 1 0', padding: '11px 13px', background: 'var(--raised)' }}>
                  <div className="label">AT RISK</div>
                  <div
                    className="num"
                    style={{ fontSize: 22, lineHeight: '28px', fontWeight: 600, color: 'var(--serious)' }}
                  >
                    {blast.data?.affected_count ?? '—'}
                  </div>
                  <div className="tiny dim">{blast.data ? `${blast.data.critical_affected} critical tier` : ''}</div>
                </div>
              </div>

              <div className="col" style={{ gap: 10 }}>
                <div className="label">BLAST RADIUS</div>
                {blast.isLoading ? (
                  <span className="small muted">Loading…</span>
                ) : (blast.data?.affected ?? []).length === 0 ? (
                  <span className="small muted">Nothing depends on it — it is an entry point.</span>
                ) : (
                  <div
                    className="col"
                    style={{ gap: 1, background: 'var(--line)', border: '1px solid var(--line)', borderRadius: 8, overflow: 'hidden' }}
                  >
                    {(blast.data?.affected ?? []).map((hop) => (
                      <div
                        key={hop.service_id}
                        className="row"
                        style={{ justifyContent: 'space-between', gap: 10, padding: '8px 12px', background: 'var(--card)' }}
                      >
                        <button
                          type="button"
                          className="mono"
                          onClick={() => setFocusAndUrl(hop.service_id)}
                          style={{
                            border: 'none',
                            background: 'none',
                            padding: 0,
                            fontSize: 13,
                            cursor: 'pointer',
                            color: alerting.includes(hop.service_id) ? 'var(--critical-ink)' : 'var(--ink)',
                          }}
                        >
                          {hop.service_id}
                        </button>
                        <span className="tiny dim" style={{ whiteSpace: 'nowrap' }}>
                          {hop.depth} {hop.depth === 1 ? 'hop' : 'hops'}
                          {hop.hard_path ? ' · hard' : ' · soft'}
                          {alerting.includes(hop.service_id) ? ' · alerting' : ''}
                        </span>
                      </div>
                    ))}
                  </div>
                )}
              </div>
            </>
          )}
        </div>
      </div>

      <div className="card card-pad col" style={{ gap: 16 }}>
        <div className="spread" style={{ alignItems: 'center' }}>
          <div className="col" style={{ gap: 3 }}>
            <h3>Correlate symptoms</h3>
            <p className="tiny muted" style={{ margin: 0 }}>
              Name the services that are misbehaving; Argus ranks what they share upstream.
            </p>
          </div>
          <div className="row-wrap" style={{ gap: 6 }}>
            {correlateOn.map((id) => (
              <button
                key={id}
                type="button"
                className="chip chip-alert"
                style={{ cursor: 'pointer' }}
                title="Remove from the correlation"
                onClick={() => setPicked(correlateOn.filter((x) => x !== id))}
              >
                {id} ✕
              </button>
            ))}
            <select
              aria-label="Add a service to the correlation"
              className="btn btn-sm"
              value=""
              onChange={(e) => {
                if (e.target.value) setPicked([...correlateOn, e.target.value])
              }}
            >
              <option value="">+ add service</option>
              {services
                .filter((s) => !correlateOn.includes(s.id))
                .map((s) => (
                  <option key={s.id} value={s.id}>
                    {s.id}
                  </option>
                ))}
            </select>
          </div>
        </div>

        {suspects.isError ? (
          <ErrorBanner error={suspects.error} />
        ) : (
          <SuspectsTable
            suspects={suspects.data?.suspects ?? []}
            affectedCount={correlateOn.length}
            onSelect={setFocusAndUrl}
          />
        )}
      </div>
    </>
  )
}

function titleFor(view: GraphView, focus: string | null): string {
  if (view === 'all') return 'The whole catalog'
  if (!focus) return 'Dependency graph'
  if (view === 'impact') return `What breaks when ${focus} fails`
  return `What ${focus} depends on`
}

function leadingCause(incident: { latest_investigation?: { hypotheses?: { root_cause_service: string; confidence: number }[] } } | undefined) {
  const hs = incident?.latest_investigation?.hypotheses ?? []
  if (hs.length === 0) return null
  return hs.reduce((best, h) => (h.confidence > best.confidence ? h : best), hs[0]).root_cause_service
}

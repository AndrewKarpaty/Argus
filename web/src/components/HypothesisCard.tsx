import type { Hypothesis } from '../api/types'
import { clockUTC } from '../lib/format'
import { CheckIcon } from './icons'
import { ConfidenceMeter, EvidenceDot, ServiceChip, Tag } from './primitives'

export function HypothesisCard({
  hypothesis,
  rank,
  affected,
  onFocusService,
}: {
  hypothesis: Hypothesis
  rank: number
  affected: string[]
  onFocusService?: (serviceId: string) => void
}) {
  const leading = rank === 1
  const evidence = hypothesis.evidence ?? []
  const actions = hypothesis.suggested_actions ?? []
  const path = hypothesis.propagation_path ?? []

  return (
    <section
      className="card"
      style={{
        padding: 20,
        display: 'flex',
        flexDirection: 'column',
        gap: leading ? 18 : 12,
        borderColor: leading ? 'var(--accent-strong)' : 'var(--line)',
      }}
      aria-label={`Hypothesis ${rank}: ${hypothesis.title}`}
    >
      <div className="spread" style={{ alignItems: 'flex-start' }}>
        <div className="row" style={{ gap: 12, alignItems: 'flex-start', minWidth: 0 }}>
          <span
            style={{
              flex: '0 0 auto',
              width: 26,
              height: 26,
              borderRadius: 999,
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              fontSize: 14,
              fontWeight: 700,
              background: leading ? 'var(--accent)' : 'var(--raised)',
              color: leading ? 'var(--accent-on)' : 'var(--ink-2)',
              border: leading ? 'none' : '1px solid var(--line-strong)',
            }}
          >
            {rank}
          </span>
          <div className="col" style={{ gap: 10, minWidth: 0 }}>
            <h3 style={{ fontSize: leading ? 19 : 16, lineHeight: leading ? '26px' : '23px' }}>
              {hypothesis.title}
            </h3>
            <div className="row-wrap" style={{ gap: 8 }}>
              {onFocusService ? (
                <button
                  type="button"
                  onClick={() => onFocusService(hypothesis.root_cause_service)}
                  className="chip chip-cause"
                  style={{ cursor: 'pointer' }}
                  title="Show this service in the topology"
                >
                  {hypothesis.root_cause_service}
                </button>
              ) : (
                <ServiceChip id={hypothesis.root_cause_service} tone="cause" />
              )}
              {hypothesis.refuted_by && <Tag>REFUTED</Tag>}
              <span className="small muted">
                explains {countExplained(hypothesis, affected)} of {affected.length}
              </span>
            </div>
          </div>
        </div>
        <ConfidenceMeter value={hypothesis.confidence} showLabel={leading} />
      </div>

      <p style={{ margin: 0, fontSize: 14, lineHeight: '22px' }}>{hypothesis.rationale}</p>

      {hypothesis.refuted_by && (
        <p className="small" style={{ margin: 0, color: 'var(--serious-ink)' }}>
          Refuted: {hypothesis.refuted_by}
        </p>
      )}

      {path.length > 1 && (
        <div className="col" style={{ gap: 8 }}>
          <div className="label">PROPAGATION</div>
          <div className="row-wrap" style={{ gap: 8 }}>
            {path.map((id, i) => (
              <span key={`${id}-${i}`} className="row" style={{ gap: 8 }}>
                {i > 0 && (
                  <svg width="20" height="12" viewBox="0 0 20 12" fill="none" aria-hidden="true">
                    <path d="M1 6h16" stroke="var(--edge)" strokeWidth="1.7" strokeLinecap="round" />
                    <path
                      d="M13.4 2.6 17 6l-3.6 3.4"
                      stroke="var(--edge)"
                      strokeWidth="1.7"
                      strokeLinecap="round"
                      strokeLinejoin="round"
                    />
                  </svg>
                )}
                <ServiceChip
                  id={id}
                  tone={
                    id === hypothesis.root_cause_service
                      ? 'cause'
                      : affected.includes(id)
                        ? 'alert'
                        : 'plain'
                  }
                />
              </span>
            ))}
          </div>
        </div>
      )}

      {evidence.length > 0 && (
        <div className="col" style={{ gap: 10 }}>
          <div className="label">EVIDENCE · {evidence.length}</div>
          {evidence.map((e, i) => (
            <div
              key={i}
              className="row"
              style={{
                gap: 10,
                alignItems: 'flex-start',
                paddingBottom: i < evidence.length - 1 ? 10 : 0,
                borderBottom: i < evidence.length - 1 ? '1px solid var(--line)' : 'none',
              }}
            >
              <EvidenceDot kind={e.kind} />
              <div style={{ minWidth: 0 }}>
                <span className="label" style={{ letterSpacing: '0.12em' }}>
                  {e.kind.toUpperCase()}
                  {e.at ? ` · ${clockUTC(e.at)} UTC` : ''}{' '}
                </span>
                <span style={{ fontSize: 14, lineHeight: '21px' }}>{e.summary}</span>
                {e.ref && <span className="mono tiny muted"> ({e.ref})</span>}
              </div>
            </div>
          ))}
        </div>
      )}

      {actions.length > 0 && (
        <div className="col" style={{ gap: 10 }}>
          <div className="label">SUGGESTED ACTIONS</div>
          {actions.map((a, i) => (
            <div key={i} className="row" style={{ gap: 10, alignItems: 'flex-start' }}>
              <span style={{ color: 'var(--good)', display: 'flex', marginTop: 3 }}>
                <CheckIcon size={16} />
              </span>
              <span style={{ fontSize: 14, lineHeight: '21px' }}>{a}</span>
            </div>
          ))}
        </div>
      )}
    </section>
  )
}

/** countExplained reads the coverage back off the evidence and the path. */
function countExplained(h: Hypothesis, affected: string[]): number {
  const mentioned = new Set<string>(h.propagation_path ?? [])
  for (const e of h.evidence ?? []) {
    if (e.service_id) mentioned.add(e.service_id)
    for (const id of affected) {
      if (e.summary.includes(id)) mentioned.add(id)
    }
  }
  const hits = affected.filter((id) => mentioned.has(id)).length
  return hits > 0 ? hits : 1
}

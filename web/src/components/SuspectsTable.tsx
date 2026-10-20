import type { Suspect } from '../api/types'

/**
 * The shared-upstream ranking. It is explicitly a plausibility ranking, not a
 * verdict - the wording in the footnote matters, because a ranked table reads
 * as an answer unless it says otherwise.
 */
export function SuspectsTable({
  suspects,
  affectedCount,
  onSelect,
}: {
  suspects: Suspect[]
  affectedCount: number
  onSelect?: (serviceId: string) => void
}) {
  if (suspects.length === 0) {
    return (
      <p className="small muted" style={{ margin: 0 }}>
        Pick at least one service that is misbehaving.
      </p>
    )
  }

  return (
    <>
      <div className="table-scroll">
        <table>
          <thead>
            <tr>
              <th scope="col">CANDIDATE</th>
              <th scope="col">COVERAGE</th>
              <th scope="col" style={{ textAlign: 'right' }}>
                SCORE
              </th>
              <th scope="col">WHY IT RANKS THERE</th>
            </tr>
          </thead>
          <tbody>
            {suspects.map((s, i) => {
              const leading = i === 0
              const explained = s.explains?.length ?? 0
              return (
                <tr key={s.service_id} style={leading ? { background: 'var(--accent-wash-soft)' } : undefined}>
                  <td style={{ whiteSpace: 'nowrap' }}>
                    {onSelect ? (
                      <button
                        type="button"
                        onClick={() => onSelect(s.service_id)}
                        className="mono"
                        style={{
                          border: 'none',
                          background: 'none',
                          padding: 0,
                          fontSize: 13.5,
                          fontWeight: leading ? 600 : 400,
                          color: leading ? 'var(--accent-ink)' : 'var(--ink)',
                          cursor: 'pointer',
                          textAlign: 'left',
                        }}
                      >
                        {s.service_id}
                      </button>
                    ) : (
                      <span
                        className="mono"
                        style={{ fontSize: 13.5, fontWeight: leading ? 600 : 400 }}
                      >
                        {s.service_id}
                      </span>
                    )}
                  </td>
                  <td>
                    <div className="row" style={{ gap: 9 }}>
                      <div
                        className={leading || s.hard_path ? 'meter' : 'meter meter-dim'}
                        style={{ width: 96, height: 7 }}
                      >
                        <span style={{ width: `${Math.round(s.coverage * 100)}%` }} />
                      </div>
                      <span className="small num" style={{ whiteSpace: 'nowrap' }}>
                        {explained} of {affectedCount}
                      </span>
                    </div>
                  </td>
                  <td className="num" style={{ textAlign: 'right', fontWeight: leading ? 600 : 400 }}>
                    {s.score.toFixed(3)}
                  </td>
                  <td className="small muted">
                    {s.reason}
                    {!s.hard_path && ' · soft path only'}
                  </td>
                </tr>
              )
            })}
          </tbody>
        </table>
      </div>
      <p className="tiny dim" style={{ margin: 0 }}>
        Coverage is how many of the selected services depend on the candidate. Score also weighs hop
        distance, dependency criticality, tier and node kind. This is a plausibility ranking, not a
        verdict — an investigation checks each candidate against telemetry before concluding.
      </p>
    </>
  )
}

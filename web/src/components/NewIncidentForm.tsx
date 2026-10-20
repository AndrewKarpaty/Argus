import { useState } from 'react'
import { useCreateIncident } from '../api/hooks'
import type { Service, Severity } from '../api/types'
import { ErrorBanner } from './primitives'

const SEVERITIES: Severity[] = ['critical', 'high', 'medium', 'low']

export function NewIncidentForm({
  services,
  onClose,
  onCreated,
}: {
  services: Service[]
  onClose: () => void
  onCreated: (incidentId: string, investigationId?: string) => void
}) {
  const [title, setTitle] = useState('')
  const [description, setDescription] = useState('')
  const [severity, setSeverity] = useState<Severity>('high')
  const [picked, setPicked] = useState<string[]>([])
  const [investigate, setInvestigate] = useState(true)
  const create = useCreateIncident()

  const toggle = (id: string) =>
    setPicked((cur) => (cur.includes(id) ? cur.filter((x) => x !== id) : [...cur, id]))

  const canSubmit = title.trim().length > 0 && picked.length > 0 && !create.isPending

  return (
    <form
      className="card card-pad col"
      style={{ gap: 16 }}
      onSubmit={(e) => {
        e.preventDefault()
        if (!canSubmit) return
        create.mutate(
          {
            title: title.trim(),
            description: description.trim() || undefined,
            severity,
            affected_services: picked,
            investigate,
          },
          {
            onSuccess: (res) => onCreated(res.incident.id, res.investigation?.id),
          },
        )
      }}
    >
      <div className="spread" style={{ alignItems: 'center' }}>
        <h2>New incident</h2>
        <button type="button" className="btn btn-sm" onClick={onClose}>
          Cancel
        </button>
      </div>

      {create.isError && <ErrorBanner error={create.error} />}

      <div className="field">
        <label htmlFor="inc-title" className="label">
          WHAT IS HAPPENING
        </label>
        <input
          id="inc-title"
          value={title}
          onChange={(e) => setTitle(e.target.value)}
          placeholder="Checkout and orders returning 5xx"
          required
        />
      </div>

      <div className="field">
        <label htmlFor="inc-desc" className="label">
          DETAIL <span className="dim">(optional)</span>
        </label>
        <textarea
          id="inc-desc"
          value={description}
          onChange={(e) => setDescription(e.target.value)}
          placeholder="Anything the on-call already knows: when it started, what changed, what was tried."
        />
      </div>

      <div className="field" style={{ maxWidth: 220 }}>
        <label htmlFor="inc-sev" className="label">
          SEVERITY
        </label>
        <select id="inc-sev" value={severity} onChange={(e) => setSeverity(e.target.value as Severity)}>
          {SEVERITIES.map((s) => (
            <option key={s} value={s}>
              {s}
            </option>
          ))}
        </select>
      </div>

      <fieldset style={{ border: 'none', margin: 0, padding: 0 }}>
        <legend className="label" style={{ padding: 0, marginBottom: 8 }}>
          AFFECTED SERVICES · {picked.length} SELECTED
        </legend>
        <p className="tiny muted" style={{ margin: '0 0 10px' }}>
          Pick every service showing symptoms. The more of them, the sharper the correlation: the
          investigation ranks what they share upstream.
        </p>
        <div
          style={{
            display: 'grid',
            gridTemplateColumns: 'repeat(auto-fill, minmax(180px, 1fr))',
            gap: 8,
            maxHeight: 220,
            overflowY: 'auto',
            padding: 10,
            border: '1px solid var(--line)',
            borderRadius: 'var(--radius-control)',
            background: 'var(--raised)',
          }}
        >
          {services.map((s) => (
            <label key={s.id} className="row small" style={{ gap: 8, cursor: 'pointer' }}>
              <input
                type="checkbox"
                checked={picked.includes(s.id)}
                onChange={() => toggle(s.id)}
                style={{ width: 16, height: 16, accentColor: 'var(--accent)' }}
              />
              <span className="mono" style={{ fontSize: 12.5 }}>
                {s.id}
              </span>
              {s.tier === 'critical' && <span className="dim tiny">critical</span>}
            </label>
          ))}
        </div>
      </fieldset>

      <label className="row small" style={{ gap: 8, cursor: 'pointer' }}>
        <input
          type="checkbox"
          checked={investigate}
          onChange={(e) => setInvestigate(e.target.checked)}
          style={{ width: 16, height: 16, accentColor: 'var(--accent)' }}
        />
        Start investigating immediately
      </label>

      <div className="row" style={{ gap: 10 }}>
        <button type="submit" className="btn btn-primary" disabled={!canSubmit}>
          {create.isPending ? 'Opening…' : 'Open incident'}
        </button>
        {picked.length === 0 && (
          <span className="tiny muted">Select at least one affected service.</span>
        )}
      </div>
    </form>
  )
}

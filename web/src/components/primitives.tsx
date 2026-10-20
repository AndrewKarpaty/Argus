import type { ReactNode } from 'react'
import type { EvidenceKind, IncidentStatus, InvestigationStatus, Severity } from '../api/types'
import { confidenceLabel } from '../lib/format'
import { CheckIcon, CrossIcon, DashIcon, SpinnerIcon } from './icons'

/* ---- status and severity ---- */

const SEVERITY: Record<Severity, { dot: string; fg: string; bg: string; border: string }> = {
  critical: { dot: 'var(--critical)', fg: 'var(--critical-ink)', bg: 'var(--critical-wash)', border: 'var(--critical)' },
  high: { dot: 'var(--serious)', fg: 'var(--serious-ink)', bg: 'var(--serious-wash)', border: 'var(--serious)' },
  medium: { dot: 'var(--warning)', fg: 'var(--warning-ink)', bg: 'var(--warning-wash)', border: 'var(--warning)' },
  low: { dot: 'var(--ink-3)', fg: 'var(--ink-2)', bg: 'var(--raised)', border: 'var(--line-strong)' },
}

export function SeverityPill({ severity }: { severity: Severity }) {
  const c = SEVERITY[severity] ?? SEVERITY.low
  return (
    <span className="pill" style={{ borderColor: c.border, background: c.bg, color: c.fg }}>
      <span className="dot" style={{ background: c.dot }} />
      {severity}
    </span>
  )
}

/** InvestigationState shows an icon plus the word - never colour alone. */
export function InvestigationState({ status }: { status: InvestigationStatus }) {
  switch (status) {
    case 'completed':
      return (
        <span className="row small" style={{ gap: 6 }}>
          <span style={{ color: 'var(--good)', display: 'flex' }}>
            <CheckIcon size={14} />
          </span>
          converged
        </span>
      )
    case 'failed':
      return (
        <span className="row small" style={{ gap: 6, color: 'var(--critical-ink)' }}>
          <CrossIcon size={14} />
          failed
        </span>
      )
    case 'running':
    case 'queued':
      return (
        <span className="row small" style={{ gap: 7, color: 'var(--accent-ink)', fontWeight: 600 }}>
          <SpinnerIcon size={14} className="spin" />
          {status === 'queued' ? 'queued' : 'investigating'}
        </span>
      )
  }
}

export function IncidentState({ status }: { status: IncidentStatus }) {
  if (status === 'resolved' || status === 'mitigated') {
    return (
      <span className="row small muted" style={{ gap: 6 }}>
        <DashIcon size={14} />
        {status}
      </span>
    )
  }
  return (
    <span className="row small" style={{ gap: 6 }}>
      <span className="dot" style={{ background: status === 'open' ? 'var(--warning)' : 'var(--accent)' }} />
      {status}
    </span>
  )
}

/* ---- service identity ---- */

export function ServiceChip({
  id,
  tone = 'plain',
}: {
  id: string
  tone?: 'plain' | 'alert' | 'cause'
}) {
  const cls = tone === 'alert' ? 'chip chip-alert' : tone === 'cause' ? 'chip chip-cause' : 'chip'
  return <span className={cls}>{id}</span>
}

export function Tag({ children }: { children: ReactNode }) {
  return <span className="tag">{children}</span>
}

/* ---- meters and tiles ---- */

export function ConfidenceMeter({
  value,
  width = 150,
  showLabel = true,
}: {
  value: number
  width?: number
  showLabel?: boolean
}) {
  const pct = Math.max(0, Math.min(1, value)) * 100
  return (
    <div className="col" style={{ gap: 5, alignItems: 'flex-end' }}>
      <div className="row" style={{ gap: 10 }}>
        <div className="meter" style={{ width }} role="img" aria-label={`confidence ${value.toFixed(2)}`}>
          <span style={{ width: `${pct}%` }} />
        </div>
        <span className="num" style={{ fontSize: 21, lineHeight: '25px', fontWeight: 600 }}>
          {value.toFixed(2)}
        </span>
      </div>
      {showLabel && <span className="tiny muted">{confidenceLabel(value)}</span>}
    </div>
  )
}

export function StatTile({
  label,
  value,
  foot,
  tone,
}: {
  label: string
  value: ReactNode
  foot?: ReactNode
  tone?: string
}) {
  return (
    <div className="card" style={{ padding: '16px 18px', display: 'flex', flexDirection: 'column', gap: 6 }}>
      <div className="label">{label}</div>
      <div className="stat-value" style={tone ? { color: tone } : undefined}>
        {value}
      </div>
      {foot !== undefined && <div className="small muted">{foot}</div>}
    </div>
  )
}

/* ---- evidence ---- */

const EVIDENCE_COLOR: Record<EvidenceKind, string> = {
  topology: 'var(--ev-topology)',
  change: 'var(--ev-change)',
  signal: 'var(--ev-signal)',
  trace: 'var(--ev-trace)',
  reasoning: 'var(--ev-reasoning)',
}

export function EvidenceDot({ kind }: { kind: EvidenceKind }) {
  return (
    <span
      className="dot"
      style={{
        width: 9,
        height: 9,
        marginTop: 6,
        background: EVIDENCE_COLOR[kind] ?? 'var(--ink-3)',
      }}
    />
  )
}

/* ---- loading, error, empty ---- */

export function Loading({ lines = 3, height = 76 }: { lines?: number; height?: number }) {
  return (
    <div className="col" style={{ gap: 12 }} aria-busy="true" aria-live="polite">
      <span className="dim small">Loading…</span>
      {Array.from({ length: lines }, (_, i) => (
        <div key={i} className="skeleton" style={{ height }} />
      ))}
    </div>
  )
}

export function ErrorBanner({ error, children }: { error: unknown; children?: ReactNode }) {
  const message = error instanceof Error ? error.message : String(error)
  return (
    <div className="banner-error" role="alert">
      <div style={{ fontWeight: 600 }}>{message}</div>
      {children && <div className="small" style={{ marginTop: 6 }}>{children}</div>}
    </div>
  )
}

export function Empty({ title, children }: { title: string; children?: ReactNode }) {
  return (
    <div className="card card-pad col" style={{ gap: 6, alignItems: 'flex-start' }}>
      <div style={{ fontWeight: 600 }}>{title}</div>
      {children && <div className="small muted">{children}</div>}
    </div>
  )
}

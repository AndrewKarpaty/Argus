/** Formatting helpers shared by the pages. */

const UNITS: [limit: number, seconds: number, name: string][] = [
  [60, 1, 'second'],
  [3600, 60, 'minute'],
  [86400, 3600, 'hour'],
  [2592000, 86400, 'day'],
]

/** relativeTime renders "40 s ago" / "in 2 min" style text. */
export function relativeTime(iso: string | undefined, now = Date.now()): string {
  if (!iso) return '—'
  const then = Date.parse(iso)
  if (Number.isNaN(then)) return '—'
  const deltaSec = Math.round((now - then) / 1000)
  const abs = Math.abs(deltaSec)
  if (abs < 5) return 'just now'

  for (const [limit, div, name] of UNITS) {
    if (abs < limit) {
      const n = Math.round(abs / div)
      const unit = n === 1 ? name : name + 's'
      return deltaSec >= 0 ? `${n} ${unit} ago` : `in ${n} ${unit}`
    }
  }
  return new Date(then).toISOString().slice(0, 10)
}

/** clockUTC renders the time-of-day part of a timestamp, in UTC. */
export function clockUTC(iso: string | undefined): string {
  if (!iso) return '—'
  const t = Date.parse(iso)
  if (Number.isNaN(t)) return '—'
  return new Date(t).toISOString().slice(11, 19)
}

/** duration renders a millisecond count as ms or seconds. */
export function duration(ms: number | undefined): string {
  if (ms === undefined) return '—'
  if (ms < 1000) return `${Math.round(ms)} ms`
  return `${(ms / 1000).toFixed(1)} s`
}

/** elapsed renders the gap between two timestamps. */
export function elapsed(from: string | undefined, to: string | undefined): string {
  if (!from) return '—'
  const start = Date.parse(from)
  const end = to ? Date.parse(to) : Date.now()
  if (Number.isNaN(start) || Number.isNaN(end)) return '—'
  return duration(end - start)
}

/** confidenceLabel puts a word on a number, so the meter is never alone. */
export function confidenceLabel(c: number): string {
  if (c >= 0.8) return 'high confidence'
  if (c >= 0.5) return 'moderate confidence'
  return 'low confidence'
}

export function thousands(n: number | undefined): string {
  if (n === undefined) return '0'
  return n.toLocaleString('en-US')
}

export function percent(fraction: number): string {
  return `${Math.round(fraction * 100)}%`
}

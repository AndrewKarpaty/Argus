/*
 * Executable checks for the layout engine: `npm run check:layout`.
 *
 * The layout is the riskiest code in the console - a wrong number there means
 * boxes on top of each other or arrows through the middle of a card, and none
 * of it is caught by the type checker. These assertions pin the properties
 * that make the drawing readable.
 *
 * It runs as a plain Node script bundled by esbuild rather than under a test
 * runner: every vitest release that is free of advisories needs Node 20, and
 * this project still builds on Node 18.
 */
import type { Dependency, Service } from '../api/types'
import { layoutGraph, NODE_H, NODE_W } from './graphLayout'

declare const process: { exitCode?: number }
declare const console: { log: (...args: unknown[]) => void }

/** A copy of configs/services.yaml, so the check needs no running server. */
const services: Service[] = [
  { id: 'edge-gateway', name: 'Edge Gateway', kind: 'service', tier: 'critical' },
  { id: 'checkout', name: 'Checkout API', kind: 'service', tier: 'critical' },
  { id: 'orders', name: 'Orders Service', kind: 'service', tier: 'critical' },
  { id: 'payments', name: 'Payments Service', kind: 'service', tier: 'critical' },
  { id: 'inventory', name: 'Inventory Service', kind: 'service', tier: 'high' },
  { id: 'auth', name: 'Auth Service', kind: 'service', tier: 'critical' },
  { id: 'recommendations', name: 'Recommendations', kind: 'service', tier: 'standard' },
  { id: 'notifications', name: 'Notifications', kind: 'service', tier: 'standard' },
  { id: 'postgres-main', name: 'Postgres (main)', kind: 'database', tier: 'critical' },
  { id: 'redis-cache', name: 'Redis (cache)', kind: 'cache', tier: 'high' },
  { id: 'kafka', name: 'Kafka', kind: 'queue', tier: 'high' },
  { id: 'stripe', name: 'Stripe', kind: 'external', tier: 'critical' },
]

const hard = (from: string, to: string, kind: Dependency['kind']): Dependency => ({
  from,
  to,
  kind,
  criticality: 'hard',
})
const soft = (from: string, to: string, kind: Dependency['kind']): Dependency => ({
  from,
  to,
  kind,
  criticality: 'soft',
})

const dependencies: Dependency[] = [
  hard('edge-gateway', 'checkout', 'http'),
  hard('edge-gateway', 'orders', 'http'),
  hard('edge-gateway', 'auth', 'grpc'),
  hard('checkout', 'payments', 'grpc'),
  hard('checkout', 'inventory', 'grpc'),
  hard('checkout', 'auth', 'grpc'),
  soft('checkout', 'recommendations', 'http'),
  hard('checkout', 'postgres-main', 'sql'),
  hard('orders', 'postgres-main', 'sql'),
  hard('orders', 'kafka', 'queue'),
  soft('orders', 'notifications', 'queue'),
  hard('payments', 'postgres-main', 'sql'),
  hard('payments', 'kafka', 'queue'),
  hard('payments', 'stripe', 'external'),
  hard('inventory', 'postgres-main', 'sql'),
  soft('inventory', 'redis-cache', 'cache'),
  hard('auth', 'postgres-main', 'sql'),
  hard('auth', 'redis-cache', 'cache'),
  soft('recommendations', 'redis-cache', 'cache'),
  hard('notifications', 'kafka', 'queue'),
]

let failures = 0
function check(ok: boolean, what: string) {
  console.log(`${ok ? 'PASS' : 'FAIL'}  ${what}`)
  if (!ok) failures++
}

// The incident view: the shared database and everything that depends on it.
const impact = layoutGraph({
  services,
  dependencies,
  focus: 'postgres-main',
  view: 'impact',
  alerting: ['checkout', 'orders'],
  cause: 'postgres-main',
})

check(
  impact.nodes.map((n) => n.id).sort().join(',') ===
    'auth,checkout,edge-gateway,inventory,orders,payments,postgres-main',
  'the impact view draws exactly the services that depend on the focus',
)
check(impact.hidden.length === 5, `the other 5 services are reported as hidden (${impact.hidden.join(', ')})`)
check(impact.edges.length === 11, `11 edges joined the drawn nodes, got ${impact.edges.length}`)
check(impact.nodes.every((n) => n.w === NODE_W), 'every box is one column wide')
check(impact.width > 0 && impact.height > 0, `the canvas has a size (${impact.width}x${impact.height})`)

const hub = impact.nodes.find((n) => n.id === 'postgres-main')
check(!!hub && hub.layer === Math.max(...impact.nodes.map((n) => n.layer)), 'the hub sits in the last layer')
check(!!hub && hub.inbound === 5, `the hub counts its 5 callers, got ${hub?.inbound}`)
check(!!hub && hub.h > NODE_H, `the hub is drawn taller for its fan-in (h=${hub?.h})`)
check(
  impact.nodes.filter((n) => n.id !== 'postgres-main').every((n) => n.h === NODE_H),
  'nodes with few callers keep the base height',
)

let overlaps = 0
for (let i = 0; i < impact.nodes.length; i++) {
  for (let j = i + 1; j < impact.nodes.length; j++) {
    const a = impact.nodes[i]
    const b = impact.nodes[j]
    if (a.x < b.x + b.w && b.x < a.x + a.w && a.y < b.y + b.h && b.y < a.y + a.h) overlaps++
  }
}
check(overlaps === 0, `no two boxes overlap (${overlaps} found)`)

const placed = new Set(impact.nodes.map((n) => n.id))
check(
  impact.edges.every((e) => placed.has(e.from) && placed.has(e.to)),
  'every edge joins two drawn nodes',
)
check(
  impact.edges.every((e) => e.path.startsWith('M ') && e.path.includes('C ')),
  'every edge has a curve path',
)
check(
  impact.edges.filter((e) => e.fault).length === 5,
  'the edges into the named cause are marked as carrying the fault',
)

const stateOf = (id: string) => impact.nodes.find((n) => n.id === id)?.state
check(stateOf('postgres-main') === 'cause', 'the named cause is painted as the cause')
check(
  stateOf('checkout') === 'alerting' && stateOf('orders') === 'alerting',
  'the alerting services are painted as alerting',
)
check(
  stateOf('payments') === 'risk' && stateOf('auth') === 'risk' && stateOf('edge-gateway') === 'risk',
  'the rest of the blast radius is painted as at risk',
)

// The property that keeps a five-way fan-in readable: entry points run down
// the target's edge in the same order as the callers run down the canvas, so
// the incoming arrows never cross one another.
const fanIn = impact.edges
  .filter((e) => e.to === 'postgres-main')
  .map((e) => {
    const src = impact.nodes.find((n) => n.id === e.from)
    const parts = e.path.trim().split(/[\s,]+/)
    return { src: (src?.y ?? 0) + (src?.h ?? 0) / 2, entry: Number(parts[parts.length - 1]) }
  })
  .sort((a, b) => a.src - b.src)
check(
  fanIn.every((v, i) => i === 0 || v.entry >= fanIn[i - 1].entry),
  `the fan-in entry points follow caller order (${fanIn.map((v) => v.entry).join(', ')})`,
)

// The mirror view, and the unfiltered one.
const deps = layoutGraph({ services, dependencies, focus: 'checkout', view: 'dependencies' })
check(
  deps.nodes.some((n) => n.id === 'postgres-main') && !deps.nodes.some((n) => n.id === 'edge-gateway'),
  'the dependencies view keeps callees and drops callers',
)

const all = layoutGraph({ services, dependencies, focus: null, view: 'all' })
check(all.nodes.length === services.length, `the whole-catalog view draws all ${services.length} services`)
check(all.edges.length === dependencies.length, `and all ${dependencies.length} dependencies`)
check(all.hidden.length === 0, 'with nothing hidden')
check(
  all.edges.filter((e) => e.criticality === 'soft').length === 4,
  'soft dependencies survive the round trip',
)

// Degenerate input must draw nothing rather than throw.
const empty = layoutGraph({ services: [], dependencies: [], focus: null, view: 'all' })
check(empty.nodes.length === 0 && empty.edges.length === 0, 'an empty catalog yields an empty drawing')

const ghost = layoutGraph({ services, dependencies, focus: 'not-in-catalog', view: 'impact' })
check(ghost.nodes.length === services.length, 'an unknown focus falls back to the whole catalog')

console.log(failures === 0 ? '\nALL LAYOUT CHECKS PASS' : `\n${failures} CHECK(S) FAILED`)
process.exitCode = failures === 0 ? 0 : 1

/*
 * Layered layout for the service dependency graph.
 *
 * The graph is a DAG in practice (the catalog loader warns about cycles), and
 * what an operator needs to read off it is direction: callers on the left,
 * the things they depend on to the right, so a fault visibly travels right to
 * left. That is a layered drawing, so:
 *
 *   1. layer by longest path    - every edge points one or more layers right
 *   2. order inside each layer  - barycentre sweeps, to cut edge crossings
 *   3. spread the arrow heads   - a hub with five callers gets a taller box
 *                                 and one entry point per caller, ordered the
 *                                 same way as the callers themselves, so the
 *                                 fan-in never crosses itself
 *   4. route                    - one curve per edge; edges that skip a layer
 *                                 bow over the top instead of crossing boxes
 *
 * Everything here is pure: same input, same drawing.
 */
import type { Criticality, Dependency, Service, ServiceKind, Tier } from '../api/types'

export const NODE_W = 180
export const NODE_H = 48
const COL_GAP = 40
const ROW_GAP = 40
const PAD_X = 16
const PAD_TOP = 30
const PAD_BOTTOM = 16
/** Vertical room each extra inbound edge buys a hub node. */
const ENTRY_PITCH = 24
const MAX_NODE_H = 168

export type NodeState = 'cause' | 'alerting' | 'risk' | 'plain'

export interface LaidOutNode {
  id: string
  name: string
  kind: ServiceKind
  tier: Tier
  x: number
  y: number
  w: number
  h: number
  layer: number
  state: NodeState
  /** Number of services that depend on this one, within the drawn subgraph. */
  inbound: number
}

export interface LaidOutEdge {
  from: string
  to: string
  criticality: Criticality
  /** True when this edge delivers the fault into the root-cause candidate. */
  fault: boolean
  path: string
}

export interface LaidOutLayer {
  index: number
  x: number
  count: number
}

export interface Layout {
  width: number
  height: number
  nodes: LaidOutNode[]
  edges: LaidOutEdge[]
  layers: LaidOutLayer[]
  /** Services excluded by the current view, for the "hidden" note. */
  hidden: string[]
}

export type GraphView = 'impact' | 'dependencies' | 'all'

export interface LayoutInput {
  services: Service[]
  dependencies: Dependency[]
  /** The service the view is centred on; null means the whole catalog. */
  focus: string | null
  view: GraphView
  /** Services currently reporting symptoms. */
  alerting?: string[]
  /** The leading root-cause candidate, if an investigation named one. */
  cause?: string | null
}

/** reachable walks the graph from seed and returns everything it touches. */
function reachable(seed: string, adjacency: Map<string, string[]>): Set<string> {
  const seen = new Set<string>([seed])
  const queue = [seed]
  while (queue.length > 0) {
    const cur = queue.shift() as string
    for (const next of adjacency.get(cur) ?? []) {
      if (!seen.has(next)) {
        seen.add(next)
        queue.push(next)
      }
    }
  }
  return seen
}

export function layoutGraph(input: LayoutInput): Layout {
  const { services, dependencies, focus, view } = input
  const alerting = new Set(input.alerting ?? [])
  const cause = input.cause ?? null

  const byId = new Map(services.map((s) => [s.id, s]))

  // Adjacency in both directions over the whole catalog.
  const callees = new Map<string, string[]>()
  const callers = new Map<string, string[]>()
  const link = (m: Map<string, string[]>, key: string, value: string) => {
    const list = m.get(key)
    if (list) list.push(value)
    else m.set(key, [value])
  }
  for (const d of dependencies) {
    if (!byId.has(d.from) || !byId.has(d.to)) continue
    link(callees, d.from, d.to)
    link(callers, d.to, d.from)
  }

  // 0. Which services does this view draw?
  let visible: Set<string>
  if (!focus || view === 'all' || !byId.has(focus)) {
    visible = new Set(byId.keys())
  } else if (view === 'impact') {
    // Everything that transitively depends on the focus: what breaks with it.
    visible = reachable(focus, callers)
  } else {
    // Everything the focus transitively depends on: where its faults come from.
    visible = reachable(focus, callees)
  }

  const hidden = services.filter((s) => !visible.has(s.id)).map((s) => s.id)
  const edges = dependencies.filter((d) => visible.has(d.from) && visible.has(d.to))

  // 1. Longest-path layering. Repeat until stable; the iteration cap keeps a
  //    cyclic catalog from spinning here instead of rendering.
  const layer = new Map<string, number>()
  for (const id of visible) layer.set(id, 0)
  for (let pass = 0; pass < visible.size + 1; pass++) {
    let moved = false
    for (const d of edges) {
      const want = (layer.get(d.from) ?? 0) + 1
      if (want > (layer.get(d.to) ?? 0)) {
        layer.set(d.to, want)
        moved = true
      }
    }
    if (!moved) break
  }

  const layerCount = Math.max(...[...layer.values()], 0) + 1
  const columns: string[][] = Array.from({ length: layerCount }, () => [])
  for (const id of [...visible].sort()) {
    columns[layer.get(id) ?? 0]!.push(id)
  }

  // 2. Barycentre sweeps: put each node near the average position of the
  //    neighbours it is already connected to, which is the cheap classic way
  //    to cut crossings without a full Sugiyama implementation.
  const indexIn = (col: string[], id: string) => col.indexOf(id)
  for (let sweep = 0; sweep < 4; sweep++) {
    const forward = sweep % 2 === 0
    for (let i = 1; i < columns.length; i++) {
      const col = forward ? columns[i]! : columns[columns.length - 1 - i]!
      const ref = forward ? columns[i - 1]! : columns[columns.length - i]!
      const neighbours = forward ? callers : callees
      const score = new Map<string, number>()
      col.forEach((id, pos) => {
        const ns = (neighbours.get(id) ?? []).filter((n) => ref.includes(n))
        if (ns.length === 0) {
          score.set(id, pos)
          return
        }
        const sum = ns.reduce((acc, n) => acc + indexIn(ref, n), 0)
        score.set(id, sum / ns.length)
      })
      col.sort((a, b) => (score.get(a) ?? 0) - (score.get(b) ?? 0) || a.localeCompare(b))
    }
  }

  // 3. Node boxes. A node with several callers is drawn taller so its arrow
  //    heads do not pile up on one short edge.
  const inboundCount = new Map<string, number>()
  for (const d of edges) inboundCount.set(d.to, (inboundCount.get(d.to) ?? 0) + 1)

  const heightOf = (id: string) => {
    const n = inboundCount.get(id) ?? 0
    if (n < 4) return NODE_H
    return Math.min(MAX_NODE_H, n * ENTRY_PITCH + ENTRY_PITCH)
  }

  // Lay each column out from the top, then centre the shorter columns against
  // the tallest one so the drawing reads as a band rather than a staircase.
  const columnHeights = columns.map((col) =>
    col.reduce((acc, id, i) => acc + heightOf(id) + (i > 0 ? ROW_GAP : 0), 0),
  )
  const tallest = Math.max(...columnHeights, NODE_H)

  // Everything that depends on the named cause is at risk from it. Computed
  // once here rather than per node.
  const riskSet = cause && visible.has(cause) ? reachable(cause, callers) : new Set<string>()

  const nodes: LaidOutNode[] = []
  const layers: LaidOutLayer[] = []
  columns.forEach((col, i) => {
    const x = PAD_X + i * (NODE_W + COL_GAP)
    layers.push({ index: i, x, count: col.length })
    let y = PAD_TOP + (tallest - (columnHeights[i] ?? 0)) / 2
    for (const id of col) {
      const svc = byId.get(id)!
      const h = heightOf(id)
      nodes.push({
        id,
        name: svc.name || id,
        kind: svc.kind,
        tier: svc.tier,
        x,
        y: Math.round(y),
        w: NODE_W,
        h,
        layer: i,
        state: stateOf(id, cause, alerting, riskSet, focus),
        inbound: inboundCount.get(id) ?? 0,
      })
      y += h + ROW_GAP
    }
  })

  const nodeById = new Map(nodes.map((n) => [n.id, n]))
  const width = PAD_X * 2 + layerCount * NODE_W + (layerCount - 1) * COL_GAP
  const height = Math.round(PAD_TOP + tallest + PAD_BOTTOM)

  // 4. Entry points, then routing. Sorting each target's callers by their own
  //    vertical position and assigning entry points in that same order is what
  //    keeps the fan-in free of self-crossings.
  const entryY = new Map<string, number>()
  for (const target of nodes) {
    const incoming = edges
      .filter((d) => d.to === target.id)
      .map((d) => ({ d, src: nodeById.get(d.from)! }))
      .sort((a, b) => a.src.y + a.src.h / 2 - (b.src.y + b.src.h / 2))

    if (incoming.length === 1) {
      entryY.set(edgeKey(incoming[0]!.d), target.y + target.h / 2)
      continue
    }
    const top = target.y + 10
    const span = target.h - 20
    incoming.forEach(({ d }, i) => {
      const t = incoming.length === 1 ? 0.5 : i / (incoming.length - 1)
      entryY.set(edgeKey(d), Math.round(top + span * t))
    })
  }

  // The bow for layer-skipping edges sits above every box, so such an edge
  // never runs through the middle of an intermediate column.
  const topMost = Math.min(...nodes.map((n) => n.y), PAD_TOP)
  const bowY = Math.max(6, topMost - 18)

  const laidOutEdges: LaidOutEdge[] = edges.map((d) => {
    const from = nodeById.get(d.from)!
    const to = nodeById.get(d.to)!
    const sx = from.x + from.w
    const sy = Math.round(from.y + from.h / 2)
    const tx = to.x
    const ty = entryY.get(edgeKey(d)) ?? Math.round(to.y + to.h / 2)
    const span = to.layer - from.layer

    let path: string
    if (span <= 1) {
      const dx = Math.max(24, (tx - sx) / 2)
      path = `M ${sx} ${sy} C ${sx + dx} ${sy}, ${tx - dx} ${ty}, ${tx} ${ty}`
    } else {
      const lift = Math.max(40, (tx - sx) / 3)
      path = `M ${sx} ${sy} C ${sx + lift} ${bowY}, ${tx - lift} ${bowY}, ${tx} ${ty}`
    }

    return {
      from: d.from,
      to: d.to,
      criticality: d.criticality,
      fault: !!cause && d.to === cause,
      path,
    }
  })

  return { width, height, nodes, edges: laidOutEdges, layers, hidden }
}

function edgeKey(d: Dependency): string {
  return `${d.from}\u0000${d.to}\u0000${d.kind}`
}

/**
 * stateOf decides how a node is painted. Red means "this one is reporting
 * symptoms", blue means "this is what Argus concluded", orange means "at risk
 * because it depends on the cause", and plain means nothing to say.
 */
function stateOf(
  id: string,
  cause: string | null,
  alerting: Set<string>,
  riskSet: Set<string>,
  focus: string | null,
): NodeState {
  if (cause === id) return 'cause'
  if (alerting.has(id)) return 'alerting'
  if (riskSet.has(id)) return 'risk'
  if (!cause && focus === id) return 'cause'
  return 'plain'
}

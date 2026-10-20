import type { Service } from '../api/types'
import type { LaidOutNode, Layout, NodeState } from '../lib/graphLayout'

const FILL: Record<NodeState, string> = {
  cause: 'var(--node-cause-fill)',
  alerting: 'var(--node-alert-fill)',
  risk: 'var(--node-risk-fill)',
  plain: 'var(--node-fill)',
}

const STROKE: Record<NodeState, string> = {
  cause: 'var(--accent)',
  alerting: 'var(--critical)',
  risk: 'var(--serious)',
  plain: 'var(--line-strong)',
}

const KIND_TAG: Record<Service['kind'], string> = {
  service: '',
  database: 'DB',
  cache: 'CACHE',
  queue: 'QUEUE',
  external: 'EXT',
}

export function DependencyGraph({
  layout,
  focus,
  onSelect,
}: {
  layout: Layout
  focus: string | null
  onSelect: (serviceId: string) => void
}) {
  if (layout.nodes.length === 0) {
    return (
      <p className="small muted" style={{ margin: 0 }}>
        Nothing to draw for this view.
      </p>
    )
  }

  return (
    <div className="graph-scroll">
      <svg
        width={layout.width}
        height={layout.height}
        viewBox={`0 0 ${layout.width} ${layout.height}`}
        style={{ minWidth: layout.width }}
        aria-label={`Dependency graph, ${layout.nodes.length} services and ${layout.edges.length} dependencies. Callers are on the left; faults travel against the arrows.`}
      >
        <defs>
          <marker id="argus-head" viewBox="0 0 6 6" refX="5" refY="3" markerWidth="5" markerHeight="5" orient="auto">
            <path d="M0 0 L6 3 L0 6 Z" fill="var(--edge)" />
          </marker>
          <marker
            id="argus-head-fault"
            viewBox="0 0 6 6"
            refX="5"
            refY="3"
            markerWidth="5"
            markerHeight="5"
            orient="auto"
          >
            <path d="M0 0 L6 3 L0 6 Z" fill="var(--critical)" />
          </marker>
        </defs>

        {layout.layers.map((layer) => (
          <text
            key={layer.index}
            x={layer.x}
            y={16}
            fontSize={9.5}
            fontWeight={600}
            letterSpacing="0.12em"
            fill="var(--ink-3)"
          >
            {`DEPTH ${layer.index}`}
          </text>
        ))}

        {layout.edges.map((edge) => (
          <path
            key={`${edge.from}->${edge.to}`}
            d={edge.path}
            fill="none"
            stroke={edge.fault ? 'var(--critical)' : 'var(--edge)'}
            strokeWidth={edge.fault ? 2 : 1.4}
            strokeLinecap="round"
            strokeDasharray={edge.criticality === 'soft' ? '5 4' : undefined}
            markerEnd={edge.fault ? 'url(#argus-head-fault)' : 'url(#argus-head)'}
          />
        ))}

        {layout.nodes.map((node) => (
          <GraphNode key={node.id} node={node} onSelect={onSelect} focused={node.id === focus} />
        ))}
      </svg>
    </div>
  )
}

function GraphNode({
  node,
  focused,
  onSelect,
}: {
  node: LaidOutNode
  focused: boolean
  onSelect: (id: string) => void
}) {
  const tag = KIND_TAG[node.kind]
  const tall = node.h > 60
  const labelY = tall ? node.y + 30 : node.y + node.h / 2 + 4.5

  return (
    <g
      role="button"
      tabIndex={0}
      aria-label={`${node.id}, ${node.kind}, tier ${node.tier}, ${node.inbound} services depend on it${
        node.state === 'cause' ? ', root cause candidate' : node.state === 'alerting' ? ', alerting' : ''
      }`}
      style={{ cursor: 'pointer' }}
      onClick={() => onSelect(node.id)}
      onKeyDown={(e) => {
        if (e.key === 'Enter' || e.key === ' ') {
          e.preventDefault()
          onSelect(node.id)
        }
      }}
    >
      <rect
        x={node.x}
        y={node.y}
        width={node.w}
        height={node.h}
        rx={node.kind === 'service' ? 7 : 14}
        fill={FILL[node.state]}
        stroke={STROKE[node.state]}
        strokeWidth={node.state === 'cause' || node.state === 'alerting' ? 2 : 1.4}
      />
      {focused && (
        <rect
          x={node.x - 4}
          y={node.y - 4}
          width={node.w + 8}
          height={node.h + 8}
          rx={node.kind === 'service' ? 11 : 18}
          fill="none"
          stroke="var(--accent)"
          strokeWidth="1"
          opacity="0.45"
        />
      )}

      <text
        x={node.x + 14}
        y={labelY}
        fontSize={13.5}
        fontFamily="var(--mono)"
        fontWeight={node.state === 'cause' ? 600 : 400}
        fill={node.state === 'plain' ? 'var(--ink-2)' : 'var(--ink)'}
      >
        {node.id}
      </text>

      {tag && (
        <text
          x={node.x + node.w - 12}
          y={labelY - 0.5}
          fontSize={9.5}
          fontWeight={600}
          letterSpacing="0.08em"
          textAnchor="end"
          fill="var(--ink-3)"
        >
          {tag}
        </text>
      )}

      {node.state === 'alerting' && (
        <text
          x={node.x + 14}
          y={labelY + 15}
          fontSize={9.5}
          fontWeight={600}
          letterSpacing="0.1em"
          fill="var(--critical-ink)"
        >
          ALERTING
        </text>
      )}

      {node.state === 'cause' && tall && (
        <>
          <text
            x={node.x + 14}
            y={labelY + 18}
            fontSize={9.5}
            fontWeight={600}
            letterSpacing="0.1em"
            fill="var(--ink-3)"
          >
            {node.kind.toUpperCase()} · TIER {node.tier.toUpperCase()}
          </text>
          <text x={node.x + 14} y={labelY + 40} fontSize={11} fontWeight={600} fill="var(--accent-ink)">
            Root cause candidate
          </text>
          <text x={node.x + 14} y={labelY + 56} fontSize={11} fill="var(--ink-2)">
            {node.inbound} services depend on it
          </text>
        </>
      )}

      {node.state === 'cause' && !tall && (
        <circle cx={node.x} cy={node.y} r={11} fill="var(--accent)" />
      )}
      {node.state === 'cause' && tall && (
        <>
          <circle cx={node.x} cy={node.y} r={12} fill="var(--accent)" />
          <text
            x={node.x}
            y={node.y + 4.5}
            fontSize={12.5}
            fontWeight={700}
            textAnchor="middle"
            fill="var(--accent-on)"
          >
            1
          </text>
        </>
      )}
    </g>
  )
}

export function GraphLegend({ hasFault, hasSoft }: { hasFault: boolean; hasSoft: boolean }) {
  return (
    <div
      className="row-wrap"
      style={{ gap: '10px 20px', paddingTop: 16, borderTop: '1px solid var(--line)' }}
    >
      {hasFault && (
        <LegendLine color="var(--critical)" width={2} label="carries the fault" />
      )}
      <LegendLine color="var(--edge)" width={1.4} label="hard dependency" />
      {hasSoft && <LegendLine color="var(--edge)" width={1.4} dashed label="soft — degrades only" />}
      <LegendSwatch fill="var(--node-cause-fill)" stroke="var(--accent)" label="root cause" />
      <LegendSwatch fill="var(--node-alert-fill)" stroke="var(--critical)" label="alerting" />
      <LegendSwatch fill="var(--node-risk-fill)" stroke="var(--serious)" label="at risk" />
      <LegendSwatch fill="var(--node-fill)" stroke="var(--line-strong)" label="healthy" />
    </div>
  )
}

function LegendLine({
  color,
  width,
  dashed,
  label,
}: {
  color: string
  width: number
  dashed?: boolean
  label: string
}) {
  return (
    <span className="row tiny muted" style={{ gap: 7 }}>
      <svg width="26" height="8" viewBox="0 0 26 8" aria-hidden="true">
        <path d="M0 4h20" stroke={color} strokeWidth={width} strokeDasharray={dashed ? '5 4' : undefined} />
        <path d="M20 1 L26 4 L20 7 Z" fill={color} />
      </svg>
      {label}
    </span>
  )
}

function LegendSwatch({ fill, stroke, label }: { fill: string; stroke: string; label: string }) {
  return (
    <span className="row tiny muted" style={{ gap: 7 }}>
      <span
        style={{ width: 12, height: 12, borderRadius: 3, background: fill, border: `2px solid ${stroke}` }}
      />
      {label}
    </span>
  )
}

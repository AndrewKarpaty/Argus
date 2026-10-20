/*
 * These types mirror the JSON that internal/domain and internal/api produce.
 * Keep them in step with the Go structs: the field names here are the wire
 * names, not camelCase conveniences.
 */

export type Severity = 'critical' | 'high' | 'medium' | 'low'
export type IncidentStatus = 'open' | 'investigating' | 'mitigated' | 'resolved'
export type InvestigationStatus = 'queued' | 'running' | 'completed' | 'failed'
export type ServiceKind = 'service' | 'database' | 'queue' | 'cache' | 'external'
export type Tier = 'critical' | 'high' | 'standard'
export type DependencyKind = 'http' | 'grpc' | 'sql' | 'queue' | 'cache' | 'external'
export type Criticality = 'hard' | 'soft'
export type SignalSource = 'alert' | 'metric' | 'log' | 'trace' | 'manual'
export type EvidenceKind = 'signal' | 'change' | 'topology' | 'trace' | 'reasoning'
export type Direction = 'dependencies' | 'dependents'

export interface Service {
  id: string
  name: string
  kind: ServiceKind
  tier: Tier
  team?: string
  repo?: string
  runbook?: string
  description?: string
  labels?: Record<string, string>
}

export interface Dependency {
  from: string
  to: string
  kind: DependencyKind
  criticality: Criticality
  description?: string
}

export interface TimeWindow {
  from: string
  to: string
}

export interface Signal {
  id: string
  source: SignalSource
  service_id: string
  title: string
  description?: string
  severity?: Severity
  observed_at: string
  value?: number
  unit?: string
  labels?: Record<string, string>
}

export interface Evidence {
  kind: EvidenceKind
  service_id?: string
  summary: string
  ref?: string
  at?: string
}

export interface Hypothesis {
  id: string
  title: string
  root_cause_service: string
  confidence: number
  rationale: string
  propagation_path?: string[]
  evidence?: Evidence[]
  suggested_actions?: string[]
  refuted_by?: string
}

export interface Step {
  seq: number
  tool: string
  input?: string
  output?: string
  error?: string
  at: string
  duration_ms: number
}

export interface TokenUsage {
  input_tokens: number
  output_tokens: number
  cache_read_tokens?: number
  cache_write_tokens?: number
}

export interface Investigation {
  id: string
  incident_id: string
  status: InvestigationStatus
  engine: string
  summary?: string
  hypotheses?: Hypothesis[]
  steps?: Step[]
  usage?: TokenUsage
  error?: string
  started_at: string
  finished_at?: string
}

export interface Incident {
  id: string
  title: string
  description?: string
  severity: Severity
  status: IncidentStatus
  affected_services: string[]
  signals?: Signal[]
  started_at: string
  detected_at: string
  resolved_at?: string
  created_at: string
  updated_at: string
}

export interface Hop {
  service_id: string
  depth: number
  via: string
  kind: DependencyKind
  criticality: Criticality
  hard_path: boolean
}

export interface Suspect {
  service_id: string
  coverage: number
  explains: string[]
  min_depth: number
  hard_path: boolean
  affected: boolean
  score: number
  reason: string
}

/* ---- response envelopes ---- */

export interface Ready {
  status: string
  version: string
  engine: string
  catalog_source: string
  catalog_loaded_at: string
  services: number
  dependencies: number
  telemetry_providers?: string[]
}

/**
 * A row of the incident list. The API attaches the newest investigation so the
 * list can show the leading hypothesis without a request per row.
 */
export interface IncidentRow extends Incident {
  latest_investigation?: Investigation
}

export interface IncidentList {
  count: number
  incidents: IncidentRow[] | null
}

export interface IncidentDetail {
  incident: Incident
  window: TimeWindow
  investigations: Investigation[] | null
}

export interface CreatedIncident {
  incident: Incident
  investigation?: Investigation
  investigation_error?: string
  unknown_services?: string[]
  warning?: string
}

export interface GraphResponse {
  services: Service[] | null
  dependencies: Dependency[] | null
  cycles: string[][] | null
  counts: { services: number; dependencies: number }
}

export interface BlastRadius {
  service: string
  affected_count: number
  critical_affected: number
  affected: Hop[] | null
}

export interface SuspectList {
  affected: string[]
  count: number
  suspects: Suspect[] | null
  note: string
}

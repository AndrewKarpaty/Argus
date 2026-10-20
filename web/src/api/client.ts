import type {
  BlastRadius,
  CreatedIncident,
  GraphResponse,
  Incident,
  IncidentDetail,
  IncidentList,
  Investigation,
  Ready,
  Severity,
  SuspectList,
} from './types'

/** ApiError carries the status code so callers can tell 404 from 500. */
export class ApiError extends Error {
  readonly status: number
  readonly requestId?: string

  constructor(status: number, message: string, requestId?: string) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.requestId = requestId
  }
}

interface ErrorBody {
  error?: string
  detail?: string
  request_id?: string
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(path, {
    ...init,
    headers: {
      Accept: 'application/json',
      ...(init?.body ? { 'Content-Type': 'application/json' } : {}),
      ...init?.headers,
    },
  })

  if (!res.ok) {
    // The API answers errors with a JSON body; fall back to the status text
    // when something else (a proxy, say) answered instead.
    let message = `${res.status} ${res.statusText}`
    let requestId: string | undefined
    try {
      const body = (await res.json()) as ErrorBody
      if (body.error) message = body.detail ? `${body.error}: ${body.detail}` : body.error
      requestId = body.request_id
    } catch {
      /* keep the status line */
    }
    throw new ApiError(res.status, message, requestId)
  }

  if (res.status === 204) return undefined as T
  return (await res.json()) as T
}

export interface NewIncidentInput {
  title: string
  description?: string
  severity: Severity
  affected_services: string[]
  investigate: boolean
}

export const api = {
  ready: () => request<Ready>('/readyz'),

  incidents: (limit = 50) => request<IncidentList>(`/api/v1/incidents?limit=${limit}`),

  incident: (id: string) => request<IncidentDetail>(`/api/v1/incidents/${encodeURIComponent(id)}`),

  createIncident: (input: NewIncidentInput) =>
    request<CreatedIncident>('/api/v1/incidents', {
      method: 'POST',
      body: JSON.stringify(input),
    }),

  investigate: (incidentId: string) =>
    request<{ investigation: Investigation }>(
      `/api/v1/incidents/${encodeURIComponent(incidentId)}/investigate`,
      { method: 'POST' },
    ),

  investigation: (id: string) =>
    request<{ investigation: Investigation }>(`/api/v1/investigations/${encodeURIComponent(id)}`),

  graph: () => request<GraphResponse>('/api/v1/graph'),

  blastRadius: (serviceId: string) =>
    request<BlastRadius>(`/api/v1/services/${encodeURIComponent(serviceId)}/blast-radius`),

  suspects: (serviceIds: string[], depth?: number) => {
    const params = new URLSearchParams()
    for (const id of serviceIds) params.append('service', id)
    if (depth) params.set('depth', String(depth))
    return request<SuspectList>(`/api/v1/suspects?${params.toString()}`)
  },
}

/** openIncident is the set of statuses that still need somebody's attention. */
export function isOpen(incident: Incident): boolean {
  return incident.status === 'open' || incident.status === 'investigating'
}

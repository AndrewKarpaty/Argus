import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, type NewIncidentInput } from './client'
import type { Investigation } from './types'

/** Investigations run in the background, so poll while one is in flight. */
function pollWhileRunning(iv: Investigation | undefined): number | false {
  if (!iv) return 1500
  return iv.status === 'queued' || iv.status === 'running' ? 1500 : false
}

export function useReady() {
  return useQuery({
    queryKey: ['ready'],
    queryFn: api.ready,
    staleTime: 30_000,
  })
}

export function useIncidents(limit = 50) {
  return useQuery({
    queryKey: ['incidents', limit],
    queryFn: () => api.incidents(limit),
    // The list shows live investigation state, so keep it reasonably fresh.
    refetchInterval: 5000,
  })
}

export function useIncident(id: string | null) {
  return useQuery({
    queryKey: ['incident', id],
    queryFn: () => api.incident(id as string),
    enabled: !!id,
  })
}

export function useInvestigation(id: string | null) {
  return useQuery({
    queryKey: ['investigation', id],
    queryFn: () => api.investigation(id as string),
    enabled: !!id,
    refetchInterval: (query) => pollWhileRunning(query.state.data?.investigation),
  })
}

export function useGraph() {
  return useQuery({
    queryKey: ['graph'],
    queryFn: api.graph,
    staleTime: 60_000,
  })
}

export function useBlastRadius(serviceId: string | null) {
  return useQuery({
    queryKey: ['blast-radius', serviceId],
    queryFn: () => api.blastRadius(serviceId as string),
    enabled: !!serviceId,
    staleTime: 60_000,
  })
}

export function useSuspects(serviceIds: string[]) {
  const key = [...serviceIds].sort().join(',')
  return useQuery({
    queryKey: ['suspects', key],
    queryFn: () => api.suspects(serviceIds),
    enabled: serviceIds.length > 0,
    staleTime: 60_000,
  })
}

export function useCreateIncident() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: NewIncidentInput) => api.createIncident(input),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['incidents'] })
    },
  })
}

export function useStartInvestigation() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (incidentId: string) => api.investigate(incidentId),
    onSuccess: (_data, incidentId) => {
      void qc.invalidateQueries({ queryKey: ['incident', incidentId] })
      void qc.invalidateQueries({ queryKey: ['incidents'] })
    },
  })
}

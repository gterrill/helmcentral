import { apiRequest, declareQuery, setQueryData, useApiQuery } from '@/lib/api-query'
import { emptyVesselDiscovery, noTracking, type VesselDiscoveryResponse } from '@/lib/vessel-settings'

/**
 * What the boat is reporting right now, grouped by kind, with the equipment
 * item that owns each source (GET /api/vessel/discovery). Settings -> SignalK
 * lists it, and the equipment page picks an item's source from it. It also
 * carries each anomaly check's setup status.
 *
 * Plain fetch-and-throw: a failure is shown, not masked.
 */
// A poll that answers the same keeps the same object.
declareQuery(['/api/vessel/discovery'], { structuralSharing: true })

export function useVesselDiscovery(enabled = true) {
  const { data, isLoading, error, refetch } = useApiQuery<VesselDiscoveryResponse>({
    queryKey: ['/api/vessel/discovery'],
    path: '/api/vessel/discovery',
    enabled,
    select: (body: VesselDiscoveryResponse) => ({
      connected: body.connected === true,
      groups: (Array.isArray(body.groups) ? body.groups : []).map((g) => ({
        kind: g.kind,
        mappable: g.mappable === true,
        signature_label: g.signature_label ?? '',
        rows: (Array.isArray(g.rows) ? g.rows : []).map((r) => ({ ...r, metrics: Array.isArray(r.metrics) ? r.metrics : [], last_seen: r.last_seen ?? null })),
      })),
      detectors: body.detectors ?? {},
      tracking: { ...noTracking, ...body.tracking },
    }),
  })

  return {
    discovery: data ?? emptyVesselDiscovery,
    loading: isLoading,
    error: error?.message ?? null,
    refresh: refetch,
  }
}

/**
 * Marks battery instance `instance` as a charger's input, not a battery, or
 * clears the mark (PUT /api/vessel/charger-inputs/:instance), then reloads the
 * discovery list. A refusal throws, so the row can say so. Resolves true when
 * the list reloaded, false when the mark was saved but the reload failed.
 */
export async function markChargerInput(instance: string, marked: boolean): Promise<boolean> {
  await apiRequest(`/api/vessel/charger-inputs/${encodeURIComponent(instance)}`, { method: 'PUT', json: { marked } })
  try {
    setQueryData<VesselDiscoveryResponse>(['/api/vessel/discovery'], await apiRequest<VesselDiscoveryResponse>('/api/vessel/discovery'))
    return true
  } catch {
    return false
  }
}

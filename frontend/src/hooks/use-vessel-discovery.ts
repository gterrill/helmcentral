import { useApiQuery } from '@/lib/api-query'
import { emptyVesselDiscovery, type VesselDiscoveryResponse } from '@/lib/vessel-settings'

/**
 * What the boat is reporting right now, grouped by kind, with the equipment
 * item that owns each source (GET /api/vessel/discovery). Settings -> SignalK
 * lists it, and the equipment page picks an item's source from it. It also
 * carries each anomaly check's setup status.
 *
 * Plain fetch-and-throw: a failure is shown, not masked.
 */
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
        rows: (Array.isArray(g.rows) ? g.rows : []).map((r) => ({ ...r, metrics: Array.isArray(r.metrics) ? r.metrics : [], last_logged: r.last_logged ?? null })),
      })),
      detectors: body.detectors ?? {},
      history_error: body.history_error ?? '',
    }),
  })

  return {
    discovery: data ?? emptyVesselDiscovery,
    loading: isLoading,
    error: error?.message ?? null,
    refresh: refetch,
  }
}

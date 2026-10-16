import { useApiQuery } from '@/lib/api-query'
import type { VesselRoles } from '@/lib/vessel-settings'

/**
 * Which equipment item plays each single-choice part (Settings -> SignalK ->
 * Roles), read from the same cached settings response the Settings page uses.
 * A cluster tile uses it to offer the alternator that belongs to its engine's
 * side. Null when the settings cannot be read (a non-admin session, or the
 * stored block is unreadable): there is then no default, never a guessed one.
 */
export function useVesselRoles(enabled = true): VesselRoles | null {
  const { data } = useApiQuery<{ roles?: VesselRoles }>({
    queryKey: ['/api/settings'],
    path: '/api/settings',
    enabled,
  })
  return data?.roles ?? null
}

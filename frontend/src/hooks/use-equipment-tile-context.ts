import { useMemo } from 'react'

import { useEquipment } from '@/hooks/use-inventory'
import { useEquipmentProfiles } from '@/hooks/use-equipment-profiles'
import { useVesselDiscovery } from '@/hooks/use-vessel-discovery'
import type { EquipmentTileContext } from '@/lib/equipment-tiles'

/**
 * What a tile that shows a piece of equipment needs to find its readings: the
 * items, their profiles, and which sources the boat is reporting (ADR 0187).
 * Each part is the same cached read the Inventory and Settings pages use, so
 * a tile adds no request of its own, and an edit to an item or a profile
 * reaches every tile showing it.
 *
 * `reporting` stays null until Discovery has answered, so a tile never says
 * "Not reporting" because a request has not come back yet.
 *
 * Nothing live is in here: engine states ride the gauge-values event, and the
 * host that needs one reads it itself (ADR 0215), so a tick or an engine
 * keying off never makes every equipment tile re-resolve.
 */
export function useEquipmentTileContext(enabled = true): EquipmentTileContext {
  const { items } = useEquipment(enabled ? {} : null)
  const { profiles } = useEquipmentProfiles(enabled)
  const { discovery, loading, error } = useVesselDiscovery(enabled)

  const reporting = useMemo(() => {
    if (!enabled || loading || error || !discovery.connected) return null
    const set = new Set<string>()
    for (const group of discovery.groups) {
      for (const row of group.rows) if (row.reporting) set.add(row.source)
    }
    return set
  }, [enabled, loading, error, discovery])

  return useMemo(() => ({ items, profiles, reporting }), [items, profiles, reporting])
}

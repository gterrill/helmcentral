// ADR 0123: the Inventory panel's sections, listed under Inventory in the
// sidebar (App.tsx) while the panel is open. App owns which section is active
// and mirrors it to the URL (ADR 0074). One flat list, in the order shown:
// what the boat carries and where it is, then what keeps it running.

import type { InventoryList, InventoryRoute } from '@/lib/app-location'
import type { AppRouter } from '@/lib/app-router'

export type InventorySectionId = 'equipment' | 'maintenance' | 'profiles' | 'locations' | 'stocktake'

export const INVENTORY_SECTIONS: Array<{ id: InventorySectionId; label: string }> = [
  { id: 'equipment', label: 'Equipment' },
  { id: 'locations', label: 'Locations' },
  // ADR 0127 (the plan's Phase B): NFC and keyboard-wedge scanning, not the
  // RFID hardware ADR 0065 §4 originally designed for - that stays
  // designed-not-built (docs/features/inventory-tracking.md says so).
  { id: 'stocktake', label: 'Stocktake' },
  // ADR 0138: service rules and the log they're completed into - a
  // plain sortable list, not a fifth index/editor split.
  { id: 'maintenance', label: 'Maintenance' },
  { id: 'profiles', label: 'Profiles' },
]

export interface LocationsEntryOverrides {
  /** Forces Table (false) or Plan (true) instead of the remembered one; the
   * deck tab is still the last one used. */
  plan?: boolean
  /** The table's search and sort, which the Plan view has none of. */
  list?: InventoryList
}

/** Where a section's entry goes: its index, except Locations, which returns to
 * whichever of Table or Plan was last showing (the router remembers it). A
 * plain read, so the sidebar can use it without subscribing to the location.
 * This is the one place that builds the Locations Table/Plan route; `overrides`
 * apply to Locations only. */
export function inventorySectionRoute(
  router: AppRouter,
  section: InventorySectionId,
  overrides: LocationsEntryOverrides = {},
): InventoryRoute {
  if (section !== 'locations') return { view: 'index', section }
  const remembered = router.locationsIndex()
  const list = overrides.list ? { list: overrides.list } : {}
  return (overrides.plan ?? remembered.plan)
    ? { view: 'plan', deckId: remembered.deckId, ...list }
    : { view: 'index', section: 'locations', ...list }
}

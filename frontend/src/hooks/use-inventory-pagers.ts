import { useMemo } from 'react'

import type { PagerProps } from '@/components/patterns'
import { useEquipment, useInventoryDecks, useInventoryZones } from '@/hooks/use-inventory'
import { useLocationDeckLabel } from '@/hooks/use-location-deck-label'
import type { InventoryList } from '@/lib/app-location'
import { deckRows, equipmentFilterOf, orderDecks, orderEquipment, orderLocations } from '@/lib/index-order'
import { recordPager } from '@/lib/record-pager'

// The arrows on an Inventory record's page. Each reads the same list the
// index shows, from the same query, through the index's own ordering function
// (lib/index-order.ts), and steps through it. The arrows follow the list that
// Back returns to. Back from an Equipment record always lands on the Equipment
// index, so a record opened from a bin, Maintenance or Mate steps through that
// index with whatever filter it carried (none, from those places). Without
// `open` there is no pager: a New item draft, which Back doesn't return to a
// list position from.

const id = (record: { id: string }) => record.id

/** The Equipment record `recordId`'s neighbours in the Equipment index as filtered and sorted. */
export function useEquipmentPager(
  recordId: string | null,
  list: InventoryList | undefined,
  open: ((id: string) => void) | undefined,
): PagerProps | undefined {
  const enabled = recordId !== null && open !== undefined
  const { items } = useEquipment(enabled ? equipmentFilterOf(list) : null)
  return useMemo(
    () => (enabled ? recordPager(orderEquipment(items, list), recordId, id, (item) => item.name, open) : undefined),
    [enabled, items, list, recordId, open],
  )
}

/** A location's neighbours in the Locations table as searched and sorted. */
export function useLocationPager(
  recordId: string,
  list: InventoryList | undefined,
  open: ((id: string) => void) | undefined,
): PagerProps | undefined {
  const { zones } = useInventoryZones()
  const { deckLabel } = useLocationDeckLabel()
  return useMemo(
    () => (open ? recordPager(orderLocations(zones, list, deckLabel), recordId, id, (zone) => zone.name, open) : undefined),
    [open, zones, deckLabel, list, recordId],
  )
}

/** A deck's neighbours in the Decks table as sorted. */
export function useDeckPager(
  recordId: string,
  list: InventoryList | undefined,
  open: ((id: string) => void) | undefined,
): PagerProps | undefined {
  const { zones } = useInventoryZones()
  const { decks } = useInventoryDecks()
  return useMemo(
    () => (open ? recordPager(orderDecks(deckRows(decks, zones), list), recordId, id, (deck) => deck.name, open) : undefined),
    [open, decks, zones, list, recordId],
  )
}

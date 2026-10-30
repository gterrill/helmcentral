import { EQUIPMENT_SYSTEMS, type EquipmentFilter, type EquipmentItem, type InventoryDeck, type InventoryZone } from '@/hooks/use-inventory'
import type { InventoryList } from '@/lib/app-location'
import { documentDisplayName } from '@/lib/document-display'
import { orderRows, type ListSort } from '@/lib/list-order'
import type { WatchRecord } from '@/lib/watches'

// One ordering function per index. The index draws its rows in this order and
// the details page of a record asks the same function for the record's
// neighbours, so the arrows on the page step through exactly what the
// operator saw. Each takes the list as the server returned it plus the state
// the address carries (filter, search, sort) and returns the visible rows.

// ── Equipment ──────────────────────────────────────────────────────────────

export function equipmentLocationLabel(item: EquipmentItem): string {
  if (item.zone_name && item.bin_code) return `${item.zone_name} / ${item.bin_code}`
  if (item.zone_name) return item.zone_name
  return '--'
}

const EQUIPMENT_SORT = {
  name: (i: EquipmentItem) => i.name,
  manufacturer: (i: EquipmentItem) => [i.manufacturer, i.model].filter(Boolean).join(' ') || '--',
  location: equipmentLocationLabel,
  status: (i: EquipmentItem) => i.status,
  link_count: (i: EquipmentItem) => i.link_count,
}

/** The query the server is asked, from what the address says. */
export function equipmentFilterOf(list: InventoryList | undefined): EquipmentFilter {
  return {
    system: (list?.system ?? '') as EquipmentFilter['system'],
    status: (list?.status ?? '') as EquipmentFilter['status'],
    zone: list?.zone ?? '',
    q: list?.q ?? '',
  }
}

/** The rows the Equipment index draws: sorted by the column, then grouped by
 * system in enum order (a system outside the enum after it), as the table
 * draws its group headings. `items` are those the server returned for
 * `equipmentFilterOf(list)`. */
export function orderEquipment(items: readonly EquipmentItem[], list: InventoryList | undefined): EquipmentItem[] {
  const sorted = orderRows(items, list?.sort ?? null, EQUIPMENT_SORT)
  const known: readonly string[] = EQUIPMENT_SYSTEMS
  const unknown = [...new Set(sorted.map((i) => i.system).filter((s) => !known.includes(s)))]
  return [...known, ...unknown].flatMap((system) => sorted.filter((i) => i.system === system))
}

// ── Locations ──────────────────────────────────────────────────────────────

/** The Deck column's text for a location: the deck's name, a dash-free blank
 * for a location on no deck, and `?` when the decks never loaded (a blank would
 * read as "on no deck"). */
export function locationDeckLabel(decks: readonly InventoryDeck[], decksUnavailable: boolean): (zone: InventoryZone) => string {
  const names = new Map(decks.map((d) => [d.id, d.name]))
  return (zone) => (zone.deck_id ? names.get(zone.deck_id) ?? (decksUnavailable ? '?' : '') : '')
}

/** The rows the Locations table draws. `deckLabel` is the Deck column's text
 * for a location. With no sort they stay in the server's order. */
export function orderLocations(
  zones: readonly InventoryZone[],
  list: InventoryList | undefined,
  deckLabel: (zone: InventoryZone) => string,
): InventoryZone[] {
  const q = (list?.q ?? '').trim().toLowerCase()
  const shown = q === '' ? zones : zones.filter((z) => z.name.toLowerCase().includes(q))
  return orderRows(shown, list?.sort ?? null, {
    name: (z) => z.name,
    deck: deckLabel,
    bins: (z) => z.bins.length,
  })
}

// ── Decks ──────────────────────────────────────────────────────────────────

export interface DeckRow extends InventoryDeck {
  zoneCount: number
}

export function deckRows(decks: readonly InventoryDeck[], zones: readonly InventoryZone[]): DeckRow[] {
  return decks.map((d) => ({ ...d, zoneCount: zones.filter((z) => z.deck_id === d.id).length }))
}

export function orderDecks(rows: readonly DeckRow[], list: InventoryList | undefined): DeckRow[] {
  return orderRows(rows, list?.sort ?? null, {
    name: (d) => d.name,
    plan: (d) => (d.plan_document_id ? 'Yes' : 'No'),
    zoneCount: (d) => d.zoneCount,
  })
}

// ── Watches ────────────────────────────────────────────────────────────────

/** The Watches index sorts by name until the operator picks another column. */
export const WATCHES_DEFAULT_SORT: ListSort = { id: 'name', desc: false }

export function orderWatches(watches: readonly WatchRecord[], sort: ListSort | null | undefined): WatchRecord[] {
  return orderRows(watches, sort ?? WATCHES_DEFAULT_SORT, {
    name: (w) => w.name,
    schedule: (w) => w.schedule_sentence,
  })
}

// ── Documents ──────────────────────────────────────────────────────────────

interface SortableDocument {
  title: string
  filename: string
  created_at: string
  size_bytes?: number
}

/** The Documents listing sorts newest first until the operator picks another
 * column, and remembers the pick on this device. */
export const DOCUMENTS_DEFAULT_SORT: ListSort = { id: 'added', desc: true }
const DOCUMENTS_SORT_KEY = 'helmcentral.documents.sort'
const DOCUMENTS_SORT_COLUMNS = ['name', 'added', 'size']

export function loadDocumentsSort(): ListSort {
  try {
    const parsed: unknown = JSON.parse(localStorage.getItem(DOCUMENTS_SORT_KEY) ?? 'null')
    if (
      Array.isArray(parsed) && parsed.length === 1 &&
      typeof parsed[0]?.id === 'string' && DOCUMENTS_SORT_COLUMNS.includes(parsed[0].id) &&
      typeof parsed[0].desc === 'boolean'
    ) {
      return { id: parsed[0].id, desc: parsed[0].desc }
    }
  } catch {
    // Unavailable or unreadable storage: the default sort.
  }
  return DOCUMENTS_DEFAULT_SORT
}

export function saveDocumentsSort(sort: ListSort): void {
  try {
    localStorage.setItem(DOCUMENTS_SORT_KEY, JSON.stringify([sort]))
  } catch {
    // Best-effort only.
  }
}

export function orderDocuments<D extends SortableDocument>(docs: readonly D[], sort: ListSort): D[] {
  return orderRows(docs, sort, {
    name: (d) => documentDisplayName(d),
    added: (d) => Date.parse(d.created_at) || undefined,
    size: (d) => d.size_bytes,
  })
}

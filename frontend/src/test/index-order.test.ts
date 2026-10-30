import { describe, expect, it } from 'vitest'

import {
  deckRows,
  equipmentFilterOf,
  orderDecks,
  orderDocuments,
  orderEquipment,
  orderLocations,
  orderWatches,
} from '@/lib/index-order'
import { neighbours } from '@/lib/list-order'
import type { EquipmentItem, InventoryDeck, InventoryZone } from '@/hooks/use-inventory'
import type { WatchRecord } from '@/lib/watches'

const item = (id: string, name: string, system: string, extra: Partial<EquipmentItem> = {}) =>
  ({ id, name, system, manufacturer: '', model: '', status: 'deployed', zone_name: '', bin_code: '', link_count: 0, ...extra }) as EquipmentItem

describe('orderEquipment', () => {
  const items = [
    item('1', 'Windlass', 'anchoring'),
    item('2', 'Bilge pump', 'bilge'),
    item('3', 'Generator', 'electrical'),
    item('4', 'Engine', 'propulsion'),
    item('5', 'Alternator', 'electrical'),
    item('6', 'Mystery', 'zzz-new-system'),
  ]

  it('groups by system in enum order, a system outside the enum last, and keeps the server order within a group', () => {
    expect(orderEquipment(items, undefined).map((i) => i.id)).toEqual(['4', '3', '5', '2', '1', '6'])
  })

  it('sorts inside each group by the chosen column', () => {
    expect(orderEquipment(items, { sort: { id: 'name', desc: false } }).map((i) => i.id)).toEqual(['4', '5', '3', '2', '1', '6'])
    expect(orderEquipment(items, { sort: { id: 'name', desc: true } }).map((i) => i.id)).toEqual(['4', '3', '5', '2', '1', '6'])
  })

  it('gives the arrows the order the table shows', () => {
    const ordered = orderEquipment(items, { sort: { id: 'name', desc: false } })
    expect(neighbours(ordered, '3').previous?.id).toBe('5')
    expect(neighbours(ordered, '3').next?.id).toBe('2')
  })
})

describe('equipmentFilterOf', () => {
  it('is empty for no list, and carries each field of one', () => {
    expect(equipmentFilterOf(undefined)).toEqual({ system: '', status: '', zone: '', q: '' })
    expect(equipmentFilterOf({ system: 'bilge', status: 'stored', zone: 'z1', q: 'pump', sort: { id: 'name', desc: false } }))
      .toEqual({ system: 'bilge', status: 'stored', zone: 'z1', q: 'pump' })
  })
})

describe('orderLocations', () => {
  const zones = [
    { id: 'a', name: 'Salon', sort_index: 0, bins: [], deck_id: 'd2' },
    { id: 'b', name: 'Lazarette', sort_index: 1, bins: [{}, {}], deck_id: null },
    { id: 'c', name: 'Engine room', sort_index: 2, bins: [{}], deck_id: 'd1' },
  ] as unknown as InventoryZone[]
  const deckLabel = (z: InventoryZone) => ({ d1: 'Lower', d2: 'Main' })[z.deck_id ?? ''] ?? ''

  it('keeps the server order with no search or sort', () => {
    expect(orderLocations(zones, undefined, deckLabel).map((z) => z.id)).toEqual(['a', 'b', 'c'])
  })

  it('searches the name, ignoring case and surrounding space', () => {
    expect(orderLocations(zones, { q: ' ROOM ' }, deckLabel).map((z) => z.id)).toEqual(['c'])
  })

  it('sorts by name, deck and bin count', () => {
    expect(orderLocations(zones, { sort: { id: 'name', desc: false } }, deckLabel).map((z) => z.id)).toEqual(['c', 'b', 'a'])
    expect(orderLocations(zones, { sort: { id: 'deck', desc: false } }, deckLabel).map((z) => z.id)).toEqual(['b', 'c', 'a'])
    expect(orderLocations(zones, { sort: { id: 'bins', desc: true } }, deckLabel).map((z) => z.id)).toEqual(['b', 'c', 'a'])
  })
})

describe('orderDecks', () => {
  const decks = [
    { id: 'd1', name: 'Main', sort_index: 0, plan_document_id: 'x' },
    { id: 'd2', name: 'Lower', sort_index: 1, plan_document_id: null },
  ] as InventoryDeck[]
  const zones = [{ id: 'z', deck_id: 'd2' }, { id: 'y', deck_id: 'd2' }] as InventoryZone[]

  it('counts the locations on each deck and sorts by the column', () => {
    const rows = deckRows(decks, zones)
    expect(orderDecks(rows, undefined).map((d) => d.id)).toEqual(['d1', 'd2'])
    expect(orderDecks(rows, { sort: { id: 'name', desc: false } }).map((d) => d.id)).toEqual(['d2', 'd1'])
    expect(orderDecks(rows, { sort: { id: 'zoneCount', desc: true } }).map((d) => d.id)).toEqual(['d2', 'd1'])
  })
})

describe('orderWatches', () => {
  const w = (id: string, name: string, schedule_sentence = '') => ({ id, name, schedule_sentence }) as WatchRecord
  const watches = [w('1', 'Overnight', 'Every night'), w('2', 'Anchor', 'Hourly'), w('3', 'Morning', 'Daily')]

  it('sorts by name when no sort is given', () => {
    expect(orderWatches(watches, null).map((x) => x.id)).toEqual(['2', '3', '1'])
  })

  it('sorts by the chosen column and direction', () => {
    expect(orderWatches(watches, { id: 'name', desc: true }).map((x) => x.id)).toEqual(['1', '3', '2'])
    expect(orderWatches(watches, { id: 'schedule', desc: false }).map((x) => x.id)).toEqual(['3', '1', '2'])
  })
})

describe('orderDocuments', () => {
  const d = (id: string, title: string, created_at: string, size_bytes?: number) =>
    ({ id, title, filename: `${id}.pdf`, created_at, size_bytes })
  const docs = [d('a', 'Manual', '2026-01-01T00:00:00Z', 30), d('b', '', '2026-03-01T00:00:00Z', 10), d('c', 'Chart', '2026-02-01T00:00:00Z')]

  it('sorts by name (the filename when there is no title), added and size', () => {
    expect(orderDocuments(docs, { id: 'name', desc: false }).map((x) => x.id)).toEqual(['b', 'c', 'a'])
    expect(orderDocuments(docs, { id: 'added', desc: true }).map((x) => x.id)).toEqual(['b', 'c', 'a'])
    expect(orderDocuments(docs, { id: 'size', desc: false }).map((x) => x.id)).toEqual(['b', 'a', 'c'])
  })
})

import { describe, expect, it } from 'vitest'
import {
  insertVertex,
  layoutFromZones,
  layoutToPayload,
  moveVertex,
  removeVertex,
  sameLayout,
  withoutZone,
} from '@/lib/deck-layout'
import type { InventoryZone } from '@/hooks/use-inventory'

const tri: Array<[number, number]> = [[0.1, 0.1], [0.5, 0.1], [0.3, 0.5]]

const zones: InventoryZone[] = [
  {
    id: 'z1', name: 'Engine room', sort_index: 0, deck_id: 'd1', polygon: tri,
    bins: [
      { id: 'b1', zone_id: 'z1', code: 'E1', name: '', sort_index: 0, pin: { x: 0.3, y: 0.2 } },
      { id: 'b2', zone_id: 'z1', code: 'E2', name: '', sort_index: 1, pin: null },
    ],
  },
  { id: 'z2', name: 'Salon', sort_index: 1, deck_id: 'd2', polygon: tri, bins: [{ id: 'b3', zone_id: 'z2', code: 'S1', name: '', sort_index: 0, pin: { x: 0.2, y: 0.2 } }] },
  { id: 'z3', name: 'Lazarette', sort_index: 2, deck_id: null, polygon: null, bins: [] },
]

describe('layoutFromZones', () => {
  it('takes the outlines and pins of the zones on this deck only', () => {
    expect(layoutFromZones(zones, 'd1')).toEqual({ polygons: { z1: tri }, pins: { b1: { x: 0.3, y: 0.2 } } })
  })
})

describe('layoutToPayload', () => {
  it('lists every outline and pin', () => {
    expect(layoutToPayload({ polygons: { z1: tri }, pins: { b1: { x: 0.3, y: 0.2 } } })).toEqual({
      zones: [{ id: 'z1', polygon: tri }],
      bins: [{ id: 'b1', x: 0.3, y: 0.2 }],
    })
  })

  it('leaves out the pin of a bin whose location has no outline in the draft', () => {
    const payload = layoutToPayload({ polygons: {}, pins: { b1: { x: 0.3, y: 0.2 } } }, zones)
    expect(payload.bins).toEqual([])
  })
})

describe('sameLayout', () => {
  it('compares outlines and pins by value', () => {
    const a = layoutFromZones(zones, 'd1')
    expect(sameLayout(a, layoutFromZones(zones, 'd1'))).toBe(true)
    expect(sameLayout(a, { ...a, pins: {} })).toBe(false)
    expect(sameLayout(a, { ...a, polygons: { z1: moveVertex(tri, 0, [0.2, 0.2]) } })).toBe(false)
  })
})

describe('vertex edits', () => {
  it('moves a vertex without touching the original', () => {
    const moved = moveVertex(tri, 1, [0.6, 0.2])
    expect(moved[1]).toEqual([0.6, 0.2])
    expect(tri[1]).toEqual([0.5, 0.1])
  })

  it('clamps a moved vertex to the plan', () => {
    expect(moveVertex(tri, 0, [-0.2, 1.4])[0]).toEqual([0, 1])
  })

  it('inserts a vertex after an index', () => {
    const next = insertVertex(tri, 0, [0.3, 0.1])
    expect(next).toHaveLength(4)
    expect(next[1]).toEqual([0.3, 0.1])
  })

  it('refuses to insert beyond 64 points', () => {
    const many = Array.from({ length: 64 }, (_, i) => [i / 100, 0.5] as [number, number])
    expect(insertVertex(many, 0, [0.5, 0.5])).toBe(many)
  })

  it('removes a vertex but never below three', () => {
    const four = insertVertex(tri, 0, [0.3, 0.1])
    expect(removeVertex(four, 1)).toHaveLength(3)
    expect(removeVertex(tri, 1)).toBe(tri)
  })
})

describe('withoutZone', () => {
  it('drops a zone outline and the pins of its bins', () => {
    const layout = layoutFromZones(zones, 'd1')
    expect(withoutZone(layout, zones[0])).toEqual({ polygons: {}, pins: {} })
  })
})

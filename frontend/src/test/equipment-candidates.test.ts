import { describe, expect, it } from 'vitest'

import { candidateItems, candidateLabels } from '@/lib/equipment-candidates'
import type { TileItem } from '@/lib/equipment-tiles'
import type { EngineProfile } from '@/lib/engine-profiles'

const item = (over: Partial<TileItem> & { id: string; name: string }): TileItem => ({
  signalk_source: '', source_kind: '', profile_id: '', system: 'other', status: 'deployed', quantity: 1,
  required_quantity: null, part_number: '', ...over,
})

const profiles: EngineProfile[] = [{ id: 'cummins', name: 'Cummins QSB 6.7', kind: 'engine', gauges: [] }]

describe('candidateItems', () => {
  const inventory: TileItem[] = [
    item({ id: 'e1', name: 'Cummins QSB 6.7', system: 'propulsion' }),
    item({ id: 'e2', name: 'Cummins QSB 6.7', system: 'propulsion' }),
    item({ id: 'e3', name: 'Odd engine', system: 'other', profile_id: 'cummins' }),
    item({ id: 'saw', name: 'Hacksaw', system: 'other', quantity: 1 }),
    item({ id: 'rags', name: 'Rags', system: 'propulsion', quantity: 12 }),
    item({ id: 'filters', name: 'Fuel filter spares', system: 'propulsion', required_quantity: 4, quantity: 2 }),
    item({ id: 'part', name: 'Impeller', system: 'propulsion', part_number: 'J-209' }),
    item({ id: 'locker', name: 'Spare alternator', system: 'electrical', status: 'stored' }),
    item({ id: 'bank', name: 'House bank', system: 'electrical' }),
    item({ id: 'tank', name: 'Day tank', system: 'fuel' }),
    item({ id: 'h2o', name: 'Header tank', system: 'water' }),
    item({ id: 'mapped', name: 'Mapped engine', system: 'propulsion', signalk_source: 'propulsion.port', source_kind: 'engine' }),
  ]
  const ids = (kind: Parameters<typeof candidateItems>[0]) => candidateItems(kind, inventory, profiles).map((i) => i.id)

  it('offers an engine only things that could be one: propulsion, or a profile of kind engine', () => {
    expect(ids('engine')).toEqual(['e1', 'e2', 'e3'])
  })

  it('leaves out tools, consumables, spares and anything stored', () => {
    const all = ['engine', 'battery', 'tank', 'alternator', 'generator'] as const
    for (const kind of all) {
      for (const excluded of ['saw', 'rags', 'filters', 'part', 'locker']) expect(ids(kind)).not.toContain(excluded)
    }
  })

  it('offers electrical items for the electrical kinds', () => {
    for (const kind of ['battery', 'alternator', 'solar', 'charger', 'inverter', 'generator'] as const) {
      expect(ids(kind)).toEqual(['bank'])
    }
  })

  it('offers fuel and water items for tanks', () => {
    expect(ids('tank')).toEqual(['tank', 'h2o'])
  })

  it('never offers an item that already owns a source', () => {
    expect(ids('engine')).not.toContain('mapped')
  })
})

describe('candidateLabels', () => {
  it('tells two items with one name apart by location, then serial', () => {
    const a = item({ id: 'a', name: 'Cummins QSB 6.7', serial: '1001', zone_name: 'Engine room' })
    const b = item({ id: 'b', name: 'Cummins QSB 6.7', serial: '1002', zone_name: 'Engine room' })
    const labels = candidateLabels([a, b])
    expect(labels.get('a')).toBe('Cummins QSB 6.7 · Engine room · serial 1001')
    expect(labels.get('b')).toBe('Cummins QSB 6.7 · Engine room · serial 1002')
  })

  it('numbers items that have nothing to tell them apart', () => {
    const labels = candidateLabels([item({ id: 'a', name: 'Cummins' }), item({ id: 'b', name: 'Cummins' })])
    expect(labels.get('a')).toBe('Cummins (1 of 2)')
    expect(labels.get('b')).toBe('Cummins (2 of 2)')
  })

  it('leaves a name that is alone as it is', () => {
    expect(candidateLabels([item({ id: 'a', name: 'Port engine' })]).get('a')).toBe('Port engine')
  })
})

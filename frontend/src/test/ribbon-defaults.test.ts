import { describe, expect, it } from 'vitest'

import { suggestRibbonLamps } from '@/lib/ribbon-defaults'
import type { TileItem } from '@/lib/equipment-tiles'

const item = (id: string, name: string, source: string, kind: TileItem['source_kind']): TileItem => ({
  id, name, signalk_source: source, source_kind: kind, profile_id: '',
})

const boat: TileItem[] = [
  item('alt-1', 'Starboard alternator', 'electrical.alternator.1', 'alternator'),
  item('eng-s', 'Starboard engine', 'propulsion.starboard', 'engine'),
  item('eng-p', 'Port engine', 'propulsion.port', 'engine'),
  item('gen', 'Generator', 'electrical.generator.0', 'generator'),
  item('alt-0', 'Port alternator', 'electrical.alternator.0', 'alternator'),
  item('inv', 'Inverter', 'electrical.inverters.0', 'inverter'),
]

describe('suggestRibbonLamps', () => {
  it('suggests one lamp per mapped item, referencing the item, never a path', () => {
    const lamps = suggestRibbonLamps(boat)
    expect(lamps.map((l) => l.equipmentId)).toEqual(['eng-p', 'eng-s', 'gen', 'inv', 'alt-0', 'alt-1'])
    expect(lamps.every((l) => l.path === '')).toBe(true)
    expect(lamps[0]).toMatchObject({ suffix: 'revolutions', group: 'Propulsion', icon: 'engine' })
  })

  it('labels each lamp with the item name, cut to the lamp label limit', () => {
    const lamps = suggestRibbonLamps(boat)
    expect(lamps[0].label).toBe('Port engine')
    expect(lamps[1].label).toBe('Starboard en')
  })

  it('gives an item with no source no lamp', () => {
    expect(suggestRibbonLamps([item('x', 'Spare engine', '', '')])).toEqual([])
  })

  it('suggests nothing for a boat with nothing mapped', () => {
    expect(suggestRibbonLamps([])).toEqual([])
  })

  it('leaves out kinds a ribbon has no running signal for', () => {
    expect(suggestRibbonLamps([item('b', 'House bank', 'electrical.batteries.1', 'battery')])).toEqual([])
  })
})

import { describe, expect, it } from 'vitest'

import { describeDependants, hasDependants, type EquipmentDependants } from '@/lib/equipment-dependants'

const none: EquipmentDependants = { item_id: 'a', tiles: [], roles: [], alarm_rules: [], features: [] }

describe('describeDependants', () => {
  it('says nothing for an item nothing uses', () => {
    expect(hasDependants(none)).toBe(false)
    expect(describeDependants(none)).toBe('')
  })

  it('names tiles by page and title, features in plain words, roles and rule counts', () => {
    const used: EquipmentDependants = {
      item_id: 'a',
      tiles: [
        { page_id: 'p1', page_name: 'Underway', tile_id: 'cluster:1', title: 'Port' },
        { page_id: 'p2', page_name: 'Wall', tile_id: 'gauge-group:2', title: 'Engines: Port' },
      ],
      roles: [{ role: 'house_bank', label: 'House bank' }],
      alarm_rules: [
        { id: '1', label: 'a', kind: 'state_of_charge' },
        { id: '2', label: 'b', kind: 'state_of_charge' },
        { id: '3', label: 'c', kind: 'state_of_charge' },
        { id: '4', label: 'd', kind: 'zone' },
      ],
      features: ['Anchor auto-raise', 'GNSS position jump check', "Mate's passage fuel estimate"],
    }
    expect(describeDependants(used)).toBe(
      "Used by 2 tiles (Underway: Port, Wall: Engines: Port), anchor auto-raise, GNSS position jump check, Mate's passage fuel estimate, the House bank role, 3 state-of-charge alarm rules, 1 gauge limit alarm.",
    )
  })

  it('names the ribbon as itself and uses the singular for one tile', () => {
    const ribbon: EquipmentDependants = {
      ...none,
      tiles: [{ page_id: '', page_name: 'Indicator ribbon', tile_id: 'ribbon', title: 'Lamps' }],
    }
    expect(describeDependants(ribbon)).toBe('Used by 1 tile (Indicator ribbon).')
  })
})

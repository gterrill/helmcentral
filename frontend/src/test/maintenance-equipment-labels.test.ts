import { describe, expect, it } from 'vitest'
import { equipmentLabeller, joinEquipmentLabel, labelEquipmentList } from '@/lib/maintenance-equipment-labels'

describe('labelEquipmentList', () => {
  it('labels same-named items by alias, else location, and leaves unique ones alone', () => {
    const labels = labelEquipmentList([
      { id: 'p', name: 'Cummins QSB 6.7', alias: null, location: 'ER - Port' },
      { id: 's', name: 'Cummins QSB 6.7', alias: 'Starboard engine', location: 'ER - Starboard' },
      { id: 'g', name: 'Genset', alias: null, location: 'ER' },
    ])
    expect(labels.get('p')).toEqual({ name: 'Cummins QSB 6.7', detail: 'ER - Port' })
    expect(labels.get('s')).toEqual({ name: 'Cummins QSB 6.7', detail: 'Starboard engine' })
    expect(labels.get('g')).toEqual({ name: 'Genset', detail: null })
  })
})

const row = (equipment_id: string | null, equipment_name: string, equipment_alias: string | null = null, equipment_location: string | null = null) =>
  ({ equipment_id, equipment_name, equipment_alias, equipment_location })

describe('equipmentLabeller', () => {
  it('leaves a unique name alone', () => {
    const a = row('a', 'Watermaker', null, 'Lazarette')
    const b = row('b', 'Main engine')
    const label = equipmentLabeller([a, b])
    expect(label(a)).toEqual({ name: 'Watermaker', detail: null })
    expect(label(b)).toEqual({ name: 'Main engine', detail: null })
  })

  it('adds the location when two items share a name', () => {
    const p = row('p', 'Cummins QSB 6.7', null, 'ER - Port')
    const s = row('s', 'Cummins QSB 6.7', null, 'ER - Starboard')
    const label = equipmentLabeller([p, s])
    expect(label(p)).toEqual({ name: 'Cummins QSB 6.7', detail: 'ER - Port' })
    expect(label(s)).toEqual({ name: 'Cummins QSB 6.7', detail: 'ER - Starboard' })
    expect(joinEquipmentLabel(label(p))).toBe('Cummins QSB 6.7 (ER - Port)')
  })

  it('prefers the alias over the location', () => {
    const p = row('p', 'Cummins QSB 6.7', 'Port engine', 'ER - Port')
    const s = row('s', 'Cummins QSB 6.7', null, 'ER - Starboard')
    const label = equipmentLabeller([p, s])
    expect(label(p).detail).toBe('Port engine')
    expect(label(s).detail).toBe('ER - Starboard')
  })

  it('leaves the name alone when there is neither alias nor location', () => {
    const p = row('p', 'Pump')
    const s = row('s', 'Pump', null, 'Bilge')
    const label = equipmentLabeller([p, s])
    expect(joinEquipmentLabel(label(p))).toBe('Pump')
    expect(joinEquipmentLabel(label(s))).toBe('Pump (Bilge)')
  })

  it('does not treat many rules of one item as a clash', () => {
    const a = row('p', 'Engine', null, 'ER')
    const b = row('p', 'Engine', null, 'ER')
    expect(equipmentLabeller([a, b])(a).detail).toBeNull()
  })

  it('counts names that differ only in case or spacing as a clash, and shows them unchanged', () => {
    const p = row('p', 'Cummins QSB 6.7', null, 'ER - Port')
    const s = row('s', 'cummins  QSB 6.7 ', null, 'ER - Starboard')
    const label = equipmentLabeller([p, s])
    expect(label(p)).toEqual({ name: 'Cummins QSB 6.7', detail: 'ER - Port' })
    expect(label(s)).toEqual({ name: 'cummins  QSB 6.7 ', detail: 'ER - Starboard' })
  })

  it('gives a rule with no equipment its bare name', () => {
    const r = row(null, '')
    expect(equipmentLabeller([r])(r)).toEqual({ name: '', detail: null })
  })
})

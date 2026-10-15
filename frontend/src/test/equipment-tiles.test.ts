import { describe, expect, it } from 'vitest'

import type { EngineClusterConfig, GaugeGroupWidgetConfig, LampStripWidgetConfig } from '@/lib/dashboard-widgets'
import type { EngineProfile } from '@/lib/engine-profiles'
import {
  bandsToProfileZones,
  effectiveScale,
  resolveCluster,
  resolveGaugeGroup,
  resolveLamps,
  withGaugeZones,
  type EquipmentTileContext,
  type TileItem,
} from '@/lib/equipment-tiles'

const yanmar: EngineProfile = {
  id: 'yanmar-4jh', name: 'Yanmar 4JH', kind: 'engine',
  gauges: [
    {
      path_suffix: 'oilPressure', label: 'Oil Press', display: 'radial', quantity: 'pressure', unit: 'psi', min: 0, max: 100,
      zones: [
        { direction: 'below', threshold: 15, state: 'alarm' },
        { direction: 'above', threshold: 40, state: 'normal' },
        { direction: 'above', threshold: null, state: 'warn' },
      ],
    },
  ],
}

const item = (over: Partial<TileItem> = {}): TileItem => ({
  id: 'eng-port', name: 'Port engine', signalk_source: 'propulsion.port', source_kind: 'engine', profile_id: 'yanmar-4jh', ...over,
})

const ctx = (items: TileItem[], over: Partial<EquipmentTileContext> = {}): EquipmentTileContext => ({
  items, profiles: [yanmar], reporting: null, ...over,
})

const slot = (suffix: string, extra = {}) => ({ path: '', suffix, label: suffix, display: 'numeric' as const, quantity: 'raw', unit: 'raw', ...extra })

const cluster: EngineClusterConfig = {
  equipmentId: 'eng-port',
  title: '',
  ring: slot('revolutions'),
  centre: slot('runTime'),
  corners: [{ label: 'Oil', rows: [slot('oilPressure', { min: 0, max: 100 })] }],
  fuel: { side: 'left', bars: [{ tankId: 'tank-5', label: 'Fwd' }, { label: 'Aft' }] },
}

describe('resolveCluster', () => {
  const tank = item({ id: 'tank-5', name: 'Port forward fuel', signalk_source: 'tanks.fuel.5', source_kind: 'tank', profile_id: '' })

  it('builds every path from the item source and the slot suffix', () => {
    const r = resolveCluster(cluster, ctx([item(), tank]))
    if (!r.ok) throw new Error('expected a resolved cluster')
    expect(r.config.ring.path).toBe('propulsion.port.revolutions')
    expect(r.config.corners[0].rows[0].path).toBe('propulsion.port.oilPressure')
    expect(r.config.title).toBe('Port engine')
  })

  it('takes zones from the profile and drops slot thresholds that are not set', () => {
    const r = resolveCluster(cluster, ctx([item(), tank]))
    if (!r.ok) throw new Error('expected a resolved cluster')
    expect(r.config.corners[0].rows[0].zones).toEqual([
      { from: 0, to: 15, state: 'alarm' },
      { from: 40, to: 100, state: 'normal' },
    ])
    expect(r.config.ring.zones).toBeUndefined()
  })

  it('follows the profile live: a different profile gives different zones', () => {
    const edited: EngineProfile = { ...yanmar, gauges: [{ ...yanmar.gauges![0], zones: [{ direction: 'below', threshold: 25, state: 'warn' }] }] }
    const r = resolveCluster(cluster, ctx([item(), tank], { profiles: [edited] }))
    if (!r.ok) throw new Error('expected a resolved cluster')
    expect(r.config.corners[0].rows[0].zones).toEqual([{ from: 0, to: 25, state: 'warn' }])
  })

  it('moves to another source when the item is mapped elsewhere', () => {
    const r = resolveCluster(cluster, ctx([item({ signalk_source: 'propulsion.0' }), tank]))
    if (!r.ok) throw new Error('expected a resolved cluster')
    expect(r.config.ring.path).toBe('propulsion.0.revolutions')
  })

  it('keeps an explicit title over the item name', () => {
    const r = resolveCluster({ ...cluster, title: 'Starboard' }, ctx([item(), tank]))
    expect(r.ok && r.config.title).toBe('Starboard')
  })

  it('resolves fuel bars to the tank item source, and leaves an unpicked bar without a path', () => {
    const r = resolveCluster(cluster, ctx([item(), tank]))
    if (!r.ok || !r.config.fuel) throw new Error('expected a rail')
    expect(r.config.fuel.bars[0].level).toMatchObject({ path: 'tanks.fuel.5.currentLevel', quantity: 'ratio', unit: 'percent', label: 'Fwd' })
    expect(r.config.fuel.bars[0].capacity).toMatchObject({ path: 'tanks.fuel.5.capacity', quantity: 'volume', unit: 'L' })
    expect(r.config.fuel.bars[1].level.path).toBe('')
  })

  it('asks for equipment when none is picked', () => {
    expect(resolveCluster({ ...cluster, equipmentId: undefined }, ctx([item()]))).toEqual({ ok: false, problem: { kind: 'pick' } })
  })

  it('says so when the item has been deleted', () => {
    expect(resolveCluster(cluster, ctx([]))).toEqual({ ok: false, problem: { kind: 'missing' } })
  })

  it('says Not mapped for an item with no source, with the item to link to', () => {
    const r = resolveCluster(cluster, ctx([item({ signalk_source: '', source_kind: '' })]))
    expect(r.ok).toBe(false)
    expect(!r.ok && r.problem.kind).toBe('not-mapped')
    expect(!r.ok && 'item' in r.problem && r.problem.item.id).toBe('eng-port')
  })

  it('says Not reporting when discovery does not list the source', () => {
    const r = resolveCluster(cluster, ctx([item()], { reporting: new Set(['propulsion.starboard']) }))
    expect(!r.ok && r.problem.kind).toBe('not-reporting')
  })

  it('shows an engine that is off rather than calling it silent', () => {
    const r = resolveCluster(cluster, ctx([item(), tank], { reporting: new Set(), engines: { port: { state: 'off' } } }))
    expect(r.ok).toBe(true)
  })

  it('does not claim silence before discovery has answered', () => {
    expect(resolveCluster(cluster, ctx([item(), tank], { reporting: null })).ok).toBe(true)
  })
})

describe('resolveGaugeGroup', () => {
  const group: GaugeGroupWidgetConfig = {
    equipmentKind: 'engine', equipmentId: 'eng-port', title: '',
    gauges: [slot('oilPressure', { min: 0, max: 100 })],
  }
  it('resolves paths and zones like a cluster', () => {
    const r = resolveGaugeGroup(group, ctx([item()]))
    if (!r.ok) throw new Error('expected resolved')
    expect(r.config.gauges[0].path).toBe('propulsion.port.oilPressure')
    expect(r.config.gauges[0].zones).toHaveLength(2)
    expect(r.config.title).toBe('Port engine')
  })
  it('asks for equipment when none is picked', () => {
    expect(resolveGaugeGroup({ ...group, equipmentId: undefined }, ctx([item()])).ok).toBe(false)
  })
})

describe('resolveLamps', () => {
  const strip: LampStripWidgetConfig = {
    title: 'Indicators',
    lamps: [
      { path: '', equipmentId: 'eng-port', suffix: 'revolutions', label: 'Port' },
      { path: '', equipmentId: 'gone', suffix: 'revolutions', label: 'Gone' },
      { path: 'electrical.switches.bilge', label: 'Bilge' },
    ],
  }
  it('fills equipment lamps and leaves path lamps alone', () => {
    const r = resolveLamps(strip, ctx([item()]))
    expect(r.lamps.map((l) => l.path)).toEqual(['propulsion.port.revolutions', '', 'electrical.switches.bilge'])
  })
})

describe('profile zone editing', () => {
  it('stores a band the way a profile holds it, and refuses a mid-scale band', () => {
    expect(bandsToProfileZones([
      { from: 0, to: 15, state: 'alarm' },
      { from: 90, to: 100, state: 'warn' },
      { from: 40, to: 60, state: 'warn' },
    ], 0, 100)).toEqual([
      { direction: 'below', threshold: 15, state: 'alarm' },
      { direction: 'above', threshold: 90, state: 'warn' },
    ])
  })

  it('replaces the edited gauge zones and keeps manufacturer slots', () => {
    const next = withGaugeZones(yanmar, 'oilPressure', [{ from: 0, to: 20, state: 'warn' }], 0, 100)
    expect(next.gauges![0].zones).toEqual([
      { direction: 'above', threshold: null, state: 'warn' },
      { direction: 'below', threshold: 20, state: 'warn' },
    ])
  })
})

describe('the scale and unit of an equipment slot', () => {
  const bar: EngineProfile = {
    id: 'bar-engine', name: 'Bar engine', kind: 'engine',
    gauges: [{
      path_suffix: 'oilPressure', label: 'Oil Press', display: 'radial', quantity: 'pressure', unit: 'bar', min: 0, max: 10,
      zones: [{ direction: 'above', threshold: 8, state: 'alarm' }],
    }],
  }
  const tile: EngineClusterConfig = {
    equipmentId: 'eng-port', title: '',
    ring: slot('oilPressure', { quantity: 'pressure', unit: 'psi' }),
    centre: slot('runTime'),
    corners: [],
  }

  it('carries the profile scale its zones were anchored to when the slot sets none', () => {
    const r = resolveCluster(tile, ctx([item({ profile_id: 'bar-engine' })], { profiles: [bar] }))
    if (!r.ok) throw new Error('expected resolved')
    expect(r.config.ring).toMatchObject({ min: 0, max: 10 })
    expect(r.config.ring.zones).toEqual([{ from: 8, to: 10, state: 'alarm' }])
  })

  it('takes the quantity and unit from the profile gauge, because its thresholds are in that unit', () => {
    const r = resolveCluster(tile, ctx([item({ profile_id: 'bar-engine' })], { profiles: [bar] }))
    if (!r.ok) throw new Error('expected resolved')
    expect(r.config.ring).toMatchObject({ quantity: 'pressure', unit: 'bar' })
  })

  it('keeps the slot own quantity and unit for a reading the profile does not have', () => {
    const r = resolveCluster(tile, ctx([item({ profile_id: 'bar-engine' })], { profiles: [bar] }))
    if (!r.ok) throw new Error('expected resolved')
    expect(r.config.centre).toMatchObject({ quantity: 'raw', unit: 'raw' })
  })

  it('has one rule for the scale: the slot, else the gauge, else 0 to 100', () => {
    expect(effectiveScale(2, 4, { min: 0, max: 10 })).toEqual({ min: 2, max: 4 })
    expect(effectiveScale(undefined, undefined, { min: 1, max: 10 })).toEqual({ min: 1, max: 10 })
    expect(effectiveScale(undefined, undefined, undefined)).toEqual({ min: 0, max: 100 })
  })
})

describe('fuel bar zones', () => {
  const tank = item({ id: 'tank-5', name: 'Port forward fuel', signalk_source: 'tanks.fuel.5', source_kind: 'tank', profile_id: '' })
  it('go onto the level slot, because a tank has no profile to hold them', () => {
    const r = resolveCluster(
      { ...cluster, fuel: { side: 'left', bars: [{ tankId: 'tank-5', label: 'Fwd', zones: [{ from: 0, to: 15, state: 'warn' }] }] } },
      ctx([item(), tank]),
    )
    if (!r.ok || !r.config.fuel) throw new Error('expected a rail')
    expect(r.config.fuel.bars[0].level.zones).toEqual([{ from: 0, to: 15, state: 'warn' }])
  })
})

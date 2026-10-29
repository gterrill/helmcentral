import { describe, expect, it } from 'vitest'

import { decodeMateCards } from '@/lib/mate-cards'

import { tileCard, tilePayload } from './helpers/tile-card-fixture'

const decode = (over: Record<string, unknown>, snap: Record<string, unknown> = {}) =>
  decodeMateCards([{ kind: 'tile', payload: tilePayload(over, snap) }])[0]

describe('decoder: tile', () => {
  it('reads a depth-tide card into the figures the tile takes', () => {
    const c = tileCard()
    expect(c).toMatchObject({ kind: 'tile', tile: 'depth-tide', capturedAt: '2026-10-28T04:03:00Z' })
    expect(c.snapshot).toMatchObject({ depth: 3.3, depthLastUpdateAgeS: 1.2, navigationState: 'anchored' })
    expect(c.snapshot.tide).toMatchObject({ current_tide_height_ft: 6.56, tide_direction: 'Rising', station_name: 'Cape Upstart' })
    expect(c.snapshot.depthTrend).toMatchObject({ since: 'tide', tideType: 'low', tideDepthM: 3.1 })
    expect(c.snapshot.depthTrend.points).toHaveLength(3)
  })

  it('accepts a trend with no points yet', () => {
    const c = tileCard({}, { depth_trend: { points: [], since: 'window' } })
    expect(c.snapshot.depthTrend).toEqual({ points: [], since: 'window' })
  })

  it('keeps a missing reading as null', () => {
    const c = tileCard({}, { depth_m: null, depth_age_s: null, navigation_state: null, tide: null })
    expect(c.snapshot).toMatchObject({ depth: null, depthLastUpdateAgeS: null, navigationState: null, tide: null })
  })

  it('accepts a tide with no station or provider, as the server omits them when empty', () => {
    const base = tilePayload().snapshot as { tide: Record<string, unknown> }
    const tide = { ...base.tide }
    delete tide.station_name
    delete tide.provider
    expect(tileCard({}, { tide }).snapshot.tide).toMatchObject({ station_name: '', provider: '' })
  })

  it.each([
    ['a tile Mate cannot show', { tile: 'generator' }, {}, /tile: expected one of depth-tide/],
    ['a captured time that is not a time', { captured_at: 'noon' }, {}, /captured_at: expected a timestamp/],
    ['a missing snapshot', { snapshot: undefined }, {}, /snapshot: expected an object/],
    ['a negative depth', {}, { depth_m: -1 }, /snapshot.depth_m/],
    ['a depth that is a string', {}, { depth_m: '3.3' }, /snapshot.depth_m/],
    ['an absent depth field', {}, { depth_m: undefined }, /snapshot.depth_m/],
    ['a tide with no direction', {}, { tide: { datetime: '2026-10-28T04:03:00Z' } }, /snapshot.tide/],
    ['a trend with no points', {}, { depth_trend: { since: 'window' } }, /snapshot.depth_trend.points/],
    ['a trend point with no depth', {}, { depth_trend: { points: [{ time: '2026-10-28T03:00:00Z' }], since: 'window' } }, /depth_m/],
  ])('turns %s into an error card, not a thrown error', (_name, over, snap, reason) => {
    const c = decode(over, snap)
    expect(c.kind).toBe('error')
    expect(c.kind === 'error' && c.rawKind).toBe('tile')
    expect(c.kind === 'error' && c.reason).toMatch(reason)
  })
})

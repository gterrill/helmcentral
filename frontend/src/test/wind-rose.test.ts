import { describe, expect, it } from 'vitest'

import { decodeMateCards, type WindRoseCard } from '@/lib/mate-cards'
import { polar, ringTicks, windRoseModel, wedgePath } from '@/lib/wind-rose'

import { windRosePayload } from './helpers/wind-rose-fixture'

function card(over: Record<string, unknown> = {}): WindRoseCard {
  const [c] = decodeMateCards([{ kind: 'wind_rose', payload: windRosePayload(over) }])
  if (c.kind !== 'wind_rose') throw new Error(`did not decode: ${JSON.stringify(c)}`)
  return c
}

describe('windRoseModel', () => {
  it('turns counts into percentages of every sample, calm and variable included', () => {
    const m = windRoseModel(card())
    // 100 samples: 10 calm, 5 variable, 85 in sectors.
    expect(m.calmPct).toBeCloseTo(10)
    expect(m.variablePct).toBeCloseTo(5)
    expect(m.sectors).toHaveLength(16)
    expect(m.sectors[4].pct).toBeCloseTo(60)
    expect(m.sectors[4].bands.map((b) => b.pct)).toEqual([0, 20, 30, 10, 0, 0])
    expect(m.sectors[8].pct).toBeCloseTo(25)
  })

  it('stacks bands outward: each ends where the sector total so far ends', () => {
    const m = windRoseModel(card())
    const s = m.sectors[4]
    expect(s.bands.map((b) => b.to)).toEqual([0, 20, 50, 60, 60, 60])
    expect(s.bands[2].from).toBe(20)
  })

  it('centres sector i on i times the sector width, north first', () => {
    const m = windRoseModel(card())
    expect(m.sectors[0].centre).toBe(0)
    expect(m.sectors[4].centre).toBe(90)
    expect(m.sectors[4].compass).toBe('E')
    expect(m.sectors[8].compass).toBe('S')
  })

  it('names the prevailing sector and its share', () => {
    const m = windRoseModel(card())
    expect(m.prevailing).toMatchObject({ compass: 'E', pct: 60 })
  })

  it('has no prevailing direction when every sample was calm or variable', () => {
    const empty = Array.from({ length: 16 }, () => [0, 0, 0, 0, 0, 0])
    const m = windRoseModel(card({ counts: empty, samples: 10, calm: 6, variable: 4 }))
    expect(m.prevailing).toBeNull()
    expect(m.maxPct).toBe(0)
  })

  it('labels each speed band, the last one open-ended', () => {
    const m = windRoseModel(card())
    expect(m.bandLabels).toEqual(['0–5 kn', '5–10 kn', '10–15 kn', '15–20 kn', '20–25 kn', '25+ kn'])
  })

  it('names sectors with the nearest of the sixteen compass points', () => {
    const counts = Array.from({ length: 8 }, () => [0, 0, 0, 0, 0, 0])
    counts[1][0] = 4
    const m = windRoseModel(card({ sectors: 8, counts, samples: 4, calm: 0, variable: 0 }))
    expect(m.sectors[1].compass).toBe('NE')
  })
})

describe('ringTicks', () => {
  it('picks round percentages up to the largest sector', () => {
    expect(ringTicks(0)).toEqual([])
    const t = ringTicks(23)
    expect(t[t.length - 1]).toBeGreaterThanOrEqual(23)
    expect(t.every((v) => v > 0)).toBe(true)
  })
})

describe('polar and wedgePath', () => {
  it('measures a bearing clockwise from north', () => {
    const north = polar(100, 100, 50, 0)
    expect(north.x).toBeCloseTo(100)
    expect(north.y).toBeCloseTo(50)
    const east = polar(100, 100, 50, 90)
    expect(east.x).toBeCloseTo(150)
    expect(east.y).toBeCloseTo(100)
  })

  it('draws a wedge as two arcs and two radial edges, and closes it', () => {
    const d = wedgePath(100, 100, 10, 40, -10, 10)
    expect(d.startsWith('M')).toBe(true)
    expect(d.endsWith('Z')).toBe(true)
    expect((d.match(/A/g) ?? []).length).toBe(2)
  })

  it('draws a wedge from the centre as a pie slice, with no inner arc', () => {
    const d = wedgePath(100, 100, 0, 40, -10, 10)
    expect((d.match(/A/g) ?? []).length).toBe(1)
  })
})

describe('decoder: wind_rose', () => {
  const decode = (over: Record<string, unknown>) => decodeMateCards([{ kind: 'wind_rose', payload: windRosePayload(over) }])[0]
  const reason = (c: ReturnType<typeof decode>) => (c.kind === 'error' ? c.reason : '')

  it('accepts the contract example', () => {
    expect(decode({})).toMatchObject({ kind: 'wind_rose', sectors: 16, speedUnit: 'kn', reference: 'true', calmBelow: 1 })
  })

  it.each([
    ['sectors not 8, 16 or 32', { sectors: 12 }, 'sectors'],
    ['counts with the wrong number of sectors', { counts: [[0, 0, 0, 0, 0, 0]] }, 'counts'],
    ['a count row with the wrong number of bands', { counts: Array.from({ length: 16 }, () => [0, 0]) }, 'counts[0]'],
    ['a negative count', { counts: Array.from({ length: 16 }, (_, i) => (i === 3 ? [-1, 0, 0, 0, 0, 0] : [0, 0, 0, 0, 0, 0])) }, 'counts[3][0]'],
    ['a fractional count', { counts: Array.from({ length: 16 }, (_, i) => (i === 0 ? [0.5, 0, 0, 0, 0, 0] : [0, 0, 0, 0, 0, 0])) }, 'counts[0][0]'],
    ['counts that exceed the samples', { samples: 10 }, 'samples'],
    ['an unknown reference', { reference: 'apparent' }, 'reference'],
    ['bands that do not start at zero', { bins: [{ from: 2, to: 5 }, { from: 5, to: null }] }, 'bins'],
    ['bands that leave a gap', { bins: [{ from: 0, to: 5 }, { from: 6, to: null }] }, 'bins[1]'],
    ['a closed top band', { bins: [{ from: 0, to: 5 }, { from: 5, to: 10 }] }, 'bins'],
    ['a missing unit', { speed_unit: 3 }, 'speed_unit'],
    ['a bad range', { range: { from: 'soon', to: '2026-10-19T00:00:00+10:00' } }, 'range.from'],
    ['a bad timezone', { timezone: 'Mars/Olympus' }, 'timezone'],
    ['a strongest gust with no time', { strongest: { speed: 20, at: 'x' } }, 'strongest.at'],
  ])('rejects %s, naming the field', (_n, over, field) => {
    const c = decode(over)
    expect(c.kind).toBe('error')
    expect(reason(c)).toContain(field)
    expect(c.kind === 'error' && c.rawKind).toBe('wind_rose')
  })

  it('accepts a null mean speed and a null strongest gust', () => {
    expect(decode({ mean_speed: null, strongest: null })).toMatchObject({ kind: 'wind_rose', meanSpeed: null, strongest: null })
  })
})

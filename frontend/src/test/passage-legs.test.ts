import { describe, expect, it } from 'vitest'

import { clipSegments, exportLegSegments, legSegments, passageLegs } from '@/lib/passage-legs'

const P = (id: string, s: number, e: number) => ({ id, startMs: s, endMs: e })
const J = (t_ms: number, starts_leg = true) => ({ t_ms, starts_leg })

describe('passageLegs', () => {
  it('makes one leg per passage without joins', () => {
    expect(passageLegs([P('a', 0, 10), P('b', 20, 30)], [J(5, false)])).toEqual([
      { passageId: 'a', fromMs: 0, toMs: 10, colourIndex: 0 },
      { passageId: 'b', fromMs: 20, toMs: 30, colourIndex: 1 },
    ])
  })
  it('splits at a join inside a passage', () => {
    expect(passageLegs([P('a', 0, 10)], [J(4)])).toEqual([
      { passageId: 'a', fromMs: 0, toMs: 4, colourIndex: 0 },
      { passageId: 'a', fromMs: 4, toMs: 10, colourIndex: 1 },
    ])
  })
  it('ignores a join exactly at a passage boundary', () => {
    expect(passageLegs([P('a', 0, 10), P('b', 10, 20)], [J(10), J(0), J(20)])).toHaveLength(2)
  })
  it('numbers colours across passages with joins', () => {
    const legs = passageLegs([P('a', 0, 10), P('b', 20, 40)], [J(5), J(25), J(30)])
    expect(legs.map((l) => [l.passageId, l.colourIndex])).toEqual([['a', 0], ['a', 1], ['b', 2], ['b', 3], ['b', 4]])
  })
  it('keeps counting past the palette', () => {
    const legs = passageLegs([P('a', 0, 100)], Array.from({ length: 7 }, (_, i) => J(10 + i)))
    expect(legs).toHaveLength(8)
    expect(legs[7].colourIndex).toBe(7)
  })
})

describe('clipSegments', () => {
  const seg = [{ t: 0, lat: 0, lon: 0 }, { t: 10, lat: 10, lon: 20 }, { t: 20, lat: 20, lon: 40 }]
  it('cuts on interpolated positions so legs meet', () => {
    const a = clipSegments([seg], -Infinity, 5)
    const b = clipSegments([seg], 5, Infinity)
    expect(a[0][a[0].length - 1]).toEqual({ t: 5, lat: 5, lon: 10 })
    expect(b[0][0]).toEqual({ t: 5, lat: 5, lon: 10 })
    expect(b[0]).toHaveLength(3)
  })
  it('drops a segment with nothing inside', () => {
    expect(clipSegments([seg], 30, 40)).toEqual([])
  })
  it('legSegments leaves a one-leg passage alone', () => {
    const segs = [seg]
    expect(legSegments(segs, [{ passageId: 'a', fromMs: 0, toMs: 20000, colourIndex: 0 }], 0)).toBe(segs)
  })
})

describe('exportLegSegments', () => {
  const seg = [{ t: 0, lat: 0, lon: 0 }, { t: 10, lat: 1, lon: 1 }, { t: 20, lat: 2, lon: 2 }]
  const legs = [{ a: 0, b: 10 }, { a: 10, b: 20 }]
  it('cuts each leg and leaves a single leg whole', () => {
    expect(exportLegSegments([seg], legs, 0)[0].map((p) => p.t)).toEqual([0, 10])
    expect(exportLegSegments([seg], legs, 1)[0].map((p) => p.t)).toEqual([10, 20])
    const one = [seg]
    expect(exportLegSegments(one, [{ a: 0, b: 20 }], 0)).toBe(one)
  })
})

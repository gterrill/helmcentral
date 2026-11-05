import { describe, expect, it } from 'vitest'
import {
  AIS_BAND_COLORS,
  BAND_CLOCK_MS,
  SELF_BAND_COLORS,
  TRAIL_BANDS,
  bandClock,
  bandForAge,
  groupLinesByAge,
  msToNextBandClock,
  type AgedPoint,
} from '@/lib/trail-age-bands'

const HOUR = 60 * 60 * 1000
const NOW = 1_700_000_000_000
const pt = (ageMs: number, lat = 0, newSegment = false): AgedPoint => ({ lat, lon: 0, timestampMs: NOW - ageMs, newSegment })
const DAY = { nowMs: NOW, windowMs: 24 * HOUR }

describe('bandForAge', () => {
  it('puts the newest in the last band and the oldest in the first, across the window', () => {
    const window = 24 * HOUR
    expect(bandForAge(0, window)).toBe(TRAIL_BANDS - 1)
    expect(bandForAge(1 * HOUR, window)).toBe(TRAIL_BANDS - 1)
    expect(bandForAge(23 * HOUR, window)).toBe(0)
    expect(bandForAge(window - 1, window)).toBe(0)
  })

  it('steps one band at each fifth of the window', () => {
    const window = 10 * HOUR
    const width = window / TRAIL_BANDS
    expect(bandForAge(width - 1, window)).toBe(TRAIL_BANDS - 1)
    expect(bandForAge(width, window)).toBe(TRAIL_BANDS - 2)
    expect(bandForAge(2 * width, window)).toBe(TRAIL_BANDS - 3)
  })

  it('treats a point stamped slightly in the future as the newest', () => {
    expect(bandForAge(-5000, 24 * HOUR)).toBe(TRAIL_BANDS - 1)
  })
})

describe('groupLinesByAge', () => {
  it('gives each line the band of its newer end, one line per run of a band', () => {
    // A point every 4.8 h reaches one band per point, newest last.
    const step = (24 * HOUR) / TRAIL_BANDS
    const points = [4, 3, 2, 1, 0].map(i => pt(i * step + 1000, i))
    const lines = groupLinesByAge(points, DAY)
    expect(lines.map(l => l.band)).toEqual([1, 2, 3, 4])
    expect(lines[lines.length - 1].band).toBe(TRAIL_BANDS - 1)
  })

  it('joins consecutive lines of one band into one line, and starts a band with the point before it', () => {
    const points = [pt(30 * 60_000, 0), pt(20 * 60_000, 1), pt(10 * 60_000, 2)]
    const lines = groupLinesByAge(points, DAY)
    expect(lines).toHaveLength(1)
    expect(lines[0].coordinates).toHaveLength(3)
  })

  it('does not draw a line into a point that starts a new segment', () => {
    const points = [pt(40 * 60_000, 0), pt(30 * 60_000, 1), pt(20 * 60_000, 2, true), pt(10 * 60_000, 3)]
    const lines = groupLinesByAge(points, DAY)
    expect(lines).toHaveLength(2)
    expect(lines[0].coordinates.map(c => c[1])).toEqual([0, 1])
    expect(lines[1].coordinates.map(c => c[1])).toEqual([2, 3])
  })

  it('leaves out stretches that end older than the window', () => {
    const points = [pt(30 * HOUR, 0), pt(26 * HOUR, 1), pt(1 * HOUR, 2)]
    const lines = groupLinesByAge(points, DAY)
    expect(lines.flatMap(l => l.coordinates.map(c => c[1]))).toEqual([1, 2])
  })

  it('bands the whole stay for an own trail: the newest point is in the newest band', () => {
    const stay = 10 * HOUR
    const points = [pt(stay, 0), pt(stay / 2, 1), pt(1000, 2)]
    const lines = groupLinesByAge(points, { nowMs: NOW, windowMs: stay })
    expect(lines[0].band).toBeLessThan(lines[lines.length - 1].band)
    expect(lines[lines.length - 1].band).toBe(TRAIL_BANDS - 1)
  })
})

describe('band clock', () => {
  it('rounds down to the five minute grid', () => {
    const mark = 7 * BAND_CLOCK_MS
    expect(BAND_CLOCK_MS).toBe(5 * 60 * 1000)
    expect(bandClock(mark)).toBe(mark)
    expect(bandClock(mark + BAND_CLOCK_MS - 1)).toBe(mark)
    expect(bandClock(mark + BAND_CLOCK_MS)).toBe(mark + BAND_CLOCK_MS)
  })

  it('says how long until the next mark', () => {
    const mark = 7 * BAND_CLOCK_MS
    expect(msToNextBandClock(mark)).toBe(BAND_CLOCK_MS)
    expect(msToNextBandClock(mark + 1000)).toBe(BAND_CLOCK_MS - 1000)
    expect(msToNextBandClock(mark + BAND_CLOCK_MS - 1)).toBe(1)
  })
})

describe('band colours', () => {
  const channels = (c: string): number[] => {
    const rgba = c.match(/rgba?\(([^)]+)\)/)
    if (rgba) return rgba[1].split(',').map(Number)
    const n = parseInt(c.slice(1), 16)
    return [(n >> 16) & 255, (n >> 8) & 255, n & 255]
  }

  it('fade the AIS amber from near transparent to solid', () => {
    expect(AIS_BAND_COLORS).toHaveLength(TRAIL_BANDS)
    const alphas = AIS_BAND_COLORS.map(c => channels(c)[3])
    expect(alphas[TRAIL_BANDS - 1]).toBe(1)
    for (let i = 1; i < alphas.length; i++) expect(alphas[i]).toBeGreaterThan(alphas[i - 1])
    expect(alphas[0]).toBeLessThan(0.3)
    expect(AIS_BAND_COLORS[TRAIL_BANDS - 1]).toContain('245, 158, 11')
  })

  it('brighten the own trail yellow from oldest to newest, solid throughout', () => {
    expect(SELF_BAND_COLORS).toHaveLength(TRAIL_BANDS)
    const lum = SELF_BAND_COLORS.map(c => { const [r, g, b] = channels(c); return 0.2126 * r + 0.7152 * g + 0.0722 * b })
    for (let i = 1; i < lum.length; i++) expect(lum[i]).toBeGreaterThan(lum[i - 1])
    for (const c of SELF_BAND_COLORS) expect(c.startsWith('#')).toBe(true)
  })
})

import { describe, expect, it } from 'vitest'

import {
  buildTimeline,
  clockFor,
  fmtDuration,
  fmtFigure,
  fmtHours,
  fmtKn,
  fmtVolume,
  fmtNM,
  fmtPct,
  formatLocalDate,
  formatLocalDateTime,
  locate,
  passageHeading,
  passageRoute,
  positionAt,
  riseLabelTime,
  segmentBounds,
  segmentsToGeoJSON,
  timedSegments,
  vesselZone,
} from '@/lib/passages'

describe('figure formatting', () => {
  it('prints -- for absent readings, never a number', () => {
    for (const f of [fmtNM, fmtKn, (v: number | null | undefined) => fmtVolume(v, 'metric'), (v: number | null | undefined) => fmtVolume(v, 'imperial'), fmtPct, fmtHours, fmtFigure, fmtDuration]) {
      expect(f(null)).toBe('--')
      expect(f(undefined)).toBe('--')
      expect(f(Number.NaN)).toBe('--')
    }
  })

  it('prints zero as a real figure', () => {
    expect(fmtNM(0)).toBe('0.0 NM')
    expect(fmtVolume(0, 'metric')).toBe('0 L')
    expect(fmtDuration(0)).toBe('0 m')
    expect(fmtPct(0)).toBe('0%')
  })

  it('formats units and durations', () => {
    expect(fmtNM(12.34)).toBe('12.3 NM')
    expect(fmtKn(6.07)).toBe('6.1 kn')
    expect(fmtVolume(341.6, 'metric')).toBe('342 L')
    expect(fmtHours(2.26)).toBe('2.3 h')
    expect(fmtDuration(3 * 3600 + 5 * 60)).toBe('3 h 05 m')
    expect(fmtDuration(42 * 60)).toBe('42 m')
  })

  it('shows an unnamed end as -- and falls back to the route for a missing title', () => {
    const base = { start: '2026-03-13T23:30:00Z', start_lon: 149 }
    expect(passageRoute({ from_name: 'Airlie', to_name: '' })).toBe('Airlie → --')
    expect(passageHeading({ ...base, title: '  ', from_name: 'Airlie', to_name: 'Hamilton' })).toBe('Airlie → Hamilton')
    expect(passageHeading({ ...base, title: 'Delivery leg', from_name: 'A', to_name: 'B' })).toBe('Delivery leg')
    expect(passageHeading({ ...base, title: '', from_name: 'Airlie', to_name: '' })).toBe('Airlie → --')
  })

  it('names a passage with no title and no places by its boat-local date', () => {
    const p = { start: '2026-03-13T23:30:00Z', start_lon: 149, title: '', from_name: '', to_name: '' }
    expect(passageHeading(p)).toBe('Passage · 14 Mar')
    expect(passageHeading({ ...p, start_lon: null })).toBe('Passage · 13 Mar')
  })
})

describe('volume follows the Units setting', () => {
  it('shows litres for metric and US gallons for imperial', () => {
    expect(fmtVolume(100, 'metric')).toBe('100 L')
    expect(fmtVolume(100, 'imperial')).toBe('26 gal')
    expect(fmtVolume(null, 'imperial')).toBe('--')
  })
})

describe('boat-local time', () => {
  it('uses whole hours from longitude, matching the server', () => {
    expect(vesselZone(149)).toEqual({ offsetHours: 10, label: 'UTC+10' })
    expect(vesselZone(-70)).toEqual({ offsetHours: -5, label: 'UTC-5' })
    expect(vesselZone(0)).toEqual({ offsetHours: 0, label: 'UTC' })
    expect(vesselZone(999).label).toBe('UTC')
  })

  it('labels the zone on the time and rolls the date', () => {
    const t = Date.UTC(2026, 2, 13, 23, 30) / 1000
    expect(formatLocalDateTime(t, 149)).toBe('14 Mar 09:30 UTC+10')
    expect(formatLocalDate(new Date(t * 1000).toISOString(), 149)).toBe('14 Mar')
    expect(formatLocalDate('garbage', 149)).toBe('--')
  })
})

describe('track segments', () => {
  const track: [number, number][][] = [
    [[-20, 149], [-20.1, 149.1]],
    [[-21, 150]],
    [[-22, 151], [-22.1, 151.1], [-22.2, 151.2]],
  ]
  const times = [[0, 100], [500], [1000, 1100, 1200]]

  it('pairs positions with times and refuses a mismatch', () => {
    const segs = timedSegments(track, times)
    expect(segs[0][1]).toEqual({ t: 100, lat: -20.1, lon: 149.1 })
    expect(() => timedSegments(track, [[0, 100]])).toThrow()
    expect(() => timedSegments(track, [[0], [500], [1, 2, 3]])).toThrow()
  })

  it('draws one line per segment and never joins across a gap', () => {
    const geo = segmentsToGeoJSON(timedSegments(track, times))
    expect(geo.geometry.coordinates).toHaveLength(2)
    expect(geo.geometry.coordinates[0]).toEqual([[149, -20], [149.1, -20.1]])
    expect(geo.geometry.coordinates[1][0]).toEqual([151, -22])
  })

  it('bounds every point, including a lone one', () => {
    expect(segmentBounds(timedSegments(track, times))).toEqual([149, -22.2, 151.2, -20])
    expect(segmentBounds([])).toBeNull()
  })
})

describe('replay position', () => {
  const segs = timedSegments(
    [[[0, 0], [0, 10]], [[5, 20], [5, 30]]],
    [[1000, 2000], [5000, 6000]],
  )

  it('interpolates inside a segment', () => {
    expect(positionAt(segs, 1500)).toEqual({ lat: 0, lon: 5 })
    expect(positionAt(segs, 5250)).toEqual({ lat: 5, lon: 22.5 })
  })

  it('holds at the last fix across a gap instead of sliding over it', () => {
    expect(positionAt(segs, 3500)).toEqual({ lat: 0, lon: 10 })
  })

  it('holds at the ends and returns null for an empty track', () => {
    expect(positionAt(segs, 0)).toEqual({ lat: 0, lon: 0 })
    expect(positionAt(segs, 9999)).toEqual({ lat: 5, lon: 30 })
    expect(positionAt([], 10)).toBeNull()
  })
})

describe('replay timeline', () => {
  const a = { id: 'a', start: 1000, end: 2000, segments: [] }
  const b = { id: 'b', start: 100000, end: 100500, segments: [] }

  it('lays passages end to end, dropping the time between them', () => {
    const tl = buildTimeline([b, a])
    expect(tl.entries.map((e) => e.id)).toEqual(['a', 'b'])
    expect(tl.total).toBe(1500)
    expect(tl.entries[1].offset).toBe(1000)
  })

  it('maps the clock to a real instant in the right passage', () => {
    const tl = buildTimeline([a, b])
    expect(locate(tl, 250)).toMatchObject({ entry: { id: 'a' }, at: 1250 })
    expect(locate(tl, 1100)).toMatchObject({ entry: { id: 'b' }, at: 100100 })
    expect(locate(tl, 99999)).toMatchObject({ entry: { id: 'b' }, at: 100500 })
    expect(locate(buildTimeline([]), 0)).toBeNull()
  })

  it('maps a real instant back to the clock, clamped to the passage', () => {
    const tl = buildTimeline([a, b])
    expect(clockFor(tl, 'b', 100200)).toBe(1200)
    expect(clockFor(tl, 'a', 0)).toBe(0)
    expect(clockFor(tl, 'zzz', 0)).toBeNull()
  })
})

describe('water rise label time', () => {
  it('names an instant inside the rise, not its edge', () => {
    expect(riseLabelTime({ start: '2026-03-14T01:00:00Z', end: '2026-03-14T01:20:00Z' })).toBe('2026-03-14T01:10:00.000Z')
  })
})

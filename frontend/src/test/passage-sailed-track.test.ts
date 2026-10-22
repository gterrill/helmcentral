import { describe, expect, it } from 'vitest'

import { buildTimeline, createSailedCache, locate, sailedAt, sailedToGeoJSON, type TimedSegment } from '@/lib/passages'

const seg: TimedSegment = [
  { t: 0, lat: 0, lon: 0 },
  { t: 10, lat: 1, lon: 1 },
  { t: 20, lat: 2, lon: 3 },
]
const later: TimedSegment = [
  { t: 100, lat: 5, lon: 5 },
  { t: 110, lat: 6, lon: 6 },
]

const coords = (at: number, segments: TimedSegment[]) => sailedToGeoJSON(segments, at).geometry.coordinates

describe('sailedToGeoJSON', () => {
  it('ends at the point interpolated between the fixes either side of the cursor', () => {
    expect(coords(15, [seg])).toEqual([[[0, 0], [1, 1], [2, 1.5]]])
  })

  it('draws the whole segment once the cursor is past it', () => {
    expect(coords(50, [seg])).toEqual([[[0, 0], [1, 1], [3, 2]]])
  })

  it('draws nothing before the first fix, or with only the first fix reached', () => {
    expect(coords(-5, [seg])).toEqual([])
    expect(coords(0, [seg])).toEqual([])
  })

  it('ends exactly on a fix when the cursor sits on it', () => {
    expect(coords(10, [seg])).toEqual([[[0, 0], [1, 1]]])
  })

  it('holds at the end of a segment through a gap and starts the next one only when reached', () => {
    expect(coords(60, [seg, later])).toEqual([[[0, 0], [1, 1], [3, 2]]])
    expect(coords(105, [seg, later])).toEqual([[[0, 0], [1, 1], [3, 2]], [[5, 5], [5.5, 5.5]]])
  })
})

describe('sailedToGeoJSON ending at the boat', () => {
  it('ends the line exactly on the given boat position', () => {
    const g = sailedToGeoJSON([seg], 15, { lat: 1.4, lon: 1.9 })
    expect(g.geometry.coordinates).toEqual([[[0, 0], [1, 1], [1.9, 1.4]]])
  })

  it('only moves the end of the last line drawn', () => {
    const g = sailedToGeoJSON([seg, later], 105, { lat: 5.4, lon: 5.4 })
    expect(g.geometry.coordinates).toEqual([[[0, 0], [1, 1], [3, 2]], [[5, 5], [5.4, 5.4]]])
  })
})

describe('createSailedCache', () => {
  const segs1 = [seg]
  const segs2 = [later]
  it('hands back the same object while nothing it depends on has moved', () => {
    const cache = createSailedCache()
    const a = cache.get('p1', segs1, 15, { lat: 1, lon: 1 })
    expect(cache.get('p1', segs1, 15, { lat: 1, lon: 1 })).toBe(a)
  })

  it('builds a new one when the instant, the boat or the segments change, and keeps other passages', () => {
    const cache = createSailedCache()
    const a = cache.get('p1', segs1, 15)
    const other = cache.get('p2', segs2, 105)
    expect(cache.get('p1', segs1, 16)).not.toBe(a)
    expect(cache.get('p1', segs1, 16, { lat: 1, lon: 1 })).not.toBe(cache.get('p1', segs1, 16))
    expect(cache.get('p1', [seg], 16)).not.toBe(cache.get('p1', segs1, 16))
    expect(cache.get('p2', segs2, 105)).toBe(other)
  })
})

describe('sailedAt', () => {
  const tl = buildTimeline([
    { id: 'a', start: 1000, end: 1100, segments: [] },
    { id: 'b', start: 5000, end: 5050, segments: [] },
  ])
  const [a, b] = tl.entries

  it('is the start before the passage is reached', () => {
    expect(sailedAt(b, 50)).toBe(5000)
  })
  it('follows the clock inside the passage', () => {
    expect(sailedAt(b, 120)).toBe(5020)
  })
  it('is the end once the passage is done', () => {
    expect(sailedAt(a, 500)).toBe(1100)
  })
  it('agrees with locate for the passage the clock is in', () => {
    const here = locate(tl, 120)!
    expect(sailedAt(here.entry, 120)).toBe(here.at)
  })
})

import { describe, expect, it } from 'vitest'

import { replayWallDuration, resolveShots, timelineAtWall, wallAtTimeline, type CameraShot } from '@/lib/replay-clock'
import { buildTimeline } from '@/lib/passages'

describe('replay clock', () => {
  it('with no pace spans the replay takes the chosen length', () => {
    expect(replayWallDuration(1000, 15, [])).toBeCloseTo(15)
    expect(timelineAtWall(1000, 15, [], 7.5)).toBeCloseTo(500)
    expect(wallAtTimeline(1000, 15, [], 500)).toBeCloseTo(7.5)
  })

  it('a slower stretch takes proportionally longer on the wall clock', () => {
    // 1000 s of timeline in 10 s: 100 timeline s per wall s. Half pace from 400 to 800.
    const spans = [{ from: 400, pace: 0.5 }, { from: 800, pace: 1 }]
    // 400 at 1x = 4 s; 400 at 0.5x = 8 s; 200 at 1x = 2 s.
    expect(replayWallDuration(1000, 10, spans)).toBeCloseTo(14)
    expect(wallAtTimeline(1000, 10, spans, 400)).toBeCloseTo(4)
    expect(wallAtTimeline(1000, 10, spans, 800)).toBeCloseTo(12)
    expect(timelineAtWall(1000, 10, spans, 8)).toBeCloseTo(600)
    expect(timelineAtWall(1000, 10, spans, 12)).toBeCloseTo(800)
    expect(timelineAtWall(1000, 10, spans, 13)).toBeCloseTo(900)
  })

  it('a quarter-pace shot that runs to the end stretches the tail', () => {
    expect(replayWallDuration(100, 10, [{ from: 50, pace: 0.25 }])).toBeCloseTo(5 + 20)
  })

  it('wall and timeline are inverse, clamp at the ends and never go backwards', () => {
    const spans = [{ from: 100, pace: 0.25 }, { from: 300, pace: 0.5 }]
    let prev = -1
    for (let c = 0; c <= 1000; c += 37) {
      const w = wallAtTimeline(1000, 30, spans, c)
      expect(w).toBeGreaterThanOrEqual(prev)
      prev = w
      expect(timelineAtWall(1000, 30, spans, w)).toBeCloseTo(c, 6)
    }
    expect(timelineAtWall(1000, 30, spans, -5)).toBe(0)
    expect(timelineAtWall(1000, 30, spans, 9999)).toBe(1000)
    expect(wallAtTimeline(1000, 30, spans, 5000)).toBeCloseTo(replayWallDuration(1000, 30, spans))
  })

  it('spans given out of order or beyond the end are tolerated', () => {
    const spans = [{ from: 800, pace: 0.5 }, { from: 200, pace: 0.5 }, { from: 5000, pace: 0.25 }]
    expect(replayWallDuration(1000, 10, spans)).toBeCloseTo(replayWallDuration(1000, 10, [{ from: 200, pace: 0.5 }, { from: 800, pace: 0.5 }]))
  })

  it('an empty timeline has no length', () => {
    expect(replayWallDuration(0, 15, [])).toBe(0)
    expect(timelineAtWall(0, 15, [], 3)).toBe(0)
  })
})

const shot = (id: string, tSeconds: number, extra: Partial<CameraShot> = {}): CameraShot => ({
  id, t_ms: tSeconds * 1000, mode: 'follow', zoom: 14, pitch: 50, bearing_offset_deg: 0, orbit_deg_per_s: 0, pace: 1, ...extra,
})

describe('resolveShots', () => {
  const timeline = buildTimeline([
    { id: 'a', start: 1000, end: 1100, segments: [] },
    { id: 'b', start: 5000, end: 5200, segments: [] },
  ])

  it('places each shot on the replay clock by its real time', () => {
    const r = resolveShots([shot('x', 5050), shot('y', 1010)], timeline)
    expect(r.map((s) => [s.shot.id, s.clock, s.at])).toEqual([['y', 10, 1010], ['x', 150, 5050]])
  })

  it('leaves out shots that fall in no replayed passage (trimmed out, or in the gap)', () => {
    expect(resolveShots([shot('gap', 3000), shot('before', 10), shot('after', 9000)], timeline)).toEqual([])
  })

  it('keeps a shot when its passage is split: the clock follows the real time', () => {
    const split = buildTimeline([
      { id: 'a1', start: 1000, end: 1040, segments: [] },
      { id: 'a2', start: 1040, end: 1100, segments: [] },
    ])
    expect(resolveShots([shot('x', 1070)], split)[0].clock).toBeCloseTo(70)
  })
})

describe('resolveShots entry ids and the clock table', () => {
  it('names the passage the shot falls in, the first one at a shared boundary', () => {
    const tl = buildTimeline([
      { id: 'a', start: 1000, end: 1100, segments: [] },
      { id: 'b', start: 1100, end: 1200, segments: [] },
    ])
    expect(resolveShots([shot('x', 1100)], tl)[0].entryId).toBe('a')
    expect(resolveShots([shot('y', 1101)], tl)[0].entryId).toBe('b')
  })

  it('answers repeated lookups from one table', () => {
    const spans = [{ from: 100, pace: 0.5 }]
    const a = wallAtTimeline(1000, 10, spans, 300)
    expect(wallAtTimeline(1000, 10, spans, 300)).toBe(a)
    expect(timelineAtWall(1000, 10, spans, a)).toBeCloseTo(300)
  })
})

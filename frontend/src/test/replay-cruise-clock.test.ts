import { describe, expect, it } from 'vitest'

import { MIN_PASSAGE_SHARE_S, cruisePaceSpans, passageShares, paceSpans, replayWallDuration, timelineAtWall, wallAtTimeline, type CameraShot, type ResolvedShot } from '@/lib/replay-clock'
import { buildTimeline } from '@/lib/passages'

const sum = (xs: number[]) => xs.reduce((a, b) => a + b, 0)

describe('passageShares', () => {
  it('shares the length in proportion to sailing time', () => {
    const shares = passageShares([1000, 3000], 60)
    expect(shares[0]).toBeCloseTo(15)
    expect(shares[1]).toBeCloseTo(45)
  })

  it('lifts a short passage to the minimum and scales the others down to fit', () => {
    // By time the first would get 1.2 s of 60; it is given 4, and the others share the other 56.
    const shares = passageShares([100, 3000, 5000], 60)
    expect(shares[0]).toBe(MIN_PASSAGE_SHARE_S)
    expect(shares[1] / shares[2]).toBeCloseTo(3000 / 5000)
    expect(sum(shares)).toBeCloseTo(60)
  })

  it('lifts the short ones all together', () => {
    const shares = passageShares([1, 1, 1, 200], 60)
    expect(shares.slice(0, 3)).toEqual([4, 4, 4])
    expect(shares[3]).toBeCloseTo(48)
  })

  it('goes on lifting: scaling the others down can bring another below the minimum', () => {
    // 40 s over [1, 12, 100]: the first is lifted to 4, which leaves 36 over 112, and the second's 3.86 is under 4 as well.
    const shares = passageShares([1, 12, 100], 40)
    expect(shares[0]).toBe(4)
    expect(shares[1]).toBe(4)
    expect(shares[2]).toBeCloseTo(32)
    expect(sum(shares)).toBeCloseTo(40)
  })

  it('gives every passage an equal share when the minimums alone come to more than the length', () => {
    expect(passageShares([100, 3000, 50, 700, 9000], 15)).toEqual([3, 3, 3, 3, 3])
    expect(passageShares([100, 3000, 50], 12)).toEqual([4, 4, 4])
  })

  it('leaves a lone passage the whole length, and gives a passage with no sailing nothing', () => {
    expect(passageShares([1234], 30)).toEqual([30])
    const shares = passageShares([1000, 0, 1000], 60)
    expect(shares).toEqual([30, 0, 30])
  })

  it('always adds up to the length', () => {
    for (const lengthS of [8, 15, 30, 60, 120, 180]) {
      for (const d of [[10, 20, 30], [1, 1, 1000], [500, 20, 7, 3000], [5, 5]]) {
        expect(sum(passageShares(d, lengthS))).toBeCloseTo(lengthS)
      }
    }
  })
})

describe('cruisePaceSpans', () => {
  const timeline = buildTimeline([
    { id: 'a', start: 0, end: 100, segments: [] },
    { id: 'b', start: 5000, end: 5000 + 3000, segments: [] },
    { id: 'c', start: 90_000, end: 90_000 + 900, segments: [] },
  ])
  const shot = (clock: number, pace: number): ResolvedShot => ({ shot: { id: `s${clock}`, pace } as CameraShot, clock, at: clock, entryId: 'a' })

  it('is the shots\' own spans for a single passage', () => {
    const one = buildTimeline([{ id: 'a', start: 0, end: 100, segments: [] }])
    const resolved = [shot(10, 0.5)]
    expect(cruisePaceSpans(resolved, one, 60)).toEqual(paceSpans(resolved))
    expect(cruisePaceSpans([], one, 60)).toEqual([])
  })

  it('gives each passage its share of the length, in the time it takes to watch', () => {
    const spans = cruisePaceSpans([], timeline, 60)
    const wall = (clock: number) => wallAtTimeline(timeline.total, 60, spans, clock)
    const shares = passageShares([100, 3000, 900], 60)
    expect(wall(timeline.entries[1].offset)).toBeCloseTo(shares[0])
    expect(wall(timeline.entries[2].offset)).toBeCloseTo(shares[0] + shares[1])
    expect(replayWallDuration(timeline.total, 60, spans)).toBeCloseTo(60)
    // The shortest passage was lifted to the minimum.
    expect(shares[0]).toBe(MIN_PASSAGE_SHARE_S)
  })

  it('keeps the two clocks in step, both ways', () => {
    const spans = cruisePaceSpans([], timeline, 60)
    for (const w of [0, 2, 4, 10, 30, 59.9]) {
      expect(wallAtTimeline(timeline.total, 60, spans, timelineAtWall(timeline.total, 60, spans, w))).toBeCloseTo(w, 6)
    }
  })

  it('slows a shot\'s pace on top of the passage\'s share, and returns to full pace at the end of that passage', () => {
    const resolved = [{ ...shot(500, 0.5), entryId: 'b' }]
    const plain = cruisePaceSpans([], timeline, 60)
    const slowed = cruisePaceSpans(resolved, timeline, 60)
    const wall = (spans: typeof plain, clock: number) => wallAtTimeline(timeline.total, 60, spans, clock)
    const b0 = timeline.entries[1].offset
    // Before the shot nothing changes; after it the same sailing takes twice as long, to the end of its passage.
    expect(wall(slowed, b0 + 400)).toBeCloseTo(wall(plain, b0 + 400))
    expect(wall(slowed, b0 + 1400) - wall(slowed, b0 + 400)).toBeCloseTo(2 * (wall(plain, b0 + 1400) - wall(plain, b0 + 400)))
    // The next passage runs at its own share again.
    const c0 = timeline.entries[2].offset
    expect(wall(slowed, c0 + 100) - wall(slowed, c0)).toBeCloseTo(wall(plain, c0 + 100) - wall(plain, c0))
  })
})

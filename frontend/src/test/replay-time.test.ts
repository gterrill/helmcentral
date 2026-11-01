import { describe, expect, it } from 'vitest'

import { MOMENT_FRAME_FRACTION, MOMENT_GROW_S, MOMENT_TOTAL_S, buildReplayTime, momentRect, momentView } from '@/lib/replay-time'

// 1000 timeline seconds watched in 10: 100 timeline seconds a second at full pace.
const TOTAL = 1000
const LENGTH = 10
const build = (moments: { clock: number; id: string }[] = [], spans: { from: number; pace: number }[] = []) =>
  buildReplayTime({ total: TOTAL, lengthS: LENGTH, spans, moments })

describe('the constants', () => {
  it('make a moment of about 2.3 seconds: 0.4 s to grow, a hold, 0.4 s to shrink', () => {
    expect(MOMENT_GROW_S).toBe(0.4)
    expect(MOMENT_TOTAL_S).toBeCloseTo(2.3)
    expect(MOMENT_FRAME_FRACTION).toBe(0.7)
  })
})

describe('a replay with no moments', () => {
  const t = build()

  it('is the length the operator chose', () => {
    expect(t.totalWall).toBeCloseTo(10)
  })

  it('maps watching time to the timeline and back', () => {
    expect(t.at(5).clock).toBeCloseTo(500)
    expect(t.at(5).moment).toBeNull()
    expect(t.wallAtClock(500)).toBeCloseTo(5)
  })

  it('holds at the ends', () => {
    expect(t.at(-3).clock).toBe(0)
    expect(t.at(50).clock).toBe(TOTAL)
  })
})

describe('a replay with moments', () => {
  const t = build([{ clock: 300, id: 'a' }, { clock: 700, id: 'b' }])

  it('adds each hold on top of the length chosen', () => {
    expect(t.totalWall).toBeCloseTo(10 + 2 * MOMENT_TOTAL_S)
  })

  it('sails as far as the first sign, then holds there for a moment', () => {
    expect(t.at(2.9).clock).toBeCloseTo(290)
    expect(t.at(3).moment).toMatchObject({ id: 'a' })
    expect(t.at(3).moment!.elapsed).toBeCloseTo(0)
    const mid = t.at(3 + 1)
    expect(mid.clock).toBeCloseTo(300)
    expect(mid.moment!.elapsed).toBeCloseTo(1)
    expect(t.at(3 + MOMENT_TOTAL_S - 0.01).clock).toBeCloseTo(300)
  })

  it('carries on from the sign when the hold is over', () => {
    const after = t.at(3 + MOMENT_TOTAL_S)
    expect(after.moment).toBeNull()
    expect(after.clock).toBeCloseTo(300)
    expect(t.at(3 + MOMENT_TOTAL_S + 1).clock).toBeCloseTo(400)
  })

  it('shifts the second hold by the first', () => {
    // The second sign is 7 s into the sailing and one hold later in the watching.
    const start = 7 + MOMENT_TOTAL_S
    expect(t.at(start - 0.01).moment).toBeNull()
    expect(t.at(start).moment).toMatchObject({ id: 'b' })
    expect(t.at(start + MOMENT_TOTAL_S).clock).toBeCloseTo(700)
  })

  it('ends at the end of the timeline, holds and all', () => {
    expect(t.at(t.totalWall).clock).toBe(TOTAL)
    expect(t.at(t.totalWall).moment).toBeNull()
  })

  it('never goes backwards as the watching goes on', () => {
    let clock = -1
    for (let w = 0; w <= t.totalWall; w += 0.05) {
      const c = t.at(w).clock
      expect(c).toBeGreaterThanOrEqual(clock)
      clock = c
    }
  })

  it('takes a hold in step with slower stretches: the pace only slows the sailing', () => {
    const slow = build([{ clock: 700, id: 'b' }], [{ from: 500, pace: 0.5 }])
    // 5 s to reach 500, then twice as long for the next 500: 15 s of sailing.
    expect(slow.totalWall).toBeCloseTo(15 + MOMENT_TOTAL_S)
    expect(slow.at(5 + 4).clock).toBeCloseTo(700 - 0, -1)
    expect(slow.at(5 + 4).moment).not.toBeNull()
  })
})

describe('seeking', () => {
  const t = build([{ clock: 300, id: 'a' }, { clock: 700, id: 'b' }])

  it('over sailing time: the slider is the timeline, not the watching', () => {
    expect(t.wallAtClock(0)).toBe(0)
    expect(t.wallAtClock(200)).toBeCloseTo(2)
    // After the first sign the watching is a hold ahead.
    expect(t.wallAtClock(400)).toBeCloseTo(4 + MOMENT_TOTAL_S)
    expect(t.wallAtClock(TOTAL)).toBeCloseTo(t.totalWall)
  })

  it('never lands in the middle of a hold, even on the very clock of a sign', () => {
    for (const clock of [0, 100, 299, 300, 301, 450, 699.5, 700, 701, 999, 1000]) {
      expect(t.at(t.wallAtClock(clock)).moment).toBeNull()
    }
  })

  it('lands after the hold of a sign standing at that clock', () => {
    const w = t.wallAtClock(300)
    expect(w).toBeCloseTo(3 + MOMENT_TOTAL_S)
    expect(t.at(w).clock).toBeCloseTo(300)
  })

  it('counts a seek to the end as the end, even with a sign standing there', () => {
    const atEnd = build([{ clock: 300, id: 'a' }, { clock: TOTAL, id: 'z' }])
    expect(atEnd.wallAtClock(TOTAL)).toBeCloseTo(atEnd.totalWall)
    expect(atEnd.at(atEnd.wallAtClock(TOTAL)).moment).toBeNull()
  })

  it('lands on the start of a sign\'s moment only when asked to land on the sign', () => {
    const w = t.wallAtClock(300, { onSign: true })
    expect(t.at(w).moment).toMatchObject({ id: 'a' })
    expect(t.at(w).moment!.elapsed).toBeCloseTo(0)
    expect(t.at(w).clock).toBeCloseTo(300)
    // Any other clock is the same either way.
    expect(t.wallAtClock(450, { onSign: true })).toBe(t.wallAtClock(450))
  })

  it('puts two signs at one place one hold after another; a seek there lands after both, or on the first', () => {
    const two = build([{ clock: 300, id: 'a' }, { clock: 300, id: 'b' }])
    expect(two.totalWall).toBeCloseTo(10 + 2 * MOMENT_TOTAL_S)
    expect(two.at(two.wallAtClock(300, { onSign: true })).moment!.id).toBe('a')
    expect(two.at(3 + MOMENT_TOTAL_S).moment!.id).toBe('b')
    expect(two.wallAtClock(300)).toBeCloseTo(3 + 2 * MOMENT_TOTAL_S)
    expect(two.at(two.wallAtClock(301)).moment).toBeNull()
  })
})

describe('where a hold ends', () => {
  const t = build([{ clock: 300, id: 'a' }])
  it('is the end of the moment the watching is in, and nowhere else', () => {
    expect(t.holdEnd(3.5)).toBeCloseTo(3 + MOMENT_TOTAL_S)
    expect(t.holdEnd(1)).toBe(1)
    expect(t.holdEnd(8)).toBe(8)
  })
})

describe('momentView', () => {
  const view = (elapsed: number, reduced = false) => momentView('a', elapsed, reduced)
  const HOLD = MOMENT_TOTAL_S - 2 * MOMENT_GROW_S

  it('starts as the card: nothing grown, no zoom', () => {
    expect(view(0)).toEqual({ signId: 'a', phase: 'grow', fill: 0, scale: 1 })
  })

  it('grows for 0.4 s', () => {
    const half = view(MOMENT_GROW_S / 2)
    expect(half.phase).toBe('grow')
    expect(half.fill).toBeCloseTo(0.5)
    expect(half.scale).toBe(1)
  })

  it('holds full, zooming slowly to 1.06 times', () => {
    expect(view(MOMENT_GROW_S)).toMatchObject({ phase: 'hold', fill: 1, scale: 1 })
    const mid = view(MOMENT_GROW_S + HOLD / 2)
    expect(mid.phase).toBe('hold')
    expect(mid.scale).toBeGreaterThan(1)
    expect(mid.scale).toBeLessThan(1.06)
    expect(view(MOMENT_GROW_S + HOLD - 0.001).scale).toBeCloseTo(1.06, 2)
  })

  it('shrinks back into the card for 0.4 s, keeping the zoom it reached', () => {
    const end = view(MOMENT_GROW_S + HOLD)
    expect(end).toMatchObject({ phase: 'shrink', fill: 1 })
    expect(end.scale).toBeCloseTo(1.06)
    expect(view(MOMENT_TOTAL_S - 0.001).fill).toBeCloseTo(0, 1)
  })

  it('grows and shrinks the same way round', () => {
    expect(view(0.1).fill).toBeCloseTo(view(MOMENT_TOTAL_S - 0.1).fill, 3)
  })

  it('under reduced motion shows the photo full and still for the whole moment', () => {
    for (const e of [0, 0.2, 1, MOMENT_TOTAL_S - 0.01]) {
      expect(view(e, true)).toMatchObject({ phase: 'hold', fill: 1, scale: 1 })
    }
  })

  it('is a function of the time in the moment alone', () => {
    const a = view(1.234)
    view(0.1)
    expect(view(1.234)).toEqual(a)
  })
})

describe('momentRect', () => {
  const card = { x: 100, y: 50, w: 240, h: 180 }
  const frame = { w: 1000, h: 400 }

  it('is the card\'s photo at the start', () => {
    expect(momentRect(momentView('a', 0, false), card, frame)).toEqual({ ...card, scale: 1 })
  })

  it('fills 70 percent of the frame when full, in the 4 by 3 shape, in the middle', () => {
    const r = momentRect(momentView('a', MOMENT_GROW_S + 0.1, false), card, frame)
    // 70 percent of the 400 high, and 4 by 3: 280 high, 373 wide.
    expect(r.h).toBeCloseTo(280)
    expect(r.w).toBeCloseTo(373.33, 1)
    expect(r.x).toBeCloseTo((1000 - r.w) / 2)
    expect(r.y).toBeCloseTo(60)
  })

  it('fits a tall frame by its width', () => {
    const r = momentRect(momentView('a', MOMENT_GROW_S + 0.1, false), card, { w: 400, h: 1000 })
    expect(r.w).toBeCloseTo(280)
    expect(r.h).toBeCloseTo(210)
  })

  it('is between the two while it grows and shrinks, and the same either way round', () => {
    const grow = momentRect(momentView('a', 0.2, false), card, frame)
    expect(grow.w).toBeGreaterThan(card.w)
    expect(grow.w).toBeLessThan(373.4)
    expect(grow.x).toBeGreaterThan(card.x)
    const shrink = momentRect(momentView('a', MOMENT_TOTAL_S - 0.2, false), card, frame)
    expect(shrink.w).toBeCloseTo(grow.w, 3)
  })

  it('carries the zoom inside the frame', () => {
    expect(momentRect(momentView('a', MOMENT_GROW_S + 0.75, false), card, frame).scale).toBeGreaterThan(1)
  })

  it('is full from the first moment with reduced motion', () => {
    const r = momentRect(momentView('a', 0, true), card, frame)
    expect(r.h).toBeCloseTo(280)
  })

  it('depends on its inputs alone', () => {
    const v = momentView('a', 0.3, false)
    const a = momentRect(v, card, frame)
    momentRect(momentView('a', 1, false), { x: 0, y: 0, w: 1, h: 1 }, { w: 10, h: 10 })
    expect(momentRect(v, card, frame)).toEqual(a)
  })
})

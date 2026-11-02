import { describe, expect, it } from 'vitest'

import { MOMENT_FRAME_FRACTION, MOMENT_GROW_S, MOMENT_TOTAL_S, buildReplayTime, momentGrowth, momentRect, momentView, playedIds } from '@/lib/replay-time'

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
    expect(t.at(5).hold).toBeNull()
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
    expect(t.at(3).hold).toMatchObject({ id: 'a' })
    expect(t.at(3).hold!.elapsed).toBeCloseTo(0)
    const mid = t.at(3 + 1)
    expect(mid.clock).toBeCloseTo(300)
    expect(mid.hold!.elapsed).toBeCloseTo(1)
    expect(t.at(3 + MOMENT_TOTAL_S - 0.01).clock).toBeCloseTo(300)
  })

  it('carries on from the sign when the hold is over', () => {
    const after = t.at(3 + MOMENT_TOTAL_S)
    expect(after.hold).toBeNull()
    expect(after.clock).toBeCloseTo(300)
    expect(t.at(3 + MOMENT_TOTAL_S + 1).clock).toBeCloseTo(400)
  })

  it('shifts the second hold by the first', () => {
    // The second sign is 7 s into the sailing and one hold later in the watching.
    const start = 7 + MOMENT_TOTAL_S
    expect(t.at(start - 0.01).hold).toBeNull()
    expect(t.at(start).hold).toMatchObject({ id: 'b' })
    expect(t.at(start + MOMENT_TOTAL_S).clock).toBeCloseTo(700)
  })

  it('ends at the end of the timeline, holds and all', () => {
    expect(t.at(t.totalWall).clock).toBe(TOTAL)
    expect(t.at(t.totalWall).hold).toBeNull()
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
    expect(slow.at(5 + 4).hold).not.toBeNull()
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
      expect(t.at(t.wallAtClock(clock)).hold).toBeNull()
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
    expect(atEnd.at(atEnd.wallAtClock(TOTAL)).hold).toBeNull()
  })

  it('lands on the start of a sign\'s moment only when asked to land on the sign', () => {
    const w = t.wallAtClock(300, { onSign: true })
    expect(t.at(w).hold).toMatchObject({ id: 'a' })
    expect(t.at(w).hold!.elapsed).toBeCloseTo(0)
    expect(t.at(w).clock).toBeCloseTo(300)
    // Any other clock is the same either way.
    expect(t.wallAtClock(450, { onSign: true })).toBe(t.wallAtClock(450))
  })

  it('puts two signs at one place one hold after another; a seek there lands after both, or on the first', () => {
    const two = build([{ clock: 300, id: 'a' }, { clock: 300, id: 'b' }])
    expect(two.totalWall).toBeCloseTo(10 + 2 * MOMENT_TOTAL_S)
    expect(two.at(two.wallAtClock(300, { onSign: true })).hold!.id).toBe('a')
    expect(two.at(3 + MOMENT_TOTAL_S).hold!.id).toBe('b')
    expect(two.wallAtClock(300)).toBeCloseTo(3 + 2 * MOMENT_TOTAL_S)
    expect(two.at(two.wallAtClock(301)).hold).toBeNull()
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
  // A card with a photo: 256 by 224 at its own size.
  const face = { w: 256, h: 224 }
  const card = { x: 100, y: 50, w: 256, h: 224 }
  const frame = { w: 1000, h: 400 }
  const full = (view = momentView('a', MOMENT_GROW_S + 0.1, false), c = card, f = frame, fc = face) => momentRect(view, c, fc, f)
  const fits = (r: { x: number; y: number; w: number; h: number }, f: { w: number; h: number }) => {
    expect(r.x).toBeGreaterThanOrEqual(0)
    expect(r.y).toBeGreaterThanOrEqual(0)
    expect(r.x + r.w).toBeLessThanOrEqual(f.w + 1e-6)
    expect(r.y + r.h).toBeLessThanOrEqual(f.h + 1e-6)
  }

  it('is the card as it stands on the map at the start', () => {
    const r = momentRect(momentView('a', 0, false), card, face, frame)
    expect(r.card).toEqual(card)
    expect(r.k).toBe(1)
  })

  it('is the card as it stands, smaller than its own size, when the map made it smaller', () => {
    const small = { x: 100, y: 50, w: 128, h: 112 }
    const r = momentRect(momentView('a', 0, false), small, face, frame)
    expect(r.card).toEqual(small)
    expect(r.k).toBeCloseTo(0.5)
  })

  it('fills 70 percent of the frame when full: the whole card, in its own shape, in the middle', () => {
    const r = full(undefined, card, { w: 1000, h: 600 })
    // 70 percent of the 600 high is 420, and the card is 256 by 224: 480 wide.
    expect(r.card.h).toBeCloseTo(420)
    expect(r.card.w).toBeCloseTo(480)
    expect(r.card.x).toBeCloseTo((1000 - r.card.w) / 2)
    expect(r.card.y).toBeCloseTo(90)
    expect(r.k).toBeCloseTo(1.875)
    expect(r.card.w / r.card.h).toBeCloseTo(256 / 224, 5)
  })

  describe('on a map too short for 70 percent to be much of a growth', () => {
    it('grows it, up to 90 percent of the frame, when 70 percent would be under 1.25 times the card', () => {
      // 300 high: 70 percent is 210, a card of 224 would shrink. 90 percent is 270.
      const r = full(undefined, card, { w: 1076, h: 300 })
      expect(r.card.h).toBeCloseTo(270)
      expect(r.k).toBeCloseTo(270 / 224, 5)
      expect(r.k).toBeGreaterThan(1)
      fits(r.card, { w: 1076, h: 300 })
    })

    it('does so on a 416 map with a note, the card 256 high', () => {
      const withNote = { w: 256, h: 256 }
      const r = full(undefined, { x: 100, y: 50, w: 256, h: 256 }, { w: 1076, h: 416 }, withNote)
      // 70 percent of 416 is 291, which is under 1.25 times 256 (320): 90 percent is 374.
      expect(r.card.h).toBeCloseTo(374.4)
      expect(r.k).toBeCloseTo(1.4625)
      fits(r.card, { w: 1076, h: 416 })
    })

    it('does so for a card 264 high', () => {
      const tall = { w: 256, h: 264 }
      const r = full(undefined, { x: 100, y: 50, w: 256, h: 264 }, { w: 1076, h: 416 }, tall)
      expect(r.card.h).toBeCloseTo(374.4)
      expect(r.k).toBeGreaterThan(1.4)
      fits(r.card, { w: 1076, h: 416 })
    })

    it('never makes the card smaller than it stands on the map, and always fits the frame', () => {
      for (const h of [120, 200, 224, 260, 300, 416, 800]) {
        const r = full(undefined, card, { w: 1076, h })
        if (h >= 224) expect(r.card.w).toBeGreaterThanOrEqual(256 - 1e-6)
        fits(r.card, { w: 1076, h })
      }
      // Already made smaller by a short map: it grows from there.
      const small = { x: 300, y: 20, w: 230, h: 201.6 }
      const r = full(undefined, small, { w: 1076, h: 260 }, face)
      expect(r.card.w).toBeGreaterThan(small.w)
      fits(r.card, { w: 1076, h: 260 })
    })
  })

  it('fits inside a short, wide map: 1076 by 416, in the middle', () => {
    const r = full(undefined, card, { w: 1076, h: 416 })
    fits(r.card, { w: 1076, h: 416 })
    expect(r.card.x + r.card.w / 2).toBeCloseTo(538)
    expect(r.card.y + r.card.h / 2).toBeCloseTo(208)
  })

  it('fits a tall frame by its width, in portrait', () => {
    const r = full(undefined, card, { w: 1080, h: 1920 })
    // 70 percent of 1080 wide is 756, and the card is 256 by 224: 662 high.
    expect(r.card.w).toBeCloseTo(756)
    expect(r.card.h).toBeCloseTo(661.5, 0)
    expect(r.card.x).toBeCloseTo(162)
    fits(r.card, { w: 1080, h: 1920 })
    const narrow = full(undefined, card, { w: 400, h: 1000 })
    // 70 percent of 400 is 280, barely more than the card: it takes up to 90 percent, 360.
    expect(narrow.card.w).toBeCloseTo(360)
    fits(narrow.card, { w: 400, h: 1000 })
  })

  it('fits a text-only card too, by its own shape', () => {
    const text = { w: 224, h: 56 }
    const r = momentRect(momentView('a', MOMENT_GROW_S + 0.1, false), { x: 10, y: 10, w: 224, h: 56 }, text, frame)
    // 70 percent of the 1000 wide frame, the shape of a wide, low card.
    expect(r.card.w).toBeCloseTo(700)
    expect(r.card.h).toBeCloseTo(175)
    fits(r.card, frame)
  })

  it('is between the two while it grows and shrinks, and the same either way round', () => {
    const grow = momentRect(momentView('a', 0.2, false), card, face, { w: 1000, h: 600 })
    expect(grow.card.w).toBeGreaterThan(card.w)
    expect(grow.card.w).toBeLessThan(480.1)
    expect(grow.card.x).toBeGreaterThan(card.x)
    const shrink = momentRect(momentView('a', MOMENT_TOTAL_S - 0.2, false), card, face, { w: 1000, h: 600 })
    expect(shrink.card.w).toBeCloseTo(grow.card.w, 3)
  })

  it('carries the slow zoom, for the photo inside its frame alone', () => {
    const early = full(momentView('a', MOMENT_GROW_S + 0.1, false))
    const late = full(momentView('a', MOMENT_GROW_S + 1.3, false))
    expect(late.zoom).toBeGreaterThan(early.zoom)
    expect(late.zoom).toBeLessThanOrEqual(1.06)
    // The card does not move through the hold.
    expect(late.card).toEqual(early.card)
    expect(late.k).toBe(early.k)
  })

  it('is full from the first moment with reduced motion, and still', () => {
    const r = momentRect(momentView('a', 0, true), card, face, { w: 1000, h: 600 })
    expect(r.card.h).toBeCloseTo(420)
    expect(r.zoom).toBe(1)
    expect(momentRect(momentView('a', 1.2, true), card, face, { w: 1000, h: 600 })).toEqual(r)
  })

  it('depends on its inputs alone', () => {
    const v = momentView('a', 0.3, false)
    const a = momentRect(v, card, face, frame)
    momentRect(momentView('a', 1, false), { x: 0, y: 0, w: 1, h: 1 }, { w: 1, h: 1 }, { w: 10, h: 10 })
    expect(momentRect(v, card, face, frame)).toEqual(a)
  })
})

describe('momentGrowth', () => {
  it('is the 70 percent fill where that is a real growth', () => {
    expect(momentGrowth({ w: 256, h: 224 }, { w: 1000, h: 600 })).toBeCloseTo(1.875)
  })
  it('is never below what the card already is, nor beyond the frame', () => {
    expect(momentGrowth({ w: 256, h: 224 }, { w: 1076, h: 200 }, 0.8)).toBeGreaterThanOrEqual(0.8)
    expect(momentGrowth({ w: 256, h: 224 }, { w: 1076, h: 200 }, 0.8) * 224).toBeLessThanOrEqual(200)
    expect(momentGrowth({ w: 256, h: 224 }, { w: 100, h: 100 }) * 256).toBeLessThanOrEqual(100)
  })
})

describe('playedIds', () => {
  const timeline = { entries: [{ id: 'a', offset: 0 }, { id: 'b', offset: 500 }] }
  it('is the passages the replay has begun by the clock', () => {
    expect([...playedIds(timeline, 0)]).toEqual(['a'])
    expect([...playedIds(timeline, 500)]).toEqual(['a', 'b'])
  })
})

import { describe, expect, it } from 'vitest'

import { buildTimeline, type TimedSegment } from '@/lib/passages'
import { MAX_SIGN_CARDS, SIGN_LEAD_S, momentsOf, placeSignCard, resolveSigns, signCardBox, signCardPlacement, visibleSigns, type CardMode, type ReplaySign } from '@/lib/replay-signs'

const sign = (id: string, t: number, extra: Partial<ReplaySign> = {}): ReplaySign => ({
  id, t_ms: t * 1000, title: id, note: '', document_id: null, photo_url: null, visibility: 'passing', show_s: 4, starts_leg: false, photo_moment: null, ...extra,
})

// One passage, 1000 s long starting at t=10000, moving east a little each 100 s.
const seg: TimedSegment = Array.from({ length: 11 }, (_, i) => ({ t: 10000 + i * 100, lat: -20, lon: 149 + i * 0.01 })) as TimedSegment
const timeline = buildTimeline([{ id: 'p1', start: 10000, end: 11000, segments: [seg] }])
// 1000 timeline s in 100 s of watching: 10 timeline s per wall second.
const wallAtClock = (clock: number) => clock / 10

describe('resolveSigns', () => {
  it('puts each sign where the track is at its time, oldest first', () => {
    const r = resolveSigns([sign('b', 10500), sign('a', 10100)], timeline)
    expect(r.map((x) => x.sign.id)).toEqual(['a', 'b'])
    expect(r[0].clock).toBeCloseTo(100)
    expect(r[1].lon).toBeCloseTo(149.05)
    expect(r[1].lat).toBeCloseTo(-20)
    expect(r[1].entryId).toBe('p1')
  })

  it('leaves out a sign whose time is outside every replayed passage, without deleting anything', () => {
    const signs = [sign('before', 9000), sign('in', 10500), sign('after', 12000)]
    expect(resolveSigns(signs, timeline).map((x) => x.sign.id)).toEqual(['in'])
    expect(signs).toHaveLength(3)
  })

  it('follows the time across two passages end to end', () => {
    const seg2: TimedSegment = [{ t: 50000, lat: -21, lon: 150 }, { t: 50500, lat: -21, lon: 150.5 }] as TimedSegment
    const two = buildTimeline([{ id: 'p1', start: 10000, end: 11000, segments: [seg] }, { id: 'p2', start: 50000, end: 50500, segments: [seg2] }])
    const r = resolveSigns([sign('x', 50250)], two)
    expect(r[0].entryId).toBe('p2')
    expect(r[0].clock).toBeCloseTo(1250)
  })
})

function at(signs: ReplaySign[], wall: number): string[] {
  return visibleSigns(resolveSigns(signs, timeline), wall, wallAtClock).map((r) => r.sign.id)
}

describe('visibleSigns', () => {
  // A sign at t=10500 is reached at wall second 50.
  it('while passing: appears shortly before the boat reaches it and stays N watched seconds', () => {
    const s = [sign('a', 10500, { show_s: 6 })]
    expect(at(s, 50 - SIGN_LEAD_S - 0.01)).toEqual([])
    expect(at(s, 50 - SIGN_LEAD_S)).toEqual(['a'])
    expect(at(s, 50)).toEqual(['a'])
    expect(at(s, 55.9)).toEqual(['a'])
    expect(at(s, 56)).toEqual([])
    expect(at(s, 99)).toEqual([])
  })

  it('after: appears when the boat reaches it and stays for the rest of the replay', () => {
    const s = [sign('a', 10500, { visibility: 'after' })]
    expect(at(s, 49.9)).toEqual([])
    expect(at(s, 50)).toEqual(['a'])
    expect(at(s, 100)).toEqual(['a'])
  })

  it('always: shown for the whole replay, even before the boat arrives', () => {
    const s = [sign('a', 10500, { visibility: 'always' })]
    expect(at(s, 0)).toEqual(['a'])
    expect(at(s, 100)).toEqual(['a'])
  })

  it('watched time is wall-clock: a slower stretch keeps the sign up for the same seconds', () => {
    // Half pace from clock 0: 20 s of watching per 100 timeline s... the sign
    // at clock 500 is reached at wall 100 instead of 50, and still stays 4 s.
    const slow = (clock: number) => clock / 5
    const r = resolveSigns([sign('a', 10500)], timeline)
    expect(visibleSigns(r, 99.5, slow).map((x) => x.sign.id)).toEqual(['a'])
    expect(visibleSigns(r, 103.9, slow).map((x) => x.sign.id)).toEqual(['a'])
    expect(visibleSigns(r, 104, slow)).toEqual([])
    expect(visibleSigns(r, 60, slow)).toEqual([])
  })

  it('shows at most two; when a third becomes due the oldest gives way', () => {
    expect(MAX_SIGN_CARDS).toBe(2)
    const s = [sign('a', 10500, { show_s: 30 }), sign('b', 10520, { show_s: 30 }), sign('c', 10540, { show_s: 30 })]
    expect(at(s, 50.1)).toEqual(['a'])
    expect(at(s, 52.1)).toEqual(['a', 'b'])
    expect(at(s, 54.1)).toEqual(['b', 'c'])
    expect(at(s, 54.1)).toHaveLength(2)
  })

  it('an always sign counts toward the two and is never the one that gives way', () => {
    const one = [sign('k', 10100, { visibility: 'always' }), sign('a', 10500, { show_s: 30 }), sign('b', 10520, { show_s: 30 })]
    expect(at(one, 49)).toEqual(['k'])
    expect(at(one, 50.1)).toEqual(['k', 'a'])
    expect(at(one, 52.1)).toEqual(['k', 'b'])
  })

  it('with two always signs nothing else shows', () => {
    const s = [sign('k1', 10100, { visibility: 'always' }), sign('k2', 10200, { visibility: 'always' }), sign('a', 10500, { show_s: 30 }), sign('z', 10600, { visibility: 'after' })]
    expect(at(s, 0)).toEqual(['k1', 'k2'])
    expect(at(s, 55)).toEqual(['k1', 'k2'])
    expect(at(s, 99)).toEqual(['k1', 'k2'])
  })

  it('with more than two always signs the earliest two show', () => {
    const s = [sign('k3', 10300, { visibility: 'always' }), sign('k1', 10100, { visibility: 'always' }), sign('k2', 10200, { visibility: 'always' })]
    expect(at(s, 10)).toEqual(['k1', 'k2'])
  })

  it('a sign that has given way does not come back when the newer ones leave', () => {
    // a stays "after" but is the oldest when c arrives, so it yields to b and c for good.
    const s = [sign('a', 10500, { visibility: 'after' }), sign('b', 10520, { show_s: 6 }), sign('c', 10540, { show_s: 6 })]
    expect(at(s, 54.1)).toEqual(['b', 'c'])
    expect(at(s, 61)).toEqual([])
    // and a passing one that was pushed out stays out the rest of its time
    const p = [sign('x', 10500, { show_s: 30 }), sign('y', 10520, { show_s: 4 }), sign('z', 10540, { show_s: 4 })]
    expect(at(p, 54.1)).toEqual(['y', 'z'])
    expect(at(p, 57)).toEqual(['z'])
    expect(at(p, 59)).toEqual([])
  })

  it('a seek shows exactly what continuous play would have', () => {
    const s = [sign('a', 10500, { show_s: 30 }), sign('b', 10520, { show_s: 30 }), sign('c', 10540, { show_s: 30 }), sign('k', 10300, { visibility: 'always' })]
    const r = resolveSigns(s, timeline)
    const frames: string[][] = []
    for (let w = 0; w <= 100; w += 0.25) frames.push(visibleSigns(r, w, wallAtClock).map((x) => x.sign.id))
    // Each frame depends only on its own time, never on the one before.
    for (const w of [10, 49.5, 53, 54.5, 70]) expect(visibleSigns(r, w, wallAtClock).map((x) => x.sign.id)).toEqual(frames[Math.round(w / 0.25)])
    for (const f of frames) expect(f.length).toBeLessThanOrEqual(2)
  })
})

describe('placeSignCard', () => {
  const container = { w: 800, h: 600 }
  const card = { w: 256, h: 220 }
  const base = { container, card, lift: 20 }
  const inside = (r: { x: number; y: number; w: number; h: number }, c = container) => {
    expect(r.x).toBeGreaterThanOrEqual(-1e-6)
    expect(r.x + r.w).toBeLessThanOrEqual(c.w + 1e-6)
    expect(r.y).toBeGreaterThanOrEqual(6 - 1e-6)
    expect(r.y + r.h).toBeLessThanOrEqual(c.h - 6 + 1e-6)
  }

  it('leaves a card alone when it fits above its point, the leader running up to it', () => {
    const r = placeSignCard({ ...base, point: { x: 400, y: 400 } })
    expect(r).toMatchObject({ mode: 'above', scale: 1, hidden: false, rect: { x: 272, y: 156, w: 256, h: 220 } })
    expect(r.leader).toEqual({ x1: 400, y1: 400, x2: 400, y2: 376 })
  })

  it('slides a card left past the right edge and right past the left edge, the leader still on it', () => {
    const right = placeSignCard({ ...base, point: { x: 780, y: 400 } })
    expect(right.rect.x).toBe(800 - 6 - 256)
    expect(right.leader!.x2).toBe(780)
    expect(right.leader!.x2).toBeLessThanOrEqual(right.rect.x + right.rect.w)
    const left = placeSignCard({ ...base, point: { x: 20, y: 400 } })
    expect(left.rect.x).toBe(6)
    expect(left.leader!.x2).toBe(20)
  })

  it('puts a card beside a point at the very edge of the map, where the leader could not meet a card that stays whole', () => {
    const r = placeSignCard({ ...base, point: { x: 2, y: 400 } })
    expect(r.mode).toBe('right')
    inside(r.rect)
    expect(r.leader).not.toBeNull()
  })

  it('keeps a card whole when its point has just crossed the edge, with no leader to run', () => {
    for (const x of [-1, -10, -40, 801, 810, 840]) {
      const r = placeSignCard({ ...base, point: { x, y: 400 } })
      inside(r.rect)
      expect(r.hidden).toBe(false)
      expect(r.leader).toBeNull()
    }
    const above = placeSignCard({ ...base, point: { x: 400, y: -20 } })
    inside(above.rect)
    expect(above.leader).toBeNull()
  })

  it('shows no card for a point far off the map, which would be off it with its point', () => {
    for (const point of [{ x: -500, y: 400 }, { x: 1500, y: 400 }, { x: 400, y: -900 }, { x: 400, y: 1900 }]) {
      expect(placeSignCard({ ...base, point }).hidden).toBe(true)
    }
  })

  it('shrinks a card wider than the map to fit it, and copes with a tiny one', () => {
    const r = placeSignCard({ ...base, container: { w: 200, h: 600 }, point: { x: 50, y: 400 } })
    inside(r.rect, { w: 200, h: 600 })
    expect(r.scale).toBeLessThan(1)
    expect(placeSignCard({ ...base, card: { w: 4, h: 220 }, point: { x: 790, y: 400 } }).hidden).toBe(false)
  })

  it('leaves the card as preferred while its size or the map size is unknown', () => {
    expect(placeSignCard({ ...base, card: { w: 0, h: 0 }, point: { x: 790, y: 10 } })).toMatchObject({ mode: 'above', scale: 1, hidden: false })
    const r = placeSignCard({ ...base, container: { w: 0, h: 0 }, point: { x: 790, y: 10 } })
    expect(r.mode).toBe('above')
    expect(r.leader).toEqual({ x1: 790, y1: 10, x2: 790, y2: 10 - 4 - 20 })
  })

  it('puts the card below its point when there is no room above', () => {
    const r = placeSignCard({ ...base, point: { x: 400, y: 120 } })
    expect(r.mode).toBe('below')
    expect(r.rect.y).toBe(120 + 4 + 20)
    expect(r.leader).toEqual({ x1: 400, y1: 120, x2: 400, y2: 144 })
  })

  it('goes beside its point when there is room neither above nor below, centred on it as far as the map allows', () => {
    const r = placeSignCard({ ...base, container: { w: 800, h: 300 }, point: { x: 400, y: 200 } })
    expect(r.mode).toBe('right')
    expect(r.scale).toBe(1)
    expect(r.rect).toEqual({ x: 420, y: 74, w: 256, h: 220 })
    expect(r.leader).toEqual({ x1: 400, y1: 200, x2: 420, y2: 200 })
  })

  it('keeps the top margin clear: a card does not run under the map controls', () => {
    // room above = 260 - 220 - 48 = -8: not enough, so it goes below
    expect(placeSignCard({ ...base, point: { x: 400, y: 260 } }).mode).toBe('below')
    // 48 + 220 + 8 above the point is just enough, with the leader shortened to 8
    const r = placeSignCard({ ...base, point: { x: 400, y: 276 } })
    expect(r.mode).toBe('above')
    expect(r.leader!.y1 - r.leader!.y2).toBe(12)
  })

  it('does not drop the top margin to put a card beside its point on a short map: it is made smaller instead', () => {
    const c = { w: 1076, h: 260 }
    const r = placeSignCard({ container: c, card: { w: 256, h: 224 }, lift: 20, point: { x: 538, y: 130 } })
    expect(r.scale).toBeLessThan(1)
    expect(r.rect.y).toBeGreaterThanOrEqual(48)
    inside(r.rect, c)
    // The same card on a map tall enough keeps its full size and the margin.
    const tall = placeSignCard({ container: { w: 1076, h: 300 }, card: { w: 256, h: 224 }, lift: 20, point: { x: 538, y: 150 } })
    expect(tall.scale).toBe(1)
    expect(tall.rect.y).toBeGreaterThanOrEqual(48)
  })

  it('shortens the second card leader so it fits a 416px map', () => {
    const r = placeSignCard({ container: { w: 800, h: 416 }, card, lift: 104, point: { x: 400, y: 300 } })
    expect(r.mode).toBe('above')
    expect(r.leader!.y1 - r.leader!.y2).toBe(32 + 4)
    expect(r.rect.y).toBeGreaterThanOrEqual(44)
  })
})

describe('momentsOf', () => {
  const placed = (id: string, clock: number, extra: Partial<ReplaySign> = {}) => ({
    sign: sign(id, 0, { photo_url: '/p', document_id: 'd', ...extra }), clock, at: 0, entryId: 'p1', lat: 0, lon: 0,
  })

  it('lists the signs whose photos hold the replay, where the boat meets them', () => {
    expect(momentsOf([placed('a', 100), placed('b', 300)])).toEqual([{ clock: 100, id: 'a', at: 0 }, { clock: 300, id: 'b', at: 0 }])
  })

  it('leaves out a sign without a photo, one switched off, and one with no title yet', () => {
    expect(momentsOf([
      placed('nophoto', 1, { photo_url: null, document_id: null }),
      placed('off', 2, { photo_moment: false }),
      placed('untitled', 3, { title: '' }),
      placed('on', 4, { photo_moment: true }),
    ])).toEqual([{ clock: 4, id: 'on', at: 0 }])
  })
})

describe('a card keeps the place it has while it fits', () => {
  const c = { w: 1076, h: 416 }
  const card = { w: 256, h: 224 }
  const at = (x: number, y: number, previous?: CardMode) => placeSignCard({ container: c, card, lift: 20, point: { x, y }, previous })

  it('does not flip across the boat as the camera dithers about the middle of the map', () => {
    // With no memory the side follows whichever has more room.
    expect([at(530, 215).mode, at(546, 215).mode]).toEqual(['right', 'left'])
    // With it, a card put on the right stays there across the middle.
    let mode: CardMode | undefined
    const seen = [530, 538, 546, 550, 541, 533, 538].map((x) => { mode = at(x, 215, mode).mode; return mode })
    expect(new Set(seen)).toEqual(new Set(['right']))
  })

  it('leaves the side it has only when that side runs out of room', () => {
    let mode: CardMode | undefined = at(500, 215).mode
    expect(mode).toBe('right')
    // Still room on the right for a card standing 20 px off the point: until the point is 6 + 256 + 20 from the edge.
    expect(at(1076 - 6 - 256 - 20, 215, mode).mode).toBe('right')
    expect(at(1076 - 6 - 256 - 20 + 2, 215, mode).mode).toBe('left')
  })

  it('does not hop between above and below on small moves of the camera', () => {
    const tall = { w: 800, h: 600 }
    const put = (y: number, previous?: CardMode) => placeSignCard({ container: tall, card: { w: 256, h: 220 }, lift: 20, point: { x: 400, y }, previous })
    // At y=300 both fit; above wins with no memory, and below is kept when that is where it was.
    expect(put(300).mode).toBe('above')
    expect(put(300, 'below').mode).toBe('below')
    let mode: CardMode | undefined = 'below'
    for (const y of [300, 290, 310, 296, 304]) mode = put(y, mode).mode
    expect(mode).toBe('below')
    // Out of room below, it goes above.
    expect(put(560, 'below').mode).toBe('above')
  })

  it('does not hop between beside and above either', () => {
    const put = (y: number, previous?: CardMode) => placeSignCard({ container: c, card, lift: 20, point: { x: 538, y }, previous })
    // y=330: above has room (330-224-48 = 58). Put beside, it stays beside.
    expect(put(330).mode).toBe('above')
    expect(put(330, 'right').mode).toBe('right')
    // Run out of room beside (too tall for the map): back to above.
    const short = placeSignCard({ container: { w: 1076, h: 260 }, card, lift: 20, point: { x: 538, y: 250 }, previous: 'right' })
    expect(short.mode === 'right' ? short.scale : 1).toBeLessThan(1.0001)
  })

  it('forgets a place that no longer exists', () => {
    // A previous 'left' on a point at the left edge cannot be had.
    expect(at(60, 215, 'left').mode).toBe('right')
  })
})

describe('signCardBox', () => {
  it('is 256 wide with a photo and 224 with text alone, as the card is', () => {
    expect(signCardBox(sign('a', 0, { photo_url: '/p' }), 1000, 0).w).toBe(256)
    expect(signCardBox(sign('a', 0), 1000, 0).w).toBe(224)
  })

  it('has the photo at 4 by 3 inside the padding, and the text under it', () => {
    const b = signCardBox(sign('a', 0, { photo_url: '/p' }), 1000, 0)
    expect([b.photoW, b.photoH]).toEqual([240, 180])
    // padding, photo, gap, one line of title, padding
    expect(b.h).toBe(8 + 180 + 8 + 20 + 8)
  })

  it('grows a line for each line of the note it is given', () => {
    const h = (lines: number) => signCardBox(sign('a', 0, { photo_url: '/p' }), 1000, lines).h
    expect(h(1)).toBe(h(0) + 16)
    expect(h(2)).toBe(h(0) + 32)
  })

  it('narrows to the map, with its margins', () => {
    expect(signCardBox(sign('a', 0, { photo_url: '/p' }), 200, 0).w).toBe(188)
  })
})

// ── always whole inside the map ───────────────────────────────────────────

describe('a sign card is always whole inside the map', () => {
  const photoSign = (extra: Partial<ReplaySign> = {}) => sign('a', 0, { photo_url: '/p', document_id: 'd', title: 'Everyone is heading south for summer', ...extra })
  /** The card's rectangle must lie within the map, less the margin kept above the foot and below the controls. */
  const inside = (r: { x: number; y: number; w: number; h: number }, c: { w: number; h: number }) => {
    expect(r.x).toBeGreaterThanOrEqual(-1e-6)
    expect(r.x + r.w).toBeLessThanOrEqual(c.w + 1e-6)
    expect(r.y).toBeGreaterThanOrEqual(6 - 1e-6)
    expect(r.y + r.h).toBeLessThanOrEqual(c.h - 6 + 1e-6)
  }
  const sweep = (c: { w: number; h: number }, sg: ReplaySign, lines = 0, lift = 20, margin = 0) => {
    const out: { x: number; y: number; p: ReturnType<typeof signCardPlacement> }[] = []
    for (let y = -margin; y <= c.h + margin; y += c.h / 40) for (let x = -margin; x <= c.w + margin; x += c.w / 20) out.push({ x, y, p: signCardPlacement(sg, { x, y }, c, lift, lines) })
    return out
  }

  it('does not cut it off on a 1076 by 416 map, with the boat in the lower half (the reproduced case)', () => {
    const c = { w: 1076, h: 416 }
    const sg = photoSign()
    // The boat at 215 used to put a 224 high card under it, running 35 px off the foot of the map.
    for (const y of [190, 200, 215, 228, 240]) inside(signCardPlacement(sg, { x: 538, y }, c, 20, 0).rect, c)
    const r = signCardPlacement(sg, { x: 538, y: 215 }, c, 20, 0)
    // Neither above nor below fits whole, so it stands beside the boat, at its full size.
    expect(['left', 'right']).toContain(r.mode)
    expect(r.scale).toBe(1)
    expect(r.rect.h).toBe(224)
  })

  it('keeps the whole card, photo and caption, in the map wherever the boat is on that map', () => {
    for (const { p } of sweep({ w: 1076, h: 416 }, photoSign())) inside(p.rect, { w: 1076, h: 416 })
  })

  it('does so for a card with a note and a long title: two lines of note', () => {
    const c = { w: 1076, h: 416 }
    const sg = photoSign({ title: 'Everyone is heading south for summer, and so are we, to the reef', note: 'The wind is behind us and the swell is easing off now.' })
    expect(signCardBox(sg, c.w, 2).h).toBe(2 * 8 + 180 + 8 + 20 + 32)
    for (const { p } of sweep(c, sg, 2)) inside(p.rect, c)
  })

  it('keeps it whole for a point just past the edge, too', () => {
    const c = { w: 1076, h: 416 }
    for (const { p } of sweep(c, photoSign(), 0, 20, 30)) if (!p.hidden) inside(p.rect, c)
  })

  it('scales it down to fit, instead of cutting it off, on a map too short to hold it', () => {
    const c = { w: 1076, h: 200 }
    const sg = photoSign()
    for (const { p } of sweep(c, sg)) {
      inside(p.rect, c)
      expect(p.scale).toBeLessThan(1)
    }
    // Still the same card in miniature: its shape is kept.
    const r = signCardPlacement(sg, { x: 500, y: 100 }, c, 20, 0).rect
    expect(r.w / r.h).toBeCloseTo(256 / 224, 5)
    inside(signCardPlacement(sg, { x: 500, y: 40 }, { w: 1076, h: 90 }, 20, 0).rect, { w: 1076, h: 90 })
  })

  it('does so in portrait, wide enough for it or not', () => {
    for (const c of [{ w: 1080, h: 1920 }, { w: 360, h: 640 }, { w: 240, h: 640 }]) {
      for (const { p } of sweep(c, photoSign())) inside(p.rect, c)
    }
  })

  it('changes nothing for a card that has room', () => {
    const r = signCardPlacement(photoSign(), { x: 538, y: 500 }, { w: 1076, h: 800 }, 20, 0)
    expect(r).toMatchObject({ mode: 'above', scale: 1, rect: { h: 224 } })
  })
})

import { describe, expect, it } from 'vitest'

import { buildTimeline, type TimedSegment } from '@/lib/passages'
import { MAX_SIGN_CARDS, SIGN_LEAD_S, momentsOf, placeSignCard, resolveSigns, signCardBox, signCardPhotoRect, visibleSigns, type ReplaySign } from '@/lib/replay-signs'

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

  it('leaves a card alone when it fits above its point', () => {
    expect(placeSignCard({ ...base, point: { x: 400, y: 400 } })).toEqual({ dx: 0, below: false, leader: 20 })
  })

  it('shifts a card left past the right edge and right past the left edge', () => {
    // card centred on 780 ends at 908; it must end at 794
    expect(placeSignCard({ ...base, point: { x: 780, y: 400 } }).dx).toBe(-114)
    expect(placeSignCard({ ...base, point: { x: 20, y: 400 } }).dx).toBe(114)
  })

  it('never shifts so far that the leader leaves the card', () => {
    expect(placeSignCard({ ...base, point: { x: -500, y: 400 } }).dx).toBe(122)
    expect(placeSignCard({ ...base, point: { x: 1500, y: 400 } }).dx).toBe(-122)
  })

  it('centres a card wider than the map, and copes with a tiny one', () => {
    expect(placeSignCard({ ...base, container: { w: 200, h: 600 }, point: { x: 50, y: 400 } }).dx).toBe(0)
    expect(placeSignCard({ ...base, card: { w: 4, h: 220 }, point: { x: 790, y: 400 } }).dx).toBe(0)
  })

  it('leaves the card as preferred while its size or the map size is unknown', () => {
    expect(placeSignCard({ ...base, card: { w: 0, h: 0 }, point: { x: 790, y: 10 } })).toEqual({ dx: 0, below: false, leader: 20 })
    expect(placeSignCard({ ...base, container: { w: 0, h: 0 }, point: { x: 790, y: 10 } })).toEqual({ dx: 0, below: false, leader: 20 })
  })

  it('puts the card below its point when there is no room above', () => {
    const r = placeSignCard({ ...base, point: { x: 400, y: 120 } })
    expect(r.below).toBe(true)
    expect(r.leader).toBe(20)
  })

  it('stays above, leader squeezed to the minimum, when neither side has room', () => {
    expect(placeSignCard({ ...base, container: { w: 800, h: 300 }, point: { x: 400, y: 200 } })).toMatchObject({ below: false, leader: 8 })
  })

  it('keeps the top margin clear: a card does not run under the map controls', () => {
    // room above = 260 - 220 - 48 = -8: not enough, so it goes below
    expect(placeSignCard({ ...base, point: { x: 400, y: 260 } }).below).toBe(true)
    // 48 + 220 + 8 above the point is just enough, with the leader shortened to 8
    expect(placeSignCard({ ...base, point: { x: 400, y: 276 } })).toMatchObject({ below: false, leader: 8 })
  })

  it('shortens the second card leader so it fits a 416px map', () => {
    const r = placeSignCard({ container: { w: 800, h: 416 }, card, lift: 104, point: { x: 400, y: 300 } })
    expect(r.below).toBe(false)
    expect(r.leader).toBe(32)
    expect(300 - r.leader - 220).toBeGreaterThanOrEqual(48)
  })
})

describe('momentsOf', () => {
  const placed = (id: string, clock: number, extra: Partial<ReplaySign> = {}) => ({
    sign: sign(id, 0, { photo_url: '/p', document_id: 'd', ...extra }), clock, at: 0, entryId: 'p1', lat: 0, lon: 0,
  })

  it('lists the signs whose photos hold the replay, where the boat meets them', () => {
    expect(momentsOf([placed('a', 100), placed('b', 300)])).toEqual([{ clock: 100, id: 'a' }, { clock: 300, id: 'b' }])
  })

  it('leaves out a sign without a photo, one switched off, and one with no title yet', () => {
    expect(momentsOf([
      placed('nophoto', 1, { photo_url: null, document_id: null }),
      placed('off', 2, { photo_moment: false }),
      placed('untitled', 3, { title: '' }),
      placed('on', 4, { photo_moment: true }),
    ])).toEqual([{ clock: 4, id: 'on' }])
  })
})

describe('signCardPhotoRect', () => {
  const container = { w: 1000, h: 400 }
  const at = (point: { x: number; y: number }, extra: Partial<ReplaySign> = {}, lift = 20, box = container) =>
    signCardPhotoRect(sign('a', 0, { photo_url: '/p', document_id: 'd', ...extra }), point, box, lift)

  it('is the photo inside a card that floats above its point on the leader', () => {
    // 256 wide with 8 around the photo: 240 by 180. The card is centred over the point.
    expect(at({ x: 500, y: 300 })).toEqual({ x: 380, y: 60, w: 240, h: 180 })
  })

  it('moves up with a note, which makes the card taller', () => {
    const noted = at({ x: 500, y: 300 }, { note: 'A humpback off the bow, very close' })
    expect(noted.y).toBeLessThan(60)
    expect(noted.h).toBe(180)
  })

  it('floats higher for a card in the second place', () => {
    expect(at({ x: 500, y: 300 }, {}, 104).y).toBeLessThan(at({ x: 500, y: 300 }).y)
  })

  it('goes under its point when there is no room above it', () => {
    const r = at({ x: 500, y: 60 })
    expect(r.y).toBeGreaterThan(60)
  })

  it('slides along to stay inside the map', () => {
    const r = at({ x: 20, y: 300 })
    expect(r.x).toBeGreaterThanOrEqual(6)
    expect(at({ x: 990, y: 300 }).x + 240).toBeLessThanOrEqual(1000 - 6 + 8)
  })

  it('narrows to a narrow map, as the card does', () => {
    expect(at({ x: 100, y: 300 }, {}, 20, { w: 200, h: 400 }).w).toBe(200 - 2 * 6 - 2 * 8)
  })
})

describe('signCardBox', () => {
  it('is 256 wide with a photo and 224 with text alone, as the card is', () => {
    expect(signCardBox(sign('a', 0, { photo_url: '/p' }), 1000).w).toBe(256)
    expect(signCardBox(sign('a', 0), 1000).w).toBe(224)
  })

  it('has the photo at 4 by 3 inside the padding, and the text under it', () => {
    const b = signCardBox(sign('a', 0, { photo_url: '/p' }), 1000)
    expect([b.photoW, b.photoH]).toEqual([240, 180])
    // padding, photo, gap, one line of title, padding
    expect(b.h).toBe(8 + 180 + 8 + 20 + 8)
  })

  it('grows a line for each line of the note, to two', () => {
    const h = (note: string) => signCardBox(sign('a', 0, { photo_url: '/p', note }), 1000).h
    expect(h('Short')).toBe(h('') + 16)
    expect(h('x'.repeat(60))).toBe(h('') + 32)
    expect(h('x'.repeat(500))).toBe(h('') + 32)
    expect(h('one\ntwo\nthree')).toBe(h('') + 32)
  })

  it('narrows to the map, with its margins', () => {
    expect(signCardBox(sign('a', 0, { photo_url: '/p' }), 200).w).toBe(188)
  })
})

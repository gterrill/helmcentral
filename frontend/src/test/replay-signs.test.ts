import { describe, expect, it } from 'vitest'

import { buildTimeline, type TimedSegment } from '@/lib/passages'
import { MAX_SIGN_CARDS, SIGN_LEAD_S, resolveSigns, visibleSigns, type ReplaySign } from '@/lib/replay-signs'

const sign = (id: string, t: number, extra: Partial<ReplaySign> = {}): ReplaySign => ({
  id, t_ms: t * 1000, title: id, note: '', document_id: null, photo_url: null, visibility: 'passing', show_s: 4, ...extra,
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

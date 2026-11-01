import { describe, expect, it } from 'vitest'

import { CARD_FADE_S, UI_SCALE, buildOverlay, type OverlayInput } from '@/lib/passage-video/overlay'
import { signCardBox, signCardPhotoRect, signCardRect, SIGN_LIFT_PX, type ReplaySign, type ResolvedSign } from '@/lib/replay-signs'
import type { ReplayFrame } from '@/lib/replay-controller'
import { MOMENT_GROW_S, momentRect, momentView } from '@/lib/replay-time'

const sign = (id: string, extra: Partial<ReplaySign> = {}): ReplaySign => ({
  id, t_ms: 0, title: id, note: '', document_id: null, photo_url: null, visibility: 'passing', show_s: 4, starts_leg: false, photo_moment: null, ...extra,
})
const placed = (s: ReplaySign, lon = 149, lat = -20): ResolvedSign => ({ sign: s, clock: 0, at: 0, entryId: 'p', lat, lon })
const photo = { document_id: 'd', photo_url: '/api/documents/d/content' }

const frame = (extra: Partial<ReplayFrame> = {}): ReplayFrame => ({
  here: null, boat: { lat: -20, lon: 149 }, heading: null, sailedTo: null, cards: [], pins: [], light: null, speed: null, moment: null, ...extra,
})

// The map, 1920 by 1080, putting a point at a pixel by its longitude.
const SIZE = { width: 1920, height: 1080 }
const project = (lon: number, lat: number) => ({ x: (lon - 148) * 600, y: 700 + lat * 5 })
const input = (f: ReplayFrame, extra: Partial<OverlayInput> = {}): OverlayInput => ({
  frame: f, size: SIZE, project, wall: 10, firstSeen: new Map(), bitmaps: new Set(), momentSign: undefined, boatDot: false, measure: (t) => t.length * 7, ...extra,
})

describe('buildOverlay', () => {
  it('sizes a card from the note as it is wrapped, so a second line is inside the card', () => {
    // 33 characters: one line by the guess of 36 to a line, but two at 7 px a character in 208 px.
    const note = 'A humpback and her calf off bow'.padEnd(33, 'x')
    const a = placed(sign('a', { note }))
    const m = buildOverlay(input(frame({ cards: [a] })))
    const card = m.cards[0]
    expect(card.noteLines.length).toBe(2)
    expect(card.noteLines.join(' ').replace(/…/g, '')).toContain('humpback')
    expect(card.rect.h).toBe(signCardBox(a.sign, m.design.w, 2).h)
    expect(card.rect.h).toBeGreaterThan(signCardBox(a.sign, m.design.w).h)
  })

  it('keeps a short note on one line, and no note on none', () => {
    const m = buildOverlay(input(frame({ cards: [placed(sign('a', { note: 'Whale' })), placed(sign('b'))] })))
    expect(m.cards.map((c) => c.noteLines)).toEqual([['Whale'], []])
  })

  it('works in the card\'s own pixels, scaled up to the video', () => {
    expect(UI_SCALE).toBe(1.5)
    const m = buildOverlay(input(frame()))
    expect(m.scale).toBe(1.5)
    expect(m.design).toEqual({ w: 1280, h: 720 })
  })

  it('places a card as placeSignCard does, from the sign\'s projected point', () => {
    const a = placed(sign('a', photo), 149, -20)
    const m = buildOverlay(input(frame({ cards: [a] }), { bitmaps: new Set(['a']) }))
    const p = project(149, -20)
    const want = signCardRect(a.sign, { x: p.x / 1.5, y: p.y / 1.5 }, m.design, SIGN_LIFT_PX[0])
    expect(m.cards).toHaveLength(1)
    expect(m.cards[0].rect).toEqual({ x: want.x, y: want.y, w: want.w, h: want.h })
    expect(m.cards[0].below).toBe(want.below)
    expect(m.cards[0].leader).toBe(want.leader)
    expect(m.cards[0].point).toEqual({ x: p.x / 1.5, y: p.y / 1.5 })
  })

  it('floats a second card higher, as the page does', () => {
    const a = placed(sign('a'), 149, -20)
    const b = placed(sign('b'), 149.5, -20)
    const m = buildOverlay(input(frame({ cards: [a, b] })))
    expect(m.cards[0].lift).toBe(SIGN_LIFT_PX[0])
    expect(m.cards[1].lift).toBe(SIGN_LIFT_PX[1])
  })

  it('says whether a card has its photo, has lost it, or never had one', () => {
    const withPhoto = placed(sign('a', photo))
    const lost = placed(sign('b', photo))
    const none = placed(sign('c'))
    const m = buildOverlay(input(frame({ cards: [withPhoto, lost, none] }), { bitmaps: new Set(['a']) }))
    expect(m.cards.map((c) => c.photo)).toEqual(['bitmap', 'missing', 'none'])
  })

  it('fades a card in over 0.3 s from the first frame it is up', () => {
    expect(CARD_FADE_S).toBe(0.3)
    const a = placed(sign('a'))
    const seen = new Map<string, number>()
    expect(buildOverlay(input(frame({ cards: [a] }), { wall: 10, firstSeen: seen })).cards[0].alpha).toBe(0)
    expect(buildOverlay(input(frame({ cards: [a] }), { wall: 10.15, firstSeen: seen })).cards[0].alpha).toBeCloseTo(0.5)
    expect(buildOverlay(input(frame({ cards: [a] }), { wall: 11, firstSeen: seen })).cards[0].alpha).toBe(1)
  })

  it('forgets a card that has gone, so it fades in again next time', () => {
    const a = placed(sign('a'))
    const seen = new Map<string, number>()
    buildOverlay(input(frame({ cards: [a] }), { wall: 10, firstSeen: seen }))
    buildOverlay(input(frame({ cards: [] }), { wall: 12, firstSeen: seen }))
    expect(seen.has('a')).toBe(false)
  })

  it('puts a pin for each sign that has no card up', () => {
    const a = placed(sign('a'), 149, -20)
    const b = placed(sign('b'), 150, -20)
    const m = buildOverlay(input(frame({ cards: [a], pins: [a, b] })))
    expect(m.pins).toHaveLength(1)
    expect(m.pins[0]).toEqual({ x: project(150, -20).x / 1.5, y: project(150, -20).y / 1.5 })
  })

  it('draws the boat as a dot only when asked to', () => {
    expect(buildOverlay(input(frame())).dot).toBeNull()
    const m = buildOverlay(input(frame(), { boatDot: true }))
    expect(m.dot).toEqual({ x: project(149, -20).x / 1.5, y: project(149, -20).y / 1.5 })
    expect(buildOverlay(input(frame({ boat: null }), { boatDot: true })).dot).toBeNull()
  })

  describe('a photo moment', () => {
    const a = placed(sign('a', photo), 149, -20)
    const view = momentView('a', MOMENT_GROW_S + 0.2, false)

    it('is the rect lib/replay-time works out from the card\'s photo', () => {
      const m = buildOverlay(input(frame({ cards: [a], moment: view }), { momentSign: a, bitmaps: new Set(['a']) }))
      const p = project(149, -20)
      const point = { x: p.x / 1.5, y: p.y / 1.5 }
      const want = momentRect(view, signCardPhotoRect(a.sign, point, m.design, SIGN_LIFT_PX[0]), m.design)
      expect(m.moment).toMatchObject({ signId: 'a', rect: { x: want.x, y: want.y, w: want.w, h: want.h }, scale: want.scale })
    })

    it('is nothing without the photo, which could not be loaded', () => {
      expect(buildOverlay(input(frame({ cards: [a], moment: view }), { momentSign: a, bitmaps: new Set() })).moment).toBeNull()
    })

    it('is nothing when there is none', () => {
      expect(buildOverlay(input(frame({ cards: [a] }), { momentSign: a, bitmaps: new Set(['a']) })).moment).toBeNull()
    })
  })
})

import { describe, expect, it } from 'vitest'

import { passageLabelBox } from '@/lib/replay-passage-label'
import { stayLabelBox, stayView } from '@/lib/replay-stay'
import { UI_SCALE, buildOverlay, type OverlayInput } from '@/lib/passage-video/overlay'
import { CARD_FADE_S, signCardBox, signCardPlacement, SIGN_LIFT_PX, type CardMode, type ReplaySign, type ResolvedSign } from '@/lib/replay-signs'
import type { ReplayFrame } from '@/lib/replay-controller'
import { MOMENT_GROW_S, momentRect, momentView } from '@/lib/replay-time'

const sign = (id: string, extra: Partial<ReplaySign> = {}): ReplaySign => ({
  id, t_ms: 0, title: id, note: '', document_id: null, photo_url: null, visibility: 'passing', show_s: 4, starts_leg: false, photo_moment: null, ...extra,
})
const placed = (s: ReplaySign, lon = 149, lat = -20): ResolvedSign => ({ sign: s, clock: 0, at: 0, entryId: 'p', lat, lon })
const photo = { document_id: 'd', photo_url: '/api/documents/d/content' }

const frame = (extra: Partial<ReplayFrame> = {}): ReplayFrame => ({
  here: null, boat: { lat: -20, lon: 149 }, heading: null, sailedTo: null, cards: [], pins: [], light: null, speed: null, moment: null, stay: null, hold: null, passageLabel: null, ...extra,
})

// The map, 1920 by 1080, putting a point at a pixel by its longitude.
const SIZE = { width: 1920, height: 1080 }
const project = (lon: number, lat: number) => ({ x: (lon - 148) * 600, y: 700 + lat * 5 })
const input = (f: ReplayFrame, extra: Partial<OverlayInput> = {}): OverlayInput => ({
  frame: f, size: SIZE, project, wall: 10, firstSeen: new Map(), bitmaps: new Set(), momentSign: undefined, boatDot: false, measure: (t) => t.length * 7, ...extra,
})

describe('buildOverlay and a passage label', () => {
  it('carries the label\'s words and its place in the frame, in design pixels', () => {
    const m = buildOverlay(input(frame({ passageLabel: { label: { id: 'p', text: 'Day 2 \u00b7 A \u2192 B' }, elapsed: 1 } })))
    expect(m.passage).toEqual({ text: 'Day 2 \u00b7 A \u2192 B', box: passageLabelBox(m.design, 1) })
    expect(m.passage!.box.x).toBe(24)
  })

  it('has none otherwise', () => {
    expect(buildOverlay(input(frame())).passage).toBeNull()
  })
})

describe('buildOverlay and a stay', () => {
  const stayFrame = (elapsed: number) => frame({
    stay: stayView({ id: 'stay:a>b', index: 0, clock: 10, at: 0, fromId: 'a', toId: 'b', arrival: 0, departure: 3600, label: '1 h 00 m \u00b7 Bay' }, elapsed),
  })

  it('carries the stay\'s label and its place in the frame, in design pixels', () => {
    const m = buildOverlay(input(stayFrame(0.75)))
    expect(m.stay).toEqual({ text: '1 h 00 m \u00b7 Bay', box: stayLabelBox(m.design, 0.75) })
    expect(m.stay!.box.x).toBe(m.design.w / 2)
  })

  it('has none outside a stay', () => {
    expect(buildOverlay(input(frame())).stay).toBeNull()
  })
})

describe('buildOverlay', () => {
  it('sizes a card from the note as it is wrapped, so a second line is inside the card', () => {
    // 33 characters: two lines at 7 px a character in the 208 px of a text card.
    const note = 'A humpback and her calf off bow'.padEnd(33, 'x')
    const a = placed(sign('a', { note }))
    const m = buildOverlay(input(frame({ cards: [a] })))
    const card = m.cards[0]
    expect(card.noteLines.length).toBe(2)
    expect(card.noteLines.join(' ').replace(/…/g, '')).toContain('humpback')
    expect(card.placement.rect.h).toBe(signCardBox(a.sign, m.design.w, 2).h)
    expect(card.placement.rect.h).toBeGreaterThan(signCardBox(a.sign, m.design.w, 1).h)
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
    const want = signCardPlacement(a.sign, { x: p.x / 1.5, y: p.y / 1.5 }, m.design, SIGN_LIFT_PX[0], 0)
    expect(m.cards).toHaveLength(1)
    expect(m.cards[0].placement).toEqual(want)
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

  it('keeps a card where it was put while it fits, from one frame to the next', () => {
    // A boat low on the map: the card goes beside it, to the right or the left by where there is more room.
    const a = placed(sign('a', photo), 149, -20)
    const low = (x: number): Parameters<typeof buildOverlay>[0]['project'] => () => ({ x: x * 1.5, y: 215 * 1.5 })
    const modes = new Map<string, CardMode>()
    const mode = (x: number) => buildOverlay(input(frame({ cards: [a] }), { project: low(x), size: { width: 1614, height: 624 }, modes })).cards[0].placement.mode
    expect(mode(500)).toBe('right')
    // Across the middle of the map it does not flip to the other side.
    expect([mode(530), mode(540), mode(560), mode(530)]).toEqual(['right', 'right', 'right', 'right'])
    // Out of room on the right, it goes to the left.
    expect(mode(900)).toBe('left')
    // And a card with no memory goes by the room alone.
    const fresh = buildOverlay(input(frame({ cards: [a] }), { project: low(540), size: { width: 1614, height: 624 } })).cards[0].placement.mode
    expect(fresh).toBe('left')
  })

  it('shows the moment from the card as the card stands, in the place the card was kept in', () => {
    const a = placed(sign('a', photo), 149, -20)
    const modes = new Map<string, CardMode>([['a', 'left']])
    const m = buildOverlay(input(frame({ cards: [a], moment: momentView('a', 0, false) }), { momentSign: a, bitmaps: new Set(['a']), project: () => ({ x: 800, y: 322 }), size: { width: 1614, height: 624 }, modes }))
    expect(m.cards[0].placement.mode).toBe('left')
    expect(m.moment!.card).toEqual(m.cards[0].placement.rect)
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

    it('is the whole card lib/replay-time works out from the card as it stands', () => {
      const m = buildOverlay(input(frame({ cards: [a], moment: view }), { momentSign: a, bitmaps: new Set(['a']) }))
      const p = project(149, -20)
      const point = { x: p.x / 1.5, y: p.y / 1.5 }
      const card = signCardPlacement(a.sign, point, m.design, SIGN_LIFT_PX[0], 0)
      const box = signCardBox(a.sign, m.design.w, 0)
      const want = momentRect(view, card.rect, { w: box.w, h: box.h }, m.design)
      expect(m.moment).toMatchObject({ card: want.card, k: want.k, zoom: want.zoom, face: { w: box.w, h: box.h } })
      expect(m.moment!.sign.id).toBe('a')
    })

    it('has the caption of the card, wrapped as the card wraps it', () => {
      const withNote = placed(sign('a', { ...photo, title: 'Whale', note: 'A humpback and her calf off the bow of the boat' }), 149, -20)
      const m = buildOverlay(input(frame({ cards: [withNote], moment: view }), { momentSign: withNote, bitmaps: new Set(['a']) }))
      expect(m.moment!.noteLines).toEqual(m.cards[0].noteLines)
      expect(m.moment!.noteLines.length).toBeGreaterThan(0)
    })

    it('stays inside the video\'s frame, in portrait and in landscape', () => {
      for (const size of [{ width: 1920, height: 1080 }, { width: 1080, height: 1920 }]) {
        const m = buildOverlay(input(frame({ cards: [a], moment: view }), { size, momentSign: a, bitmaps: new Set(['a']) }))
        const c = m.moment!.card
        expect(c.x).toBeGreaterThanOrEqual(0)
        expect(c.y).toBeGreaterThanOrEqual(0)
        expect(c.x + c.w).toBeLessThanOrEqual(m.design.w)
        expect(c.y + c.h).toBeLessThanOrEqual(m.design.h)
      }
    })

    it('is nothing without the photo, which could not be loaded', () => {
      expect(buildOverlay(input(frame({ cards: [a], moment: view }), { momentSign: a, bitmaps: new Set() })).moment).toBeNull()
    })

    it('is nothing when there is none', () => {
      expect(buildOverlay(input(frame({ cards: [a] }), { momentSign: a, bitmaps: new Set(['a']) })).moment).toBeNull()
    })
  })
})

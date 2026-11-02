import { describe, expect, it, vi } from 'vitest'

import { BRAND_LINE, CLOSING_SCALE, closingCardLayout } from '@/lib/passage-video/closing'
import { passageLabelBox } from '@/lib/replay-passage-label'
import { MAP_LABEL } from '@/lib/map-label'
import { stayLabelBox } from '@/lib/replay-stay'
import { createCanvasCompositor, drawOverlay, paintClosingCard, themeFor, wrapLines } from '@/lib/passage-video/draw'
import { UI_SCALE, type OverlayModel } from '@/lib/passage-video/overlay'
import { VIDEO_SIZES } from '@/lib/passage-video/schedule'
import type { CardPlacement, ReplaySign } from '@/lib/replay-signs'

// A drawing surface that writes down what it is asked to do.
function recorder() {
  const calls: { op: string; args: unknown[] }[] = []
  const state: Record<string, unknown> = { globalAlpha: 1 }
  const ctx = new Proxy({}, {
    get(_t, key: string) {
      if (key === 'measureText') return (text: string) => ({ width: text.length * 7 })
      if (key in state) return state[key]
      return (...args: unknown[]) => { calls.push({ op: key, args }) }
    },
    set(_t, key: string, value) { state[key] = value; calls.push({ op: `set ${key}`, args: [value] }); return true },
  }) as unknown as CanvasRenderingContext2D
  return { ctx, calls, texts: () => calls.filter((c) => c.op === 'fillText').map((c) => c.args[0] as string), ops: (op: string) => calls.filter((c) => c.op === op) }
}

const sign = (extra: Partial<ReplaySign> = {}): ReplaySign => ({
  id: 'a', t_ms: 0, title: 'Whale', note: 'A humpback off the bow', document_id: 'd', photo_url: '/p', visibility: 'passing', show_s: 4, starts_leg: false, photo_moment: null, ...extra,
})
const model = (extra: Partial<OverlayModel> = {}): OverlayModel => ({ scale: UI_SCALE, design: { w: 1280, h: 720 }, cards: [], pins: [], moment: null, dot: null, stay: null, passage: null, ...extra })
const card = (extra: Partial<OverlayModel['cards'][number]> = {}): OverlayModel['cards'][number] => ({
  sign: sign(), point: { x: 600, y: 500 }, placement: placed({ x: 472, y: 276, w: 256, h: 224 }), lift: 20, noteLines: ['A humpback off the bow'], alpha: 1, photo: 'bitmap', ...extra,
})
/** A card's placement: where it is, the leader from the point to it, and how much smaller than its own size. */
function placed(rect: { x: number; y: number; w: number; h: number }, extra: Partial<CardPlacement> = {}): CardPlacement {
  return { rect, scale: 1, mode: 'above', hidden: false, leader: { x1: 600, y1: 500, x2: 600, y2: rect.y + rect.h }, ...extra }
}
const bitmap = { width: 800, height: 600 } as unknown as ImageBitmap
const photos = new Map<string, ImageBitmap>([['a', bitmap]])

describe('wrapLines', () => {
  const measure = (t: string) => t.length * 7
  it('breaks on spaces to fit, and on new lines', () => {
    expect(wrapLines('one two three', 7 * 8, '14px x', measure, 3)).toEqual(['one two', 'three'])
    expect(wrapLines('one\ntwo', 700, '14px x', measure, 3)).toEqual(['one', 'two'])
  })

  it('stops at the most lines and ends the last with an ellipsis', () => {
    const lines = wrapLines('alpha beta gamma delta epsilon zeta', 7 * 12, '14px x', measure, 2)
    expect(lines).toHaveLength(2)
    expect(lines[1].endsWith('…')).toBe(true)
  })

  it('shortens a word wider than the line', () => {
    const [line] = wrapLines('x'.repeat(100), 70, '14px x', measure, 1)
    expect(line.length).toBeLessThanOrEqual(10)
    expect(line.endsWith('…')).toBe(true)
  })

  it('has nothing for an empty note', () => {
    expect(wrapLines('', 100, '14px x', measure, 2)).toEqual([])
  })
})

describe('createCanvasCompositor', () => {
  it('puts the map across the whole frame, then the signs over it', () => {
    const r = recorder()
    const canvas = { width: 0, height: 0, getContext: () => r.ctx }
    const spy = vi.spyOn(document, 'createElement').mockReturnValue(canvas as unknown as HTMLElement)
    try {
      const compositor = createCanvasCompositor(VIDEO_SIZES.portrait, { isDarkTheme: false, photos })
      expect(canvas).toMatchObject({ width: 1080, height: 1920 })
      const map = {} as CanvasImageSource
      compositor.frame(map, model({ cards: [card()] }))
      expect(r.ops('drawImage')[0].args).toEqual([map, 0, 0, 1080, 1920])
      expect(r.texts()).toContain('Whale')
      expect(compositor.measure('abc', '12px x')).toBe(21)
    } finally {
      spy.mockRestore()
    }
  })
})

describe('the stay label', () => {
  const stay = (elapsed: number) => ({ text: '2 nights \u00b7 Blue Pearl Bay', box: stayLabelBox({ w: 1280, h: 720 }, elapsed) })

  it('writes the same words the page shows, on a card in the box the pure layout gives', () => {
    const r = recorder()
    const s = stay(0.75)
    drawOverlay(r.ctx, model({ stay: s }), photos, themeFor(false))
    expect(r.texts()).toEqual(['2 nights \u00b7 Blue Pearl Bay'])
    const text = r.ops('fillText')[0]
    const w = s.text.length * 7 + 2 * s.box.padX
    // The card is as wide as its text and the padding, and centred on the box's middle.
    const card = r.ops('roundRect')[0].args
    expect(card[0]).toBeCloseTo(s.box.x - w / 2)
    expect(card[1]).toBe(s.box.y)
    expect(card[2]).toBeCloseTo(w)
    expect(card[3]).toBeCloseTo(s.box.fontPx * MAP_LABEL.lineHeight + 2 * s.box.padY)
    expect(card[4]).toBe(MAP_LABEL.radius)
    expect(text.args[1]).toBeCloseTo(s.box.x - w / 2 + s.box.padX)
  })

  it('takes the page\'s weight and its card and text colours from the theme', () => {
    const r = recorder()
    drawOverlay(r.ctx, model({ stay: stay(0.75) }), photos, themeFor(true))
    const fonts = r.calls.filter((c) => c.op === 'set font').map((c) => c.args[0] as string)
    expect(fonts.every((f) => f.startsWith(`${MAP_LABEL.weight} 13px`))).toBe(true)
    const fills = r.calls.filter((c) => c.op === 'set fillStyle').map((c) => c.args[0])
    expect(fills).toEqual([themeFor(true).card, themeFor(true).text])
    const alphas = r.calls.filter((c) => c.op === 'set globalAlpha').map((c) => c.args[0])
    expect(alphas).toContain(MAP_LABEL.bgAlpha)
  })

  it('fades with the stay: nothing at its start, and shortened to the room it has', () => {
    const none = recorder()
    drawOverlay(none.ctx, model({ stay: stay(0) }), photos, themeFor(false))
    expect(none.ops('fillText')).toHaveLength(0)
    const narrow = recorder()
    drawOverlay(narrow.ctx, model({ stay: { text: 'x'.repeat(200), box: { ...stay(0.75).box, maxW: 100 } } }), photos, themeFor(false))
    expect(narrow.texts()[0].endsWith('\u2026')).toBe(true)
  })

  it('is not drawn when there is no stay', () => {
    const r = recorder()
    drawOverlay(r.ctx, model(), photos, themeFor(false))
    expect(r.ops('fillText')).toHaveLength(0)
  })
})

describe('the passage label', () => {
  const label = (elapsed: number) => ({ text: 'Day 3 \u00b7 Funnel Bay \u2192 Thomas Island', box: passageLabelBox({ w: 1280, h: 720 }, elapsed) })

  it('writes the page\'s words on a card at the left, in the box the pure layout gives', () => {
    const r = recorder()
    const l = label(1)
    drawOverlay(r.ctx, model({ passage: l }), photos, themeFor(false))
    expect(r.texts()).toEqual([l.text])
    const w = l.text.length * 7 + 2 * l.box.padX
    const card = r.ops('roundRect')[0].args
    // Left-aligned: the card starts at the box's left edge, not centred on it.
    expect(card[0]).toBe(l.box.x)
    expect(card[1]).toBe(l.box.y)
    expect(card[2]).toBeCloseTo(w)
    expect(card[3]).toBeCloseTo(l.box.fontPx * MAP_LABEL.lineHeight + 2 * l.box.padY)
    expect(r.ops('fillText')[0].args[1]).toBeCloseTo(l.box.x + l.box.padX)
  })

  it('looks like the stay\'s label: the same weight, size and colours', () => {
    const a = recorder()
    const b = recorder()
    drawOverlay(a.ctx, model({ passage: label(1) }), photos, themeFor(true))
    drawOverlay(b.ctx, model({ stay: { text: 'x', box: stayLabelBox({ w: 1280, h: 720 }, 0.75) } }), photos, themeFor(true))
    const look = (r: typeof a) => r.calls.filter((c) => ['set font', 'set fillStyle'].includes(c.op)).map((c) => [c.op, c.args[0]])
    expect(look(a)).toEqual(look(b))
  })

  it('is not drawn at the start of its fade, or when there is none', () => {
    const r = recorder()
    drawOverlay(r.ctx, model({ passage: label(0) }), photos, themeFor(false))
    drawOverlay(r.ctx, model(), photos, themeFor(false))
    expect(r.ops('fillText')).toHaveLength(0)
  })
})

describe('drawOverlay', () => {
  it('draws at the video\'s scale', () => {
    const r = recorder()
    drawOverlay(r.ctx, model({ cards: [card()] }), photos, themeFor(false))
    expect(r.ops('scale')[0].args).toEqual([1.5, 1.5])
  })

  it('writes the sign\'s title and its note on its card, and the photo above them', () => {
    const r = recorder()
    drawOverlay(r.ctx, model({ cards: [card()] }), photos, themeFor(false))
    expect(r.texts()).toContain('Whale')
    expect(r.texts()).toContain('A humpback off the bow')
    const img = r.ops('drawImage')[0]
    expect(img.args[0]).toBe(bitmap)
    // Cropped to the photo's 4 by 3, whatever its own shape.
    expect((img.args[3] as number) / (img.args[4] as number)).toBeCloseTo(4 / 3, 2)
  })

  it('fades a card by its alpha', () => {
    const r = recorder()
    drawOverlay(r.ctx, model({ cards: [card({ alpha: 0.4 })] }), photos, themeFor(false))
    expect(r.ops('set globalAlpha').map((c) => c.args[0])).toContain(0.4)
  })

  it('says "Photo not available" on a card whose photo is missing, and still draws the card', () => {
    const r = recorder()
    drawOverlay(r.ctx, model({ cards: [card({ photo: 'missing' })] }), new Map(), themeFor(false))
    expect(r.texts()).toContain('Photo not available')
    expect(r.texts()).toContain('Whale')
    expect(r.ops('drawImage')).toHaveLength(0)
  })

  it('draws a card with no photo as text alone', () => {
    const r = recorder()
    drawOverlay(r.ctx, model({ cards: [card({ sign: sign({ photo_url: null, document_id: null }), photo: 'none' })] }), new Map(), themeFor(false))
    expect(r.texts()).toContain('Whale')
    expect(r.texts()).not.toContain('Photo not available')
  })

  it('joins the card to its point with a leader line and a dot', () => {
    const r = recorder()
    drawOverlay(r.ctx, model({ cards: [card()] }), photos, themeFor(false))
    expect(r.ops('lineTo').length).toBeGreaterThan(0)
    expect(r.ops('arc').some((c) => c.args[0] === 600 && c.args[1] === 500)).toBe(true)
  })

  it('draws a pin and the boat\'s dot', () => {
    const r = recorder()
    drawOverlay(r.ctx, model({ pins: [{ x: 100, y: 100 }], dot: { x: 300, y: 300 } }), photos, themeFor(true))
    expect(r.ops('rotate').length).toBeGreaterThan(0)
    expect(r.ops('arc').some((c) => c.args[0] === 300 && c.args[1] === 300)).toBe(true)
  })

  describe('the photo moment', () => {
    const moment = (extra: Partial<NonNullable<OverlayModel['moment']>> = {}): NonNullable<OverlayModel['moment']> => ({
      sign: sign(), noteLines: ['A humpback off the bow'], card: { x: 240, y: 90, w: 320, h: 280 }, k: 1.25, face: { w: 256, h: 224 }, zoom: 1.04, ...extra,
    })

    it('draws the whole card, grown: the frame, the photo and the caption, at the scale of the card', () => {
      const r = recorder()
      drawOverlay(r.ctx, model({ moment: moment() }), photos, themeFor(false))
      // The card is put where it stands and made 1.25 times bigger, then drawn at its own size.
      expect(r.ops('translate')[0].args).toEqual([240, 90])
      expect(r.ops('scale').some((c) => c.args[0] === 1.25 && c.args[1] === 1.25)).toBe(true)
      expect(r.texts()).toEqual(['Whale', 'A humpback off the bow'])
      expect(r.ops('roundRect')[0].args).toEqual([0, 0, 256, 224, 6])
      expect(r.ops('drawImage')).toHaveLength(1)
    })

    it('zooms the photo inside its frame alone: the caption and the frame are not zoomed', () => {
      const r = recorder()
      drawOverlay(r.ctx, model({ moment: moment({ zoom: 1.04 }) }), photos, themeFor(false))
      const zoomAt = r.calls.findIndex((c) => c.op === 'scale' && c.args[0] === 1.04)
      expect(zoomAt).toBeGreaterThan(-1)
      const photoAt = r.calls.findIndex((c) => c.op === 'drawImage')
      const textAt = r.calls.findIndex((c) => c.op === 'fillText' && c.args[0] === 'Whale')
      // The zoom is set before the photo is drawn and gone (restored) before the title is written.
      expect(zoomAt).toBeLessThan(photoAt)
      expect(r.calls.slice(photoAt, textAt).some((c) => c.op === 'restore')).toBe(true)
      expect(r.ops('scale').filter((c) => c.args[0] === 1.04)).toHaveLength(1)
    })

    it('has the photo in the 4 by 3 frame of the card, whole', () => {
      const r = recorder()
      drawOverlay(r.ctx, model({ moment: moment({ zoom: 1 }) }), photos, themeFor(false))
      const img = r.ops('drawImage')[0].args
      // The bitmap is 800 by 600: the whole picture, no crop, into 240 by 180.
      expect(img.slice(1, 5)).toEqual([0, 0, 800, 600])
      expect(img.slice(5)).toEqual([8, 8, 240, 180])
    })

    it('draws nothing when the photo is not there', () => {
      const r = recorder()
      drawOverlay(r.ctx, model({ moment: moment({ sign: sign({ id: 'gone' }) }) }), photos, themeFor(false))
      expect(r.ops('fillText')).toHaveLength(0)
    })
  })

  describe('a card beside its point, or made smaller', () => {
    it('is drawn at its own size, then made smaller, with a leader to its edge', () => {
      const r = recorder()
      const point = { x: 680, y: 256 }
      drawOverlay(r.ctx, model({ cards: [card({ point, placement: placed({ x: 700, y: 200, w: 128, h: 112 }, { scale: 0.5, mode: 'right', leader: { x1: 680, y1: 256, x2: 700, y2: 256 } }) })] }), photos, themeFor(false))
      expect(r.ops('translate')[0].args).toEqual([700, 200])
      expect(r.ops('scale').some((c) => c.args[0] === 0.5)).toBe(true)
      expect(r.ops('roundRect')[0].args).toEqual([0, 0, 256, 224, 6])
      // The leader runs from the card's left edge, level with the point, to the point.
      expect(r.ops('moveTo')[0].args).toEqual([700, 256])
      expect(r.ops('lineTo')[0].args).toEqual([680, 256])
    })

    it('has the leader from the card\'s right edge when it is on the left', () => {
      const r = recorder()
      const point = { x: 376, y: 500 }
      drawOverlay(r.ctx, model({ cards: [card({ point, placement: placed({ x: 100, y: 200, w: 256, h: 224 }, { mode: 'left', leader: { x1: 376, y1: 500, x2: 356, y2: 424 } }) })] }), photos, themeFor(false))
      expect(r.ops('moveTo')[0].args).toEqual([356, 424])
      expect(r.ops('lineTo')[0].args).toEqual([376, 500])
    })

    it('has no leader when the point is off the map, and no card when it is far off', () => {
      const off = recorder()
      drawOverlay(off.ctx, model({ cards: [card({ placement: placed({ x: 6, y: 100, w: 256, h: 224 }, { leader: null }) })] }), photos, themeFor(false))
      expect(off.ops('moveTo')).toHaveLength(0)
      expect(off.texts()).toContain('Whale')
      const far = recorder()
      drawOverlay(far.ctx, model({ cards: [card({ placement: placed({ x: -900, y: 100, w: 256, h: 224 }, { hidden: true }) })] }), photos, themeFor(false))
      expect(far.ops('fillText')).toHaveLength(0)
    })
  })

  it('draws nothing for an empty model', () => {
    const r = recorder()
    drawOverlay(r.ctx, model(), photos, themeFor(false))
    expect(r.ops('fillText')).toHaveLength(0)
    expect(r.ops('drawImage')).toHaveLength(0)
  })
})

describe('the closing card', () => {
  const summary = { title: 'Fairey Reef to Blue Pearl Bay', dates: '24 Oct 2026', stats: [{ l: 'Distance', v: '48.2 nm' }, { l: 'Time', v: '7 h 20 m' }, { l: 'Avg SOG', v: '6.6 kn' }] }
  const measure = (t: string) => t.length * 7

  it('is the PNG export\'s band, title, dates, figures and credit, drawn in a layout 1.6 times smaller than the video', () => {
    expect(CLOSING_SCALE).toBe(1.6)
    const c = closingCardLayout(VIDEO_SIZES.landscape, summary, false, measure)
    expect(c.design).toEqual({ w: 1200, h: 675 })
    expect(c.layout.title.text).toBe('Fairey Reef to Blue Pearl Bay')
    expect(c.layout.dates.text).toBe('24 Oct 2026')
    expect(c.layout.stats.map((s) => s.value.text)).toEqual(['48.2 nm', '7 h 20 m', '6.6 kn'])
    expect(c.layout.attribution.text).toContain('OpenStreetMap')
    expect(c.layout.inset).toBeNull()
  })

  it('adds the quiet Helmcentral line over the foot of the map, clear of the band', () => {
    expect(BRAND_LINE).toBe('Made with Helmcentral · github.com/gterrill/helmcentral')
    const c = closingCardLayout(VIDEO_SIZES.landscape, summary, false, measure)
    expect(c.brand.text).toBe(BRAND_LINE)
    expect(c.brand.y).toBeLessThan(c.layout.band.y)
    expect(c.brand.x).toBe(36)
  })

  it('is the same card turned upright for portrait', () => {
    const c = closingCardLayout(VIDEO_SIZES.portrait, summary, false, measure)
    expect(c.design).toEqual({ w: 675, h: 1200 })
    expect(c.layout.band.y + c.layout.band.h).toBe(1200)
    expect(c.layout.title.text.length).toBeGreaterThan(0)
  })

  describe('upright, for portrait', () => {
    // Text is as wide as the font is tall, per character, as it is in life.
    const scaled = (t: string, f: string) => t.length * Number(/(\d+)px/.exec(f)![1]) * 0.55
    const title = 'Fairey Reef #2 \u2192 Blue Pearl Bay'
    const fuller = { title, dates: '24 Oct 2026 \u2013 25 Oct 2026', stats: [{ l: 'Distance', v: '48.2 nm' }, { l: 'Underway', v: '7 h 20 m' }, { l: 'Max gust', v: '31.4 kn' }, { l: 'Avg SOG', v: '6.6 kn' }] }

    it('does not cut the title, and puts the date under it', () => {
      const c = closingCardLayout(VIDEO_SIZES.portrait, fuller, false, scaled)
      const lines = [c.layout.title, ...c.layout.titleRest].map((t) => t.text)
      expect(lines.join(' ')).toBe(title)
      expect(lines.join('')).not.toContain('\u2026')
      expect(c.layout.dates.y).toBeGreaterThan(c.layout.title.y)
      expect(c.layout.dates.align).toBe('left')
      // Every line is inside the frame.
      for (const t of [c.layout.title, ...c.layout.titleRest, c.layout.dates]) {
        expect(t.x + scaled(t.text, t.font)).toBeLessThanOrEqual(c.design.w)
      }
    })

    it('draws every figure, in rows that fit the width', () => {
      const c = closingCardLayout(VIDEO_SIZES.portrait, fuller, false, scaled)
      expect(c.layout.stats.map((s) => s.value.text)).toEqual(['48.2 nm', '7 h 20 m', '31.4 kn', '6.6 kn'])
      for (const s of c.layout.stats) expect(s.value.x + scaled(s.value.text, s.value.font)).toBeLessThanOrEqual(c.design.w)
      expect(c.layout.stats.every((s) => s.value.y < c.layout.attribution.y)).toBe(true)
    })

    it('wraps the figures onto a second row when they do not fit in one', () => {
      const many = { ...fuller, stats: Array.from({ length: 5 }, (_, i) => ({ l: `Figure ${i}`, v: `${i}00 units` })) }
      const c = closingCardLayout(VIDEO_SIZES.portrait, many, false, scaled)
      expect(c.layout.stats).toHaveLength(5)
      expect(new Set(c.layout.stats.map((s) => s.value.y)).size).toBeGreaterThan(1)
    })

    it('wraps a long title onto two lines and shortens only past that', () => {
      const long = { ...fuller, title: 'A very long passage name that goes on and on and on and on and on and on and on and on' }
      const c = closingCardLayout(VIDEO_SIZES.portrait, long, false, scaled)
      expect(c.layout.titleRest).toHaveLength(1)
      expect(c.layout.titleRest[0].text.endsWith('\u2026')).toBe(true)
    })

    it('sits at the foot of the frame, with the Made-with line over the map just above it', () => {
      const c = closingCardLayout(VIDEO_SIZES.portrait, fuller, false, scaled)
      expect(c.layout.band.y + c.layout.band.h).toBe(c.design.h)
      expect(c.brand.y).toBeLessThan(c.layout.band.y)
      expect(c.brand.x).toBe(c.layout.title.x)
    })

    it('leaves the landscape card as it was', () => {
      const c = closingCardLayout(VIDEO_SIZES.landscape, fuller, false, scaled)
      expect(c.layout.titleRest).toEqual([])
      expect(c.layout.dates.align).toBe('right')
    })
  })

  it('drops figures that do not fit rather than squeezing them', () => {
    const many = { ...summary, stats: Array.from({ length: 40 }, (_, i) => ({ l: `Figure ${i}`, v: `${i} units` })) }
    const c = closingCardLayout(VIDEO_SIZES.portrait, many, false, measure)
    expect(c.layout.stats.length).toBeLessThan(40)
    expect(c.layout.stats.length).toBeGreaterThan(0)
  })

  it('credits the imagery when the map was drawn over it', () => {
    expect(closingCardLayout(VIDEO_SIZES.landscape, summary, true, measure).layout.attribution.text).toContain('Esri')
  })

  it('paints the map, then the band and the figures, then the line', () => {
    const r = recorder()
    const map = { width: 1920, height: 1080 } as unknown as CanvasImageSource
    paintClosingCard(r.ctx, map, closingCardLayout(VIDEO_SIZES.landscape, summary, false, measure), VIDEO_SIZES.landscape)
    expect(r.ops('drawImage')[0].args[0]).toBe(map)
    expect(r.texts()).toEqual(expect.arrayContaining(['Fairey Reef to Blue Pearl Bay', '24 Oct 2026', '48.2 nm', BRAND_LINE]))
    expect(r.ops('fillRect').length).toBeGreaterThan(0)
    expect(r.ops('scale')[0].args).toEqual([1.6, 1.6])
  })
})

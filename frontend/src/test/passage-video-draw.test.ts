import { describe, expect, it, vi } from 'vitest'

import { BRAND_LINE, CLOSING_SCALE, closingCardLayout } from '@/lib/passage-video/closing'
import { createCanvasCompositor, drawOverlay, paintClosingCard, themeFor, wrapLines } from '@/lib/passage-video/draw'
import { UI_SCALE, type OverlayModel } from '@/lib/passage-video/overlay'
import { VIDEO_SIZES } from '@/lib/passage-video/schedule'
import type { ReplaySign } from '@/lib/replay-signs'

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
const model = (extra: Partial<OverlayModel> = {}): OverlayModel => ({ scale: UI_SCALE, design: { w: 1280, h: 720 }, cards: [], pins: [], moment: null, dot: null, ...extra })
const card = (extra: Partial<OverlayModel['cards'][number]> = {}): OverlayModel['cards'][number] => ({
  sign: sign(), point: { x: 600, y: 500 }, rect: { x: 472, y: 276, w: 256, h: 224 }, below: false, leader: 20, lift: 20, noteLines: ['A humpback off the bow'], alpha: 1, photo: 'bitmap', ...extra,
})
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

  it('draws the moment\'s photo in its rect, zoomed, over the card', () => {
    const r = recorder()
    drawOverlay(r.ctx, model({ cards: [card()], moment: { signId: 'a', rect: { x: 100, y: 80, w: 600, h: 450 }, scale: 1.04, fill: 1 } }), photos, themeFor(false))
    const draws = r.ops('drawImage')
    expect(draws).toHaveLength(2)
    expect(r.ops('translate').some((c) => c.args[0] === 400 && c.args[1] === 305)).toBe(true)
    expect(r.ops('scale').some((c) => c.args[0] === 1.04)).toBe(true)
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

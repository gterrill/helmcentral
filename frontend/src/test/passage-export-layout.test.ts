import { describe, expect, it } from 'vitest'

import { IMAGE_SIZES, composeImageLayout, fitText, paintOverlay, type LayoutSpec, type Measure } from '@/lib/passage-export-layout'

// Every glyph is half its font size wide, so widths are exact and the same on
// every machine.
const measure: Measure = (text, font) => {
  const px = Number(/(\d+)px/.exec(font)?.[1] ?? 16)
  return text.length * px * 0.5
}

const STATS = [
  { l: 'Distance', v: '93.9 NM' },
  { l: 'Underway', v: '12 h 25 m' },
  { l: 'Nights', v: '2' },
  { l: 'Fuel', v: '300 L' },
  { l: 'Max gust', v: '33.0 kn' },
]

function spec(over: Partial<LayoutSpec> = {}): LayoutSpec {
  return { ...IMAGE_SIZES.og, title: 'Hamilton Island to Mackay', dates: '14 to 16 Sep 2026', stats: STATS, inset: true, ...over }
}

describe('image sizes', () => {
  it('are 1200 x 630 and 2:1 at twice the layout size', () => {
    expect(IMAGE_SIZES.og).toMatchObject({ width: 1200, height: 630, outScale: 1 })
    const w = IMAGE_SIZES.wide
    expect([w.width * w.outScale, w.height * w.outScale]).toEqual([2400, 1200])
  })
})

describe('fitText', () => {
  it('leaves text that fits', () => {
    expect(fitText('Short', 500, '16px x', measure)).toBe('Short')
  })

  it('cuts to the longest prefix that fits with an ellipsis', () => {
    const out = fitText('A very long passage title indeed', 120, '16px x', measure)
    expect(out.endsWith('…')).toBe(true)
    expect(measure(out, '16px x')).toBeLessThanOrEqual(120)
    expect(measure(`${out.slice(0, -1)}x…`, '16px x')).toBeGreaterThan(120)
  })

  it('gives nothing when not even one letter fits', () => {
    expect(fitText('Anything', 4, '16px x', measure)).toBe('')
  })
})

describe('composeImageLayout', () => {
  it('puts the band along the bottom and keeps the map clear of it', () => {
    const l = composeImageLayout(spec(), measure)
    expect(l.band.y + l.band.h).toBe(630)
    expect(l.band.w).toBe(1200)
    expect(l.padding.bottom).toBeGreaterThan(l.band.h)
  })

  it('shows every figure in order with the title and dates on one row', () => {
    const l = composeImageLayout(spec(), measure)
    expect(l.stats.map((s) => [s.label.text, s.value.text])).toEqual([
      ['DISTANCE', '93.9 NM'], ['UNDERWAY', '12 h 25 m'], ['NIGHTS', '2'], ['FUEL', '300 L'], ['MAX GUST', '33.0 kn'],
    ])
    expect(l.title.text).toBe('Hamilton Island to Mackay')
    expect(l.title.y).toBe(l.dates.y)
    expect(l.dates.align).toBe('right')
    const xs = l.stats.map((s) => s.label.x)
    expect([...xs].sort((a, b) => a - b)).toEqual(xs)
  })

  it('shortens a long title so it never runs into the dates', () => {
    const l = composeImageLayout(spec({ title: 'A'.repeat(200) }), measure)
    expect(l.title.text.endsWith('…')).toBe(true)
    const titleEnd = l.title.x + measure(l.title.text, l.title.font)
    const datesStart = l.dates.x - measure(l.dates.text, l.dates.font)
    expect(titleEnd).toBeLessThan(datesStart)
  })

  it('drops the figures that do not fit, from the end, and never overlaps the attribution', () => {
    const narrow = composeImageLayout(spec({ width: 800, height: 420 }), measure)
    expect(narrow.stats.length).toBeLessThan(5)
    expect(narrow.stats.length).toBeGreaterThan(0)
    const last = narrow.stats[narrow.stats.length - 1]
    const lastEnd = last.value.x + measure(last.value.text, last.value.font)
    const attrStart = narrow.attribution.x - measure(narrow.attribution.text, narrow.attribution.font)
    expect(lastEnd).toBeLessThan(attrStart)
    expect(narrow.stats[0].label.text).toBe('DISTANCE')
  })

  it('draws no strip when there are no figures', () => {
    expect(composeImageLayout(spec({ stats: [] }), measure).stats).toEqual([])
  })

  it('places the overview in the top right corner only when it was on', () => {
    const on = composeImageLayout(spec(), measure)
    expect(on.inset).not.toBeNull()
    const r = on.inset as NonNullable<typeof on.inset>
    expect(r.x + r.w).toBeLessThan(1200)
    expect(r.y).toBeGreaterThan(0)
    expect(r.y + r.h).toBeLessThan(on.band.y)
    expect(composeImageLayout(spec({ inset: false }), measure).inset).toBeNull()
  })

  it('credits the map sources on the image', () => {
    const l = composeImageLayout(spec(), measure)
    expect(l.attribution.text).toContain('CARTO')
    expect(l.attribution.text).toContain('OpenStreetMap')
    expect(l.attribution.text).toContain('OpenSeaMap')
  })

  it('lays out the 2:1 image on its own height', () => {
    const w = IMAGE_SIZES.wide
    const l = composeImageLayout({ width: w.width, height: w.height, title: 'T', dates: 'D', stats: STATS, inset: false }, measure)
    expect(l.band.y + l.band.h).toBe(600)
  })
})

describe('paintOverlay', () => {
  it('fills the band and writes every text item', () => {
    const calls: string[] = []
    const ctx = new Proxy({}, {
      get: (_t, prop) => (typeof prop === 'string' && ['fillRect', 'fillText'].includes(prop)
        ? (...args: unknown[]) => calls.push(`${prop}:${String(args[0])}`)
        : undefined),
      set: () => true,
    }) as unknown as CanvasRenderingContext2D
    const l = composeImageLayout(spec(), measure)
    paintOverlay(ctx, l)
    const texts = calls.filter((c) => c.startsWith('fillText')).map((c) => c.slice(9))
    expect(texts).toContain('Hamilton Island to Mackay')
    expect(texts).toContain('14 to 16 Sep 2026')
    expect(texts).toContain('DISTANCE')
    expect(texts).toContain('93.9 NM')
    expect(calls.filter((c) => c.startsWith('fillRect')).length).toBeGreaterThanOrEqual(1)
  })
})

import { exportImageAttribution } from '@/lib/passage-export-style'
import type { InsetCorner } from '@/lib/passage-inset-view'

// Layout of the static passage images (ADR 0174): where the title, dates,
// figures and overview go, worked out as plain numbers and strings so it can be
// tested without WebGL. passage-export-image.ts draws the map and paints this
// over it.

export type ImageKind = 'og' | 'wide'

export interface ImageSize {
  kind: ImageKind
  label: string
  /** Layout size in CSS pixels. */
  width: number
  height: number
  /** File pixels per layout pixel. */
  outScale: number
}

export const IMAGE_SIZES: Record<ImageKind, ImageSize> = {
  og: { kind: 'og', label: '1200 × 630', width: 1200, height: 630, outScale: 1 },
  wide: { kind: 'wide', label: '2:1', width: 1200, height: 600, outScale: 2 },
}

export interface Rect { x: number; y: number; w: number; h: number }
export interface TextItem {
  text: string
  x: number
  y: number
  font: string
  fill: string
  align: 'left' | 'right'
}
export type Measure = (text: string, font: string) => number

export interface ImageLayout {
  width: number
  height: number
  band: Rect
  title: TextItem
  /** The lines of a long title after the first (portrait only). */
  titleRest: TextItem[]
  dates: TextItem
  stats: { label: TextItem; value: TextItem }[]
  attribution: TextItem
  inset: Rect | null
  /** Space the map keeps clear when framing the track. */
  padding: { top: number; right: number; bottom: number; left: number }
}

export const FONT_STACK = 'system-ui, -apple-system, "Segoe UI", "Helvetica Neue", Arial, sans-serif'
const INK = '#1c1917'
const MUTED = '#57534e'

const font = (weight: number, px: number) => `${weight} ${px}px ${FONT_STACK}`

/** The text, shortened with an ellipsis until it fits `maxWidth`. */
export function fitText(text: string, maxWidth: number, f: string, measure: Measure): string {
  if (measure(text, f) <= maxWidth) return text
  let lo = 0
  let hi = text.length
  while (lo < hi) {
    const mid = Math.ceil((lo + hi) / 2)
    if (measure(`${text.slice(0, mid).trimEnd()}…`, f) <= maxWidth) lo = mid
    else hi = mid - 1
  }
  return lo === 0 ? '' : `${text.slice(0, lo).trimEnd()}…`
}

export interface LayoutSpec {
  width: number
  height: number
  title: string
  dates: string
  stats: { l: string; v: string }[]
  inset: boolean
  /** Which corner of the map the overview sits in; top right when absent. */
  insetCorner?: InsetCorner
  /** The map is drawn over satellite imagery, which needs its credit. */
  satellite?: boolean
  /** A tall frame: the title wraps, the date goes under it, and the figures flow onto rows. */
  portrait?: boolean
}

/** Where everything goes on the image. Stats that do not fit are dropped from
 * the end, never squeezed. */
export function composeImageLayout(spec: LayoutSpec, measure: Measure): ImageLayout {
  if (spec.portrait) return composePortraitLayout(spec, measure)
  const { width, height } = spec
  const margin = 36
  const bandH = Math.round(height * 0.24)
  const band: Rect = { x: 0, y: height - bandH, w: width, h: bandH }

  const titleFont = font(700, Math.round(bandH * 0.26))
  const datesFont = font(400, Math.round(bandH * 0.16))
  const labelFont = font(600, 13)
  const valueFont = font(700, Math.round(bandH * 0.2))
  const attrFont = font(400, 12)

  const row1 = band.y + Math.round(bandH * 0.1) + Math.round(bandH * 0.26)
  const datesW = measure(spec.dates, datesFont)
  const titleMax = width - margin * 2 - datesW - 24
  const title: TextItem = { text: fitText(spec.title, titleMax, titleFont, measure), x: margin, y: row1, font: titleFont, fill: INK, align: 'left' }
  const dates: TextItem = { text: spec.dates, x: width - margin, y: row1, font: datesFont, fill: MUTED, align: 'right' }

  const attribution: TextItem = {
    text: exportImageAttribution(spec.satellite ?? false), x: width - margin, y: height - 14, font: attrFont, fill: MUTED, align: 'right',
  }
  const statsMax = width - margin * 2 - measure(attribution.text, attrFont) - 28
  const gap = 40
  const labelY = band.y + Math.round(bandH * 0.62)
  const valueY = labelY + Math.round(bandH * 0.2) + 6
  const stats: ImageLayout['stats'] = []
  let x = margin
  for (const s of spec.stats) {
    const label = s.l.toUpperCase()
    const w = Math.max(measure(label, labelFont), measure(s.v, valueFont))
    if (x + w - margin > statsMax) break
    stats.push({
      label: { text: label, x, y: labelY, font: labelFont, fill: MUTED, align: 'left' },
      value: { text: s.v, x, y: valueY, font: valueFont, fill: INK, align: 'left' },
    })
    x += w + gap
  }

  const insetW = Math.round(width * 0.22)
  const insetH = Math.round(insetW * 0.68)
  // Always inside the map area, 24 px from its edges: a bottom corner sits
  // above the band, so the title, the figures and the credit are never covered.
  const corner = spec.insetCorner ?? 'top-right'
  const inset: Rect | null = spec.inset
    ? {
      x: corner.endsWith('left') ? 24 : width - 24 - insetW,
      y: corner.startsWith('top') ? 24 : band.y - 24 - insetH,
      w: insetW,
      h: insetH,
    }
    : null
  return {
    width, height, band, title, titleRest: [], dates, stats, attribution, inset,
    padding: { top: 48, right: 64, bottom: bandH + 48, left: 64 },
  }
}

/** The text broken into at most `maxLines` lines that fit `maxWidth` in `font`; the last
 * is shortened with an ellipsis if there is more. New lines in it stay. */
export function wrapLines(text: string, maxWidth: number, font: string, measure: Measure, maxLines: number): string[] {
  if (text === '') return []
  const lines: string[] = []
  for (const paragraph of text.split('\n')) {
    let line = ''
    for (const word of paragraph.split(/\s+/).filter(Boolean)) {
      const next = line === '' ? word : `${line} ${word}`
      if (measure(next, font) <= maxWidth || line === '') line = next
      else { lines.push(line); line = word }
    }
    lines.push(line)
  }
  const truncated = lines.length > maxLines
  const out = lines.slice(0, maxLines)
  const last = out.length - 1
  out[last] = fitText(truncated ? `${out[last]}…` : out[last], maxWidth, font, measure)
  return out
}

/** The tall layout: the band is as high as what is in it. The title wraps onto
 * two lines at a size that fits the width, the date sits under it, and the
 * figures flow onto rows (three at most; any more are dropped, never squeezed). */
function composePortraitLayout(spec: LayoutSpec, measure: Measure): ImageLayout {
  const { width, height } = spec
  const margin = 28
  const inner = width - margin * 2
  const titleFont = font(700, 28)
  const titleLineH = 34
  const datesFont = font(400, 15)
  const labelFont = font(600, 12)
  const valueFont = font(700, 22)
  const attrFont = font(400, 12)
  const rowH = 50
  const gap = 28

  const lines = wrapLines(spec.title, inner, titleFont, measure, 2)
  const top = 20
  const titleY = (i: number) => top + 24 + i * titleLineH
  const datesY = top + 24 + (Math.max(lines.length, 1) - 1) * titleLineH + 24

  // The figures flow left to right and drop to the next row when the next one does not fit.
  const items: { label: string; value: string; w: number }[] = spec.stats.map((s) => {
    const label = s.l.toUpperCase()
    return { label, value: s.v, w: Math.max(measure(label, labelFont), measure(s.v, valueFont)) }
  })
  const placed: { row: number; x: number; item: (typeof items)[number] }[] = []
  let row = 0
  let x = margin
  for (const item of items) {
    if (x > margin && x + item.w > margin + inner) { row++; x = margin }
    if (row >= 3) break
    placed.push({ row, x, item })
    x += item.w + gap
  }
  const rows = placed.length === 0 ? 0 : placed[placed.length - 1].row + 1
  const statsTop = datesY + 18
  const attrH = 30
  const bandH = statsTop + rows * rowH + attrH
  const band: Rect = { x: 0, y: height - bandH, w: width, h: bandH }

  const text = (t: string, xx: number, y: number, f: string, fill: string, align: 'left' | 'right' = 'left'): TextItem => ({ text: t, x: xx, y: band.y + y, font: f, fill, align })
  const [first = '', ...rest] = lines
  const attribution: TextItem = { text: exportImageAttribution(spec.satellite ?? false), x: width - margin, y: height - 12, font: attrFont, fill: MUTED, align: 'right' }
  return {
    width, height, band,
    title: text(first, margin, titleY(0), titleFont, INK),
    titleRest: rest.map((l, i) => text(l, margin, titleY(i + 1), titleFont, INK)),
    dates: text(fitText(spec.dates, inner, datesFont, measure), margin, datesY, datesFont, MUTED),
    stats: placed.map(({ row: r, x: xx, item }) => ({
      label: text(item.label, xx, statsTop + r * rowH + 12, labelFont, MUTED),
      value: text(item.value, xx, statsTop + r * rowH + 12 + 24, valueFont, INK),
    })),
    attribution,
    inset: null,
    padding: { top: 48, right: 64, bottom: bandH + 48, left: 64 },
  }
}

// ── drawing ──────────────────────────────────────────────────────────────

function drawText(ctx: CanvasRenderingContext2D, t: TextItem) {
  ctx.font = t.font
  ctx.fillStyle = t.fill
  ctx.textAlign = t.align
  ctx.textBaseline = 'alphabetic'
  ctx.fillText(t.text, t.x, t.y)
}

/** The band behind the text, the text, and the border round the inset. */
export function paintOverlay(ctx: CanvasRenderingContext2D, layout: ImageLayout) {
  ctx.fillStyle = '#ffffff'
  ctx.fillRect(layout.band.x, layout.band.y, layout.band.w, layout.band.h)
  ctx.fillStyle = '#e7e5e4'
  ctx.fillRect(layout.band.x, layout.band.y, layout.band.w, 1)
  drawText(ctx, layout.title)
  for (const t of layout.titleRest) drawText(ctx, t)
  drawText(ctx, layout.dates)
  for (const s of layout.stats) {
    drawText(ctx, s.label)
    drawText(ctx, s.value)
  }
  drawText(ctx, layout.attribution)
}

export function roundRectPath(ctx: CanvasRenderingContext2D, r: Rect, radius: number) {
  ctx.beginPath()
  ctx.roundRect(r.x, r.y, r.w, r.h, radius)
}


// ── what to trust from the map ───────────────────────────────────────────

/** The part of the rendered map canvas that shows above the band, in canvas
 * pixels. The ratio comes from the canvas itself, not an assumption. */
export function mapCropRect(canvasHeight: number, layoutHeight: number, bandTop: number) {
  const ratio = canvasHeight / layoutHeight
  return { sx: 0, sy: 0, sh: Math.round(bandTop * ratio), ratio }
}

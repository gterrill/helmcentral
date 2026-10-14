import { EXPORT_IMAGE_ATTRIBUTION } from '@/lib/passage-export-style'

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
}

/** Where everything goes on the image. Stats that do not fit are dropped from
 * the end, never squeezed. */
export function composeImageLayout(spec: LayoutSpec, measure: Measure): ImageLayout {
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
    text: EXPORT_IMAGE_ATTRIBUTION, x: width - margin, y: height - 14, font: attrFont, fill: MUTED, align: 'right',
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
  const inset: Rect | null = spec.inset ? { x: width - 24 - insetW, y: 24, w: insetW, h: insetH } : null
  return {
    width, height, band, title, dates, stats, attribution, inset,
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


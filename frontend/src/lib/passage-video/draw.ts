// Canvas drawing for a video frame: the map, the sign cards and pins, the
// boat's dot, the photo of a moment, and the closing card. Plain 2D calls, so
// it runs the same on a page canvas as anywhere else. What goes where is worked
// out in overlay.ts and closing.ts.

import { FONT_STACK, fitText, paintOverlay, roundRectPath, wrapLines, type Measure } from '@/lib/passage-export-layout'
import { measureWithCanvas } from '@/lib/passage-export-image'
import type { ClosingLayout } from '@/lib/passage-video/closing'
import { NOTE_FONT, type Box, type OverlayCard, type OverlayModel } from '@/lib/passage-video/overlay'
import type { VideoSize } from '@/lib/passage-video/schedule'
import { SIGN_CARD_GAP_PX, SIGN_CARD_PAD_PX, SIGN_NOTE_LINE_PX, SIGN_TITLE_LINE_PX } from '@/lib/replay-signs'

// Literal colours, since a canvas cannot read the page's tokens: the light and
// dark values of the card surface the page uses.
export interface Theme {
  card: string
  border: string
  text: string
  muted: string
  placeholder: string
  primary: string
  shadow: string
}

export function themeFor(isDark: boolean): Theme {
  return isDark
    ? { card: '#1c1917', border: '#44403c', text: '#fafaf9', muted: '#a8a29e', placeholder: '#292524', primary: '#38bdf8', shadow: 'rgba(0, 0, 0, 0.5)' }
    : { card: '#ffffff', border: '#e7e5e4', text: '#1c1917', muted: '#57534e', placeholder: '#f5f5f4', primary: '#0369a1', shadow: 'rgba(0, 0, 0, 0.25)' }
}

export { wrapLines }

const font = (weight: number, px: number) => `${weight} ${px}px ${FONT_STACK}`

/** A picture filling a box, cropped to its shape as object-cover does. */
function drawCover(ctx: CanvasRenderingContext2D, image: ImageBitmap, box: Box) {
  const ratio = box.w / box.h
  let sw = image.width
  let sh = image.width / ratio
  if (sh > image.height) { sh = image.height; sw = image.height * ratio }
  ctx.drawImage(image, (image.width - sw) / 2, (image.height - sh) / 2, sw, sh, box.x, box.y, box.w, box.h)
}

function drawCard(ctx: CanvasRenderingContext2D, c: OverlayCard, photos: ReadonlyMap<string, ImageBitmap>, t: Theme) {
  const { rect: r, point } = c
  ctx.save()
  ctx.globalAlpha = c.alpha
  // The leader and its dot, on the point.
  ctx.strokeStyle = t.text
  ctx.lineWidth = 1
  ctx.beginPath()
  ctx.moveTo(point.x, c.below ? r.y : r.y + r.h)
  ctx.lineTo(point.x, point.y)
  ctx.stroke()
  ctx.beginPath()
  ctx.arc(point.x, point.y, 4, 0, Math.PI * 2)
  ctx.fillStyle = t.text
  ctx.fill()
  ctx.strokeStyle = t.card
  ctx.stroke()
  // The card.
  ctx.shadowColor = t.shadow
  ctx.shadowBlur = 6
  ctx.shadowOffsetY = 2
  roundRectPath(ctx, r, 6)
  ctx.fillStyle = t.card
  ctx.fill()
  ctx.shadowColor = 'transparent'
  ctx.strokeStyle = t.border
  ctx.stroke()
  let y = r.y + SIGN_CARD_PAD_PX
  const inner = r.w - 2 * SIGN_CARD_PAD_PX
  if (c.photo !== 'none') {
    const box: Box = { x: r.x + SIGN_CARD_PAD_PX, y, w: inner, h: (inner * 3) / 4 }
    ctx.save()
    roundRectPath(ctx, box, 2)
    ctx.clip()
    const bitmap = photos.get(c.sign.id)
    if (c.photo === 'bitmap' && bitmap) drawCover(ctx, bitmap, box)
    else {
      ctx.fillStyle = t.placeholder
      ctx.fillRect(box.x, box.y, box.w, box.h)
      ctx.fillStyle = t.muted
      ctx.font = font(400, 12)
      ctx.textAlign = 'center'
      ctx.textBaseline = 'alphabetic'
      ctx.fillText('Photo not available', box.x + box.w / 2, box.y + box.h / 2 + 4)
    }
    ctx.restore()
    y += box.h + SIGN_CARD_GAP_PX
  }
  ctx.textAlign = 'left'
  ctx.textBaseline = 'alphabetic'
  const measure: Measure = (text, f) => { ctx.font = f; return ctx.measureText(text).width }
  const titleFont = font(600, 14)
  ctx.font = titleFont
  ctx.fillStyle = t.text
  ctx.fillText(fitText(c.sign.title, inner, titleFont, measure), r.x + SIGN_CARD_PAD_PX, y + SIGN_TITLE_LINE_PX - 5)
  y += SIGN_TITLE_LINE_PX
  ctx.font = NOTE_FONT
  ctx.fillStyle = t.muted
  c.noteLines.forEach((line, i) => {
    ctx.fillText(line, r.x + SIGN_CARD_PAD_PX, y + (i + 1) * SIGN_NOTE_LINE_PX - 4)
  })
  ctx.restore()
}

/** The pin the page shows for a sign whose card is not up: a diamond on its point. */
function drawPin(ctx: CanvasRenderingContext2D, p: { x: number; y: number }, t: Theme) {
  ctx.save()
  ctx.translate(p.x, p.y)
  ctx.rotate(Math.PI / 4)
  roundRectPath(ctx, { x: -8, y: -8, w: 16, h: 16 }, 2)
  ctx.fillStyle = t.primary
  ctx.fill()
  ctx.lineWidth = 2
  ctx.strokeStyle = t.card
  ctx.stroke()
  ctx.restore()
}

/** The boat when it has no model: a dot with a ring. */
function drawBoatDot(ctx: CanvasRenderingContext2D, p: { x: number; y: number }, t: Theme) {
  ctx.save()
  ctx.beginPath()
  ctx.arc(p.x, p.y, 10, 0, Math.PI * 2)
  ctx.globalAlpha = 0.4
  ctx.fillStyle = t.primary
  ctx.fill()
  ctx.globalAlpha = 1
  ctx.beginPath()
  ctx.arc(p.x, p.y, 8, 0, Math.PI * 2)
  ctx.fillStyle = t.primary
  ctx.fill()
  ctx.lineWidth = 2
  ctx.strokeStyle = t.card
  ctx.stroke()
  ctx.restore()
}

function drawMoment(ctx: CanvasRenderingContext2D, m: NonNullable<OverlayModel['moment']>, photos: ReadonlyMap<string, ImageBitmap>, t: Theme) {
  const bitmap = photos.get(m.signId)
  if (!bitmap) return
  const r = m.rect
  ctx.save()
  ctx.shadowColor = t.shadow
  ctx.shadowBlur = 16
  ctx.shadowOffsetY = 4
  roundRectPath(ctx, r, 6)
  ctx.fillStyle = t.card
  ctx.fill()
  ctx.restore()
  ctx.save()
  roundRectPath(ctx, r, 6)
  ctx.clip()
  // The slow zoom is about the middle of the photo.
  ctx.translate(r.x + r.w / 2, r.y + r.h / 2)
  ctx.scale(m.scale, m.scale)
  ctx.translate(-(r.x + r.w / 2), -(r.y + r.h / 2))
  drawCover(ctx, bitmap, r)
  ctx.restore()
  roundRectPath(ctx, r, 6)
  ctx.strokeStyle = t.border
  ctx.lineWidth = 1
  ctx.stroke()
}

/** The signs, the dot and the moment's photo over a frame that already has the map. */
export function drawOverlay(ctx: CanvasRenderingContext2D, model: OverlayModel, photos: ReadonlyMap<string, ImageBitmap>, theme: Theme) {
  ctx.save()
  ctx.scale(model.scale, model.scale)
  if (model.dot) drawBoatDot(ctx, model.dot, theme)
  for (const pin of model.pins) drawPin(ctx, pin, theme)
  for (const card of model.cards) drawCard(ctx, card, photos, theme)
  if (model.moment) drawMoment(ctx, model.moment, photos, theme)
  ctx.restore()
}

/** The closing card: the map frame, then the band and the figures laid out by the PNG export, then the line. */
export function paintClosingCard(ctx: CanvasRenderingContext2D, map: CanvasImageSource, closing: ClosingLayout, size: Pick<VideoSize, 'width' | 'height'>) {
  ctx.drawImage(map, 0, 0, size.width, size.height)
  ctx.save()
  ctx.scale(closing.scale, closing.scale)
  paintOverlay(ctx, closing.layout)
  const b = closing.brand
  ctx.font = b.font
  ctx.fillStyle = b.fill
  ctx.textAlign = b.align
  ctx.textBaseline = 'alphabetic'
  // Over a map of any colour: a soft shadow keeps it readable without making it loud.
  ctx.shadowColor = 'rgba(0, 0, 0, 0.6)'
  ctx.shadowBlur = 4
  ctx.fillText(b.text, b.x, b.y)
  ctx.restore()
}

/** Lays a frame's pictures together on a canvas. */
export interface Compositor {
  /** What the encoder is given. */
  readonly canvas: CanvasImageSource
  /** Measures text as the canvas will draw it, for the closing card's layout. */
  measure: Measure
  /** The map, then the signs and the moment's photo over it. */
  frame(map: CanvasImageSource, model: OverlayModel): void
  /** The map, then the band and the figures of the closing card. */
  closing(map: CanvasImageSource, layout: ClosingLayout): void
}

/** The canvas the encoder is given: the map and everything laid over it, on a 2D canvas the size of the video. */
export function createCanvasCompositor(size: VideoSize, opts: { isDarkTheme: boolean; photos: ReadonlyMap<string, ImageBitmap> }): Compositor {
  const canvas = document.createElement('canvas')
  canvas.width = size.width
  canvas.height = size.height
  const ctx = canvas.getContext('2d')
  if (!ctx) throw new Error('This browser cannot draw the video frames')
  ctx.imageSmoothingQuality = 'high'
  const theme = themeFor(opts.isDarkTheme)
  return {
    canvas,
    measure: measureWithCanvas(),
    frame(map, model) {
      ctx.drawImage(map, 0, 0, size.width, size.height)
      drawOverlay(ctx, model, opts.photos, theme)
    },
    closing: (map, layout) => paintClosingCard(ctx, map, layout, size),
  }
}

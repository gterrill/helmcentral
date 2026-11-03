// The small labels over the replay's map: the stay's, centred, and the
// passage's, at the left. They look alike and sit on one line, so how they look
// and where they go is worked out here, in the frame's own pixels, and the
// page (components/passages/map-label) and a video's canvas
// (lib/passage-video/draw) both draw from it. Pure.

import type { InsetCorner } from '@/lib/passage-inset-view'
import { CARD_FADE_S } from '@/lib/replay-signs'

/** How a label looks, for the page and a canvas alike. The colours are the
 * page's card and text colours, so a canvas takes them from its own theme. */
export const MAP_LABEL = {
  /** Font weight. */
  weight: 500,
  /** Corner radius, in the frame's pixels. */
  radius: 6,
  /** How much of the card colour shows behind the text, before the fade. */
  bgAlpha: 0.8,
  /** The line height as a multiple of the font size. */
  lineHeight: 1.4,
  fontPx: 13,
  padX: 12,
  padY: 6,
  /** How far above the foot of the map the top of a label is. */
  aboveFoot: 72,
  /** The space kept between a label and the edge of the map, and between it and the overview map. */
  edge: 24,
  insetGap: 12,
} as const

/** The overview map sits in a corner of the page's map, `h-32 w-44` and 8 px
 * from the side, with the bottom ones 32 px up from the foot. */
const INSET = { w: 176, h: 128, side: 8 } as const

export interface MapLabelBox {
  /** The middle of the label across the frame (`align` 'center'), or its left edge ('left'). */
  x: number
  align: 'left' | 'center'
  /** The top of the label. */
  y: number
  fontPx: number
  padX: number
  padY: number
  /** The widest the text may be before it is shortened. */
  maxW: number
  /** 0 to 1: fades in and out over the time the label is up. */
  alpha: number
}

/** A part of the frame a page keeps for something of its own (the exported card's time readout), in the frame's pixels. */
export interface ReservedArea {
  x: number
  y: number
  w: number
  h: number
}

export interface MapLabelOptions {
  align: MapLabelBox['align']
  /** Seconds the label has been up, and how long it is up for. */
  elapsedS: number
  durationS: number
  /** The corner the overview map is shown in, if it is: in a bottom one the label narrows to keep clear of it. */
  insetCorner?: InsetCorner | null
  /** Areas the page keeps clear (default none), which the label gives way to sideways if it would sit over them. A page that knows where its own overview map really is (the exported card's is smaller than the app's) passes that box here, with no `insetCorner`. */
  reserved?: readonly ReservedArea[]
}

/** Whether a label has room for any text between its padding. With no more width than the padding it would be an empty pill, so the page and a video's canvas draw nothing. */
export const labelHasRoom = (box: Pick<MapLabelBox, 'maxW' | 'padX'>) => box.maxW > 2 * box.padX

/** How tall a label is: one line of its text and the padding. */
const labelHeight = () => MAP_LABEL.fontPx * MAP_LABEL.lineHeight + 2 * MAP_LABEL.padY

/** The style a label is drawn with, for the page (React, the exported card) and anything else that sets CSS: pixels are numbers. */
export function mapLabelStyle(box: MapLabelBox) {
  return {
    left: box.x,
    top: box.y,
    transform: box.align === 'center' ? 'translateX(-50%)' : undefined,
    fontSize: box.fontPx,
    fontWeight: MAP_LABEL.weight,
    lineHeight: MAP_LABEL.lineHeight,
    borderRadius: MAP_LABEL.radius,
    padding: `${box.padY}px ${box.padX}px`,
    maxWidth: box.maxW,
    opacity: box.alpha,
  }
}

/** Where a label sits in a frame of `container`'s size, and how big: quiet,
 * small, low, clear of the cards at the sides and of the overview map in a
 * bottom corner. */
export function mapLabelBox(container: { w: number; h: number }, { align, elapsedS, durationS, insetCorner = null, reserved = [] }: MapLabelOptions): MapLabelBox {
  const e = Math.max(0, Math.min(durationS, elapsedS))
  let left: number = MAP_LABEL.edge
  let right = container.w - MAP_LABEL.edge
  if (insetCorner === 'bottom-left') left = Math.max(left, INSET.side + INSET.w + MAP_LABEL.insetGap)
  if (insetCorner === 'bottom-right') right = Math.min(right, container.w - INSET.side - INSET.w - MAP_LABEL.insetGap)
  const y = container.h - MAP_LABEL.aboveFoot
  // Over something the page keeps clear: the label narrows from the side that thing is on.
  for (const area of reserved) {
    if (area.y < y + labelHeight() && area.y + area.h > y) {
      if (area.x + area.w / 2 < container.w / 2) left = Math.max(left, area.x + area.w + MAP_LABEL.insetGap)
      else right = Math.min(right, area.x - MAP_LABEL.insetGap)
    }
  }
  return {
    x: align === 'center' ? (left + right) / 2 : left,
    align,
    y,
    fontPx: MAP_LABEL.fontPx,
    padX: MAP_LABEL.padX,
    padY: MAP_LABEL.padY,
    maxW: Math.max(0, right - left),
    alpha: Math.max(0, Math.min(1, Math.min(e, durationS - e) / CARD_FADE_S)),
  }
}

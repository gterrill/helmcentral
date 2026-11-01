// The closing card: the last map frame with the PNG export's band across the
// foot of it (title, dates, figures, credit), laid out by the same
// composeImageLayout, and one quiet line over the map. Pure; draw.ts paints it.

import { FONT_STACK, composeImageLayout, type ImageLayout, type Measure, type TextItem } from '@/lib/passage-export-layout'
import type { VideoSize } from '@/lib/passage-video/schedule'

/** The video's pixels to the layout's, which are the PNG export's size. */
export const CLOSING_SCALE = 1.6
export const BRAND_LINE = 'Made with Helmcentral · github.com/gterrill/helmcentral'

export interface ClosingLayout {
  scale: number
  /** The size the layout is worked out in. */
  design: { w: number; h: number }
  layout: ImageLayout
  brand: TextItem
}

/** Soft white over the map, which the drawing gives a shadow so it reads on any colour. */
const BRAND_FILL = 'rgba(255, 255, 255, 0.9)'

export function closingCardLayout(
  size: Pick<VideoSize, 'width' | 'height'>,
  summary: { title: string; dates: string; stats: { l: string; v: string }[] },
  satellite: boolean,
  measure: Measure,
): ClosingLayout {
  const design = { w: Math.round(size.width / CLOSING_SCALE), h: Math.round(size.height / CLOSING_SCALE) }
  const layout = composeImageLayout({ width: design.w, height: design.h, title: summary.title, dates: summary.dates, stats: summary.stats, inset: false, satellite, portrait: size.height > size.width }, measure)
  // Over the foot of the map, just above the band, so it never meets the figures or the credit.
  const brand: TextItem = { text: BRAND_LINE, x: layout.title.x, y: layout.band.y - 12, font: `400 12px ${FONT_STACK}`, fill: BRAND_FILL, align: 'left' }
  return { scale: CLOSING_SCALE, design, layout, brand }
}

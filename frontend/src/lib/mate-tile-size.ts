import { DASHBOARD_WIDGET_DEFAULT_SIZE } from '@/lib/dashboard-widgets'
import { gridPixelHeight } from '@/lib/grid-metrics'
import type { TileCard } from '@/lib/mate-cards'

/** The row above a tile card: its Live marker or captured time. A fixed height so the placeholder and the card agree. */
export const TILE_CARD_HEADER_PX = 24

/** The tile's cell height on the dashboard, which is the box a tile card gives it (ADR 0229). */
export function tileBoxHeight(tile: TileCard['tile']): number {
  return gridPixelHeight(DASHBOARD_WIDGET_DEFAULT_SIZE[tile].h)
}

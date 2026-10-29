import { tileBoxHeight, TILE_CARD_HEADER_PX } from '@/lib/mate-tile-size'
import type { TileCard } from '@/lib/mate-cards'

/** Holds the place of a tile card while its code loads, at the card's own height. */
export function AssistantTileCardFallback({ tile }: { tile: TileCard['tile'] }) {
  return (
    <div
      data-testid="assistant-tile-fallback"
      aria-hidden="true"
      className="min-w-0 rounded-md border border-border bg-card"
      style={{ height: tileBoxHeight(tile) + TILE_CARD_HEADER_PX }}
    />
  )
}

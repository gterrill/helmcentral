import { DepthTideTile, LiveDepthTideTile } from '@/components/depth-tide-tile'
import { useVesselStateSelector } from '@/hooks/use-vessel-state'
import { formatTimeOfDay, selectTimeZone } from '@/lib/vessel-time'
import { useAppConfig } from '@/hooks/use-app-config'
import { defaultTide } from '@/hooks/use-tide-today'
import { DASHBOARD_WIDGET_LABELS } from '@/lib/dashboard-widgets'
import type { TileCard } from '@/lib/mate-cards'
import { tileBoxHeight, TILE_CARD_HEADER_PX } from '@/lib/mate-tile-size'

/**
 * A dashboard tile in a Mate reply (ADR 0227). The newest reply draws the live
 * tile. An older one draws the same tile on the figures the server read when
 * Mate answered, so the picture still agrees with the words above it. Read
 * only: nothing on it opens or configures anything.
 */
export function AssistantTileCard({ card, live }: { card: TileCard; live: boolean }) {
  const { ui } = useAppConfig()
  const timeZone = useVesselStateSelector(selectTimeZone)
  const isImperialDistance = ui.distanceUnits === 'imperial'
  // The tile fills its box, so the box has the height of the tile's cell on
  // the dashboard. Its pt-2 is the inset the title pill hangs into there.
  const height = tileBoxHeight(card.tile)

  return (
    <section aria-label={`${DASHBOARD_WIDGET_LABELS[card.tile]} tile`} data-testid="assistant-tile-card" className="min-w-0">
      <div data-testid="assistant-tile-header" className="flex items-center justify-end text-xs text-muted-foreground" style={{ height: TILE_CARD_HEADER_PX }}>
        {live ? <span className="font-medium text-foreground">Live</span> : <time dateTime={card.capturedAt}>{formatTimeOfDay(card.capturedAt, timeZone)}</time>}
      </div>
      <div data-testid="assistant-tile-box" className="min-w-0 pt-2" style={{ height }}>
        {live ? (
          <LiveDepthTideTile isImperialDistance={isImperialDistance} />
        ) : (
          <DepthTideTile
            depth={card.snapshot.depth}
            lastUpdateAgeS={card.snapshot.depthLastUpdateAgeS}
            navigationState={card.snapshot.navigationState}
            tide={card.snapshot.tide ?? defaultTide}
            depthTrend={card.snapshot.depthTrend}
            isImperialDistance={isImperialDistance}
            timeZone={timeZone}
          />
        )}
      </div>
    </section>
  )
}

import { Ship } from 'lucide-react'

import { type DistanceUnits } from '@/config/app-config'
import { type AlarmState } from '@/hooks/use-alarms'
import { type NearbyVessel, type NearbyVesselFeed } from '@/hooks/use-nearby-vessels'
import { bearingDeg } from '@/lib/geo'
import {
  aisSilenceMessage,
  COLLISION_TIER_STYLE,
  collisionTier,
  formatRelativeDuration,
  formatVesselRange,
  formatSearchRange,
  formatSightingSummary,
} from '@/lib/nearby-vessel-display'
import { formatBearing } from '@/lib/poi'
import { cn } from '@/lib/utils'

// The heading both list groups (places and vessels) use.
export const nearbyGroupHeadingClass = 'text-[10px] uppercase tracking-[0.14em] text-muted-foreground'

// The Vessels group lists at most this many contacts, alarmed first and then
// nearest; "+N more" beside the heading says when others qualified. Four
// two-line rows fit beside the places on the 272 px wall tile with no scrolling,
// but a line of their own for "+N more" would not.
const VESSEL_LIST_SIZE = 4

// A vessel's row says how old its position is only past this. Stationary AIS
// reports can be about three minutes apart, so a younger position is normal
// and not worth the operator's attention.
const VESSEL_OLD_POSITION_S = 180

/**
 * What the group says in place of its rows, or null when it lists vessels. The
 * tile asks too, so it only puts the group first when alarm rows really show.
 */
export function vesselGroupMessage({
  vessels,
  gnssCriticalAlert,
  feed,
  limitM,
  distanceUnits,
}: {
  vessels: readonly NearbyVessel[]
  gnssCriticalAlert: boolean
  feed: NearbyVesselFeed | null
  limitM: number
  distanceUnits: DistanceUnits
}): string | null {
  if (feed === null) return 'Loading…'
  // First among the failures: when SignalK itself is down the backend raises
  // the GNSS alert too, and blaming the fix would send the operator to the
  // wrong fault.
  if (feed.source !== 'signalk') return 'AIS unavailable'
  // Under the -1,-1 position sentinel every contact's range is wrong.
  if (gnssCriticalAlert) return 'Vessels need a GNSS fix'
  if (vessels.length === 0) {
    // An empty list after a quiet receiver is not an empty sea.
    const silent = aisSilenceMessage(feed.aisHeardAgeS)
    if (silent !== null) return silent
    return feed.maxRangeM === null ? 'No vessels in range' : `No vessels within ${formatSearchRange(limitM, distanceUnits)}`
  }
  return null
}

/**
 * The Vessels group of the list: the AIS contacts with their sighting history.
 * A plain, non-interactive display (the kiosk has no input). When it cannot
 * list vessels it says why, in this order: feed not heard yet, feed not coming
 * from the live AIS source, no trustworthy position, nothing in range.
 */
export function VesselGroup({
  vessels,
  ownLat,
  ownLon,
  gnssCriticalAlert,
  feed,
  limitM,
  distanceUnits,
  aisCollisionAlarms,
  stacked,
  first = false,
}: {
  vessels: NearbyVessel[]
  ownLat: number | null
  ownLon: number | null
  gnssCriticalAlert: boolean
  feed: NearbyVesselFeed | null
  limitM: number
  distanceUnits: DistanceUnits
  aisCollisionAlarms?: ReadonlyMap<string, AlarmState>
  stacked: boolean
  /** Stacked above the places: the divider goes under the group, not over it. */
  first?: boolean
}) {
  const message = vesselGroupMessage({ vessels, gnssCriticalAlert, feed, limitM, distanceUnits })
  const shown = message === null ? vessels.slice(0, VESSEL_LIST_SIZE) : []
  // Boats the backend's cap left out of the feed count as more: they exist.
  const more = message === null ? vessels.length - shown.length + (feed?.cappedCount ?? 0) : 0

  return (
    <div
      data-testid="poi-vessel-group"
      className={cn(
        'flex min-h-0 min-w-0 flex-col gap-0.5',
        !stacked ? 'border-l border-border/60 pl-2' : first ? 'border-b border-border/60 pb-1' : 'border-t border-border/60 pt-1',
      )}
    >
      <div className="flex items-baseline justify-between gap-1">
        <span className={nearbyGroupHeadingClass}>Vessels</span>
        {more > 0 && (
          <span data-testid="poi-vessel-more" className="text-xs text-muted-foreground">+{more} more</span>
        )}
      </div>
      {message !== null ? (
        <div
          data-testid="poi-vessel-state"
          className="flex flex-1 items-center justify-center text-center text-xs text-muted-foreground"
        >
          {message}
        </div>
      ) : (
        shown.map((vessel) => (
          <VesselListRow
            key={vessel.id}
            vessel={vessel}
            ownLat={ownLat}
            ownLon={ownLon}
            distanceUnits={distanceUnits}
            stacked={stacked}
            alarmState={aisCollisionAlarms?.get(vessel.id)}
          />
        ))
      )}
    </div>
  )
}

function VesselListRow({
  vessel,
  ownLat,
  ownLon,
  distanceUnits,
  stacked,
  alarmState,
}: {
  vessel: NearbyVessel
  ownLat: number | null
  ownLon: number | null
  distanceUnits: DistanceUnits
  /** Stacked panes are tall and have the width: a long name may take a second line. Side by side it stays on one. */
  stacked: boolean
  alarmState: AlarmState | undefined
}) {
  const tier = collisionTier(alarmState)
  const tierStyle = tier ? COLLISION_TIER_STYLE[tier] : null
  // The bearing is worked out here from own ship to the contact; the feed
  // gives only the range. Without both positions it is left out, not guessed.
  const bearing = ownLat !== null && ownLon !== null && vessel.lat !== undefined && vessel.lon !== undefined
    ? formatBearing(bearingDeg(ownLat, ownLon, vessel.lat, vessel.lon))
    : null
  const oldPosition = vessel.age_seconds > VESSEL_OLD_POSITION_S ? `position ${formatRelativeDuration(vessel.age_seconds)} old` : null
  const detail = [oldPosition, formatSightingSummary(vessel)].filter((part) => part !== null).join(' · ')
  return (
    <div
      data-testid="poi-vessel-row"
      role={tierStyle ? 'group' : undefined}
      aria-label={tierStyle ? `${vessel.name}, ${tierStyle.label.toLowerCase()}` : undefined}
      className={cn(
        'flex items-start gap-1.5 rounded-xs border py-0.5 pr-1 pl-0.5',
        tierStyle ? tierStyle.row : 'border-transparent',
      )}
    >
      <Ship className={cn('mt-0.5 h-3.5 w-3.5 shrink-0', tierStyle ? tierStyle.text : 'text-muted-foreground')} />
      <div className="min-w-0 flex-1">
        <div className="flex items-baseline justify-between gap-1">
          <span className={cn('min-w-0 text-sm font-semibold text-foreground', stacked ? 'line-clamp-2 break-words' : 'truncate')}>{vessel.name}</span>
          <span className="shrink-0 text-xs tabular-nums text-muted-foreground">
            {formatVesselRange(vessel.range_m, distanceUnits)}{bearing ? ` ${bearing}` : ''}
          </span>
        </div>
        {(tierStyle || detail !== '') && (
          <div className="truncate text-xs text-muted-foreground">
            {tierStyle && <span className={cn('font-semibold', tierStyle.text)}>{tierStyle.label}{detail !== '' ? ' · ' : ''}</span>}
            {detail}
          </div>
        )}
      </div>
    </div>
  )
}

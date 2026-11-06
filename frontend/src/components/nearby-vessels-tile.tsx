import { Ship } from 'lucide-react'
import { useVesselStateSelector } from '@/hooks/use-vessel-state'
import { formatDayTimeIn, selectTimeZone } from '@/lib/vessel-time'
import { memo, useMemo, useState, type ComponentProps } from 'react'

import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { Tile } from '@/components/ui/tile'
import { type DistanceUnits } from '@/config/app-config'
import { type AlarmState, useAisCollisionAlarms } from '@/hooks/use-alarms'
import { useVesselSightings, type VesselSighting } from '@/hooks/use-vessel-sightings'
import { useNearbyVessels, type NearbyVessel } from '@/hooks/use-nearby-vessels'
import { formatCoordinate } from '@/lib/format'
import {
  aisSilenceMessage,
  cappedBoatsMessage,
  COLLISION_TIER_STYLE,
  collisionTier,
  formatRelativeAge,
  formatSearchRange,
  formatSightingSummary,
  sortByCollisionAlarm,
} from '@/lib/nearby-vessel-display'
import { formatDataAge, isStale } from '@/lib/staleness'
import { cn } from '@/lib/utils'

// vesselContactKey mirrors the backend's vesselContactKey resolution
// (backend/nearby_contacts.go): the vessel's MMSI. The backend only ever
// increments seen_count for MMSI-keyed vessels, and this is only called from
// VesselHistoryPopover, which is only rendered when seen_count > 0 (see
// below), so vessel.mmsi is guaranteed present by the time this runs.
function vesselContactKey(vessel: NearbyVessel): string {
  return vessel.mmsi ?? ''
}

function formatSightingTime(seenAt: string, timeZone: string | undefined) {
  const parsedMs = Date.parse(seenAt)
  if (!Number.isFinite(parsedMs)) {
    return seenAt
  }
  return formatDayTimeIn(parsedMs, timeZone)
}

function navContextLabel(navContext: string) {
  const normalized = navContext.trim().toLowerCase()
  if (normalized === 'anchored' || normalized === 'moored') {
    return 'At Anchor'
  }
  if (normalized === 'motoring' || normalized === 'underway' || normalized === 'under way using engine') {
    return 'On Passage'
  }
  if (normalized === '') {
    return ''
  }
  return normalized.charAt(0).toUpperCase() + normalized.slice(1)
}

function SightingRow({ sighting }: { sighting: VesselSighting }) {
  const timeZone = useVesselStateSelector(selectTimeZone)
  const label = navContextLabel(sighting.nav_context)
  return (
    <div className="border-b border-border/60 py-1.5 last:border-b-0">
      <p className="text-xs font-medium text-foreground">{formatSightingTime(sighting.seen_at, timeZone)}</p>
      <p className="mt-0.5 font-mono text-xs text-muted-foreground">
        {formatCoordinate(sighting.lat, true)} {formatCoordinate(sighting.lon, false)}
      </p>
      <p className="mt-0.5 text-xs text-muted-foreground">
        {sighting.geoname ? <span>{sighting.geoname}</span> : null}
        {sighting.geoname && label ? ' • ' : ''}
        {label ? <span>{label}</span> : null}
      </p>
    </div>
  )
}

function VesselHistoryPopover({ vessel, children }: { vessel: NearbyVessel; children: React.ReactNode }) {
  const [open, setOpen] = useState(false)
  const key = vesselContactKey(vessel)
  const { sightings, loading } = useVesselSightings(open ? key : null)

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger className="text-left" aria-label={`View sighting history for ${vessel.name}`}>
        {children}
      </PopoverTrigger>
      <PopoverContent className="w-64 p-2" align="start">
        <p className="mb-1 px-1 text-[10px] font-semibold uppercase tracking-[0.1em] text-muted-foreground">Sighting History</p>
        {loading ? <p className="px-1 py-2 text-xs text-muted-foreground">Loading…</p> : null}
        {!loading && sightings.length === 0 ? <p className="px-1 py-2 text-xs text-muted-foreground">No recorded sightings yet</p> : null}
        {!loading && sightings.length > 0 ? (
          <div className="max-h-64 overflow-y-auto px-1">
            {sightings.map((sighting) => (
              <SightingRow key={sighting.seen_at} sighting={sighting} />
            ))}
          </div>
        ) : null}
      </PopoverContent>
    </Popover>
  )
}

type NearbyVesselsTileProps = {
  vessels: NearbyVessel[]
  loading: boolean
  distanceUnits: DistanceUnits
  /**
   * Seconds since the nearby-vessels feed itself last reported, or null if
   * the upstream source publishes no age for it. Distinct from each
   * vessel's own `age_seconds` (how long since that contact was last seen) —
   * this is whether the AIS/radar feed behind the whole list has died.
   * `buildNearbyVesselsPayload` (backend/main.go) carries no such field
   * today, so this is always null until a source actually publishes one.
   */
  lastUpdateAgeS: number | null
  /**
   * Vessel id -> worst live collision-alarm state (ADR 0088), keyed the same
   * way collisionAlarmStatesByVessel keys it and the anchor-watch map reads
   * it. Optional and left undefined by every existing caller/test with no
   * alarm feed wired up, so the tile renders exactly as it did before this
   * prop existed.
   */
  aisCollisionAlarms?: ReadonlyMap<string, AlarmState>
  /**
   * How far the backend searches, in metres, from the nearby-vessels payload.
   * Null when the payload did not carry it, in which case the empty state says
   * "in range" and names no distance: a figure the tile guessed would go stale
   * the day the backend's limit moved.
   */
  maxRangeM?: number | null
  /**
   * Where the backend got the list. "signalk" is the live AIS feed; anything
   * else means it could not read it. Left undefined by callers that have no
   * feed to ask, which skips the check.
   */
  source?: string | null
  /** Seconds since any vessel last reported (-1: none held), from the same payload. */
  aisHeardAgeS?: number | null
  /** Boats near the anchor the feed left out during a watch because it is capped. */
  cappedCount?: number
  /** The cap the backend applied, from the same payload. */
  feedCap?: number | null
  /** The boat has no trustworthy fix: with the feed not from SignalK, that is the likelier cause. */
  gnssCriticalAlert?: boolean
}

function formatRange(rangeMeters: number, distanceUnits: DistanceUnits) {
  if (distanceUnits === 'imperial') {
    return `${Math.round(rangeMeters * 3.28084)} ft`
  }
  return `${Math.round(rangeMeters)} m`
}

export const NearbyVesselsTile = memo(function NearbyVesselsTile({ vessels, loading, distanceUnits, lastUpdateAgeS, aisCollisionAlarms, maxRangeM = null, source, aisHeardAgeS = null, cappedCount = 0, feedCap = null, gnssCriticalAlert = false }: NearbyVesselsTileProps) {
  const feedStale = isStale(lastUpdateAgeS)
  // Why the list is empty when the feed is the reason: a silent receiver must
  // not read as an empty sea.
  // A null source is unknown, not unavailable. The backend also reports
  // backend-fallback when only the boat's own fix is missing, so the GNSS
  // alert is checked first.
  const emptyMessage = typeof source === 'string' && source !== 'signalk'
    ? (gnssCriticalAlert ? 'Vessels need a GNSS fix' : 'AIS unavailable')
    : aisSilenceMessage(aisHeardAgeS)
      ?? (maxRangeM === null ? 'No vessels in range' : `No vessels within ${formatSearchRange(maxRangeM, distanceUnits)}`)
  const sortedVessels = useMemo(() => sortByCollisionAlarm(vessels, aisCollisionAlarms), [vessels, aisCollisionAlarms])

  return (
    <Tile
      title="Nearby Vessels"
      icon={<Ship className="h-3.5 w-3.5 text-gauge-secondary" />}
      stale={feedStale}
      staleLabel={formatDataAge(lastUpdateAgeS)}
      shrinkToContent
    >
      <div className="@container mt-3 space-y-2">
        {sortedVessels.map((vessel) => {
          const tier = collisionTier(aisCollisionAlarms?.get(vessel.id))
          const tierStyle = tier ? COLLISION_TIER_STYLE[tier] : null

          return (
            <div
              key={vessel.id}
              role={tierStyle ? 'group' : undefined}
              aria-label={tierStyle ? `${vessel.name}, ${tierStyle.label.toLowerCase()}` : undefined}
              className={cn(
                'flex items-center justify-between gap-2 rounded-md border px-3 py-2',
                tierStyle ? tierStyle.row : 'bg-muted/45',
              )}
            >
              <div className="min-w-0">
                <p className="truncate font-display text-lg uppercase leading-tight text-foreground @max-[28rem]:whitespace-normal @max-[28rem]:line-clamp-2 @max-[28rem]:break-words">{vessel.name}</p>
                {tierStyle ? (
                  <p className={cn('mt-1 text-[10px] font-semibold uppercase tracking-[0.16em]', tierStyle.text)}>{tierStyle.label}</p>
                ) : null}
                <p className="mt-1 text-xs text-muted-foreground">({formatRelativeAge(vessel.age_seconds)})</p>
                {typeof vessel.seen_count === 'number' && vessel.seen_count > 0 ? (
                  <VesselHistoryPopover vessel={vessel}>
                    <p className="mt-1 text-xs text-muted-foreground underline decoration-dotted underline-offset-2">
                      {formatSightingSummary(vessel)}
                    </p>
                  </VesselHistoryPopover>
                ) : null}
              </div>
              <div className="shrink-0 text-right">
                <p className={cn('font-display text-3xl leading-none', tierStyle ? tierStyle.text : 'text-gauge-secondary')}>{formatRange(vessel.range_m, distanceUnits)}</p>
                {typeof vessel.sog_knots === 'number' ? (
                  <p className={cn('mt-1 text-xs', tierStyle ? tierStyle.text : 'text-gauge-secondary')}>{vessel.sog_knots.toFixed(1)} kts</p>
                ) : null}
              </div>
            </div>
          )
        })}

        {!loading && vessels.length === 0 ? (
          <div className="rounded-md border border-dashed bg-muted/25 px-3 py-4 text-center text-sm text-muted-foreground">
            {emptyMessage}
          </div>
        ) : null}

        {cappedCount > 0 ? (
          <p data-testid="nearby-vessels-capped" className="text-xs text-muted-foreground">
            {cappedBoatsMessage(cappedCount, feedCap)}
          </p>
        ) : null}

        {loading ? (
          <div className="rounded-md border border-dashed bg-muted/25 px-3 py-4 text-center text-sm text-muted-foreground">Loading nearby traffic...</div>
        ) : null}
      </div>
    </Tile>
  )
})

/** The Nearby Vessels list: the contacts from their own stream, and the collision highlight on each row. */
export const LiveNearbyVesselsTile = memo(function LiveNearbyVesselsTile(props: Omit<ComponentProps<typeof NearbyVesselsTile>,
  'aisCollisionAlarms' | 'vessels' | 'loading' | 'lastUpdateAgeS' | 'maxRangeM' | 'source' | 'aisHeardAgeS' | 'cappedCount' | 'feedCap' | 'gnssCriticalAlert'>) {
  const nearby = useNearbyVessels()
  const gnssCriticalAlert = useVesselStateSelector((v) => v.gnssCriticalAlert)
  const aisCollisionAlarms = useAisCollisionAlarms()
  return <NearbyVesselsTile {...props} {...nearby} gnssCriticalAlert={gnssCriticalAlert} aisCollisionAlarms={aisCollisionAlarms} />
})

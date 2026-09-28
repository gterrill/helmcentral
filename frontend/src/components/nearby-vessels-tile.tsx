import { Ship } from 'lucide-react'
import { memo, useMemo, useState } from 'react'

import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { Tile } from '@/components/ui/tile'
import type { DistanceUnits } from '@/config/app-config'
import { ALARM_STATES, type AlarmState } from '@/hooks/use-alarms'
import { useVesselSightings, type VesselSighting } from '@/hooks/use-vessel-sightings'
import type { NearbyVessel } from '@/hooks/use-nearby-vessels'
import { formatCoordinate } from '@/lib/format'
import { formatDataAge, isStale } from '@/lib/staleness'
import { cn } from '@/lib/utils'

// formatRelativeAge renders a seconds count on the shared s/m/h/d ladder.
// Both the server-computed age_seconds (currently capped well under an hour
// by nearbyVesselMaxAge) and formatLastSeen (a client-side delta against
// last_seen_at, which can be days old) share this: age_seconds only ever
// exercises the top of the ladder today, but keeping one implementation
// means a future staleness-cutoff change can't silently reintroduce the
// "2648m ago" bug by falling through a ladder that was never extended.
function formatRelativeAge(seconds: number) {
  const clamped = Math.max(0, seconds)
  if (clamped < 60) {
    return `${clamped}s ago`
  }

  const minutes = Math.floor(clamped / 60)
  if (minutes < 60) {
    return `${minutes}m ago`
  }

  const hours = Math.floor(minutes / 60)
  if (hours < 24) {
    return `${hours}h ago`
  }

  const days = Math.floor(hours / 24)
  return `${days}d ago`
}

function formatLastSeen(lastSeenAt: string, nowMs = Date.now()) {
  const parsedMs = Date.parse(lastSeenAt)
  if (!Number.isFinite(parsedMs)) {
    return 'unknown'
  }

  const deltaSeconds = Math.max(0, Math.floor((nowMs - parsedMs) / 1000))
  return formatRelativeAge(deltaSeconds)
}

// vesselContactKey mirrors the backend's vesselContactKey resolution
// (backend/nearby_contacts.go): the vessel's MMSI. The backend only ever
// increments seen_count for MMSI-keyed vessels, and this is only called from
// VesselHistoryPopover, which is only rendered when seen_count > 0 (see
// below), so vessel.mmsi is guaranteed present by the time this runs.
function vesselContactKey(vessel: NearbyVessel): string {
  return vessel.mmsi ?? ''
}

function formatSightingTime(seenAt: string) {
  const parsedMs = Date.parse(seenAt)
  if (!Number.isFinite(parsedMs)) {
    return seenAt
  }
  return new Date(parsedMs).toLocaleString()
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
  const label = navContextLabel(sighting.nav_context)
  return (
    <div className="border-b border-border/60 py-1.5 last:border-b-0">
      <p className="text-xs font-medium text-foreground">{formatSightingTime(sighting.seen_at)}</p>
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

/**
 * Two-tier collapse of the live collision-alarm ladder for this row (ADR
 * 0140). The anchor-watch map (ADR 0088) paints one red for every state from
 * `alert` through `emergency` — a marker has only one colour to spend, and
 * shape already says "this is AIS." A list row has room for a text label as
 * well as colour, so it keeps the split the map deliberately gives up:
 * `alert`/`warn` read as a still-forming risk (amber, "Collision warning"),
 * `alarm`/`emergency` as an imminent one (red, "Collision alarm") — the same
 * `alarm` cutoff the map uses for its own ring. `normal` is not a state
 * collisionAlarmStatesByVessel actually hands back, but the type doesn't
 * rule it out, so it is treated as no alarm rather than crashing the lookup.
 */
type CollisionTier = 'warn' | 'alarm'

function collisionTier(state: AlarmState | undefined): CollisionTier | null {
  if (state === undefined || state === 'normal') return null
  return ALARM_STATES.indexOf(state) >= ALARM_STATES.indexOf('alarm') ? 'alarm' : 'warn'
}

const COLLISION_TIER_STYLE: Record<CollisionTier, { row: string; text: string; label: string }> = {
  alarm: {
    row: 'border-red-500 bg-red-50 dark:bg-red-950/60',
    text: 'text-red-600 dark:text-red-400',
    label: 'Collision alarm',
  },
  warn: {
    row: 'border-amber-500 bg-amber-50 dark:bg-amber-950/60',
    text: 'text-amber-600 dark:text-amber-400',
    label: 'Collision warning',
  },
}

/**
 * Alarmed vessels first, worst state first; unalarmed vessels — and vessels
 * tied on tier — keep the order the feed gave them (nearest first, set by
 * the backend). Builds a new array rather than sorting `vessels` in place,
 * so the memoised prop from App.tsx is never mutated, and ties break on the
 * original index so the result doesn't depend on the sort being stable.
 */
function sortByCollisionAlarm(vessels: NearbyVessel[], aisCollisionAlarms: ReadonlyMap<string, AlarmState> | undefined): NearbyVessel[] {
  if (!aisCollisionAlarms || aisCollisionAlarms.size === 0) return vessels

  return vessels
    .map((vessel, index) => ({ vessel, index, rank: ALARM_STATES.indexOf(aisCollisionAlarms.get(vessel.id) ?? 'normal') }))
    .sort((a, b) => (b.rank !== a.rank ? b.rank - a.rank : a.index - b.index))
    .map((entry) => entry.vessel)
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
}

function formatRange(rangeMeters: number, distanceUnits: DistanceUnits) {
  if (distanceUnits === 'imperial') {
    return `${Math.round(rangeMeters * 3.28084)} ft`
  }
  return `${Math.round(rangeMeters)} m`
}

export const NearbyVesselsTile = memo(function NearbyVesselsTile({ vessels, loading, distanceUnits, lastUpdateAgeS, aisCollisionAlarms }: NearbyVesselsTileProps) {
  const feedStale = isStale(lastUpdateAgeS)
  const sortedVessels = useMemo(() => sortByCollisionAlarm(vessels, aisCollisionAlarms), [vessels, aisCollisionAlarms])

  return (
    <Tile
      title="Nearby Vessels"
      icon={<Ship className="h-3.5 w-3.5 text-gauge-secondary" />}
      stale={feedStale}
      staleLabel={formatDataAge(lastUpdateAgeS)}
    >
      <div className="mt-3 space-y-2">
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
                <p className="truncate font-display text-lg uppercase leading-none text-foreground">{vessel.name}</p>
                {tierStyle ? (
                  <p className={cn('mt-1 text-[10px] font-semibold uppercase tracking-[0.16em]', tierStyle.text)}>{tierStyle.label}</p>
                ) : null}
                <p className="mt-1 text-xs text-muted-foreground">({formatRelativeAge(vessel.age_seconds)})</p>
                {typeof vessel.seen_count === 'number' && vessel.seen_count > 0 ? (
                  <VesselHistoryPopover vessel={vessel}>
                    <p className="mt-1 text-xs text-muted-foreground underline decoration-dotted underline-offset-2">
                      Seen {vessel.seen_count}x before
                      {typeof vessel.last_seen_at === 'string' && vessel.last_seen_at.trim() !== '' ? `, last ${formatLastSeen(vessel.last_seen_at)}` : ''}
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
          <div className="rounded-md border border-dashed bg-muted/25 px-3 py-4 text-center text-sm text-muted-foreground">No nearby targets</div>
        ) : null}

        {loading ? (
          <div className="rounded-md border border-dashed bg-muted/25 px-3 py-4 text-center text-sm text-muted-foreground">Loading nearby traffic...</div>
        ) : null}
      </div>
    </Tile>
  )
})

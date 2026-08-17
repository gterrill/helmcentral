import { Ship } from 'lucide-react'
import { memo, useState } from 'react'

import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { Tile } from '@/components/ui/tile'
import type { DistanceUnits } from '@/config/app-config'
import { useVesselSightings, type VesselSighting } from '@/hooks/use-vessel-sightings'
import type { NearbyVessel } from '@/hooks/use-nearby-vessels'
import { formatCoordinate } from '@/lib/format'

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
      <p className="mt-0.5 font-mono text-[11px] text-muted-foreground">
        {formatCoordinate(sighting.lat, true)} {formatCoordinate(sighting.lon, false)}
      </p>
      <p className="mt-0.5 text-[11px] text-muted-foreground">
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
}

function formatRange(rangeMeters: number, distanceUnits: DistanceUnits) {
  if (distanceUnits === 'imperial') {
    return `${Math.round(rangeMeters * 3.28084)} ft`
  }
  return `${Math.round(rangeMeters)} m`
}

export const NearbyVesselsTile = memo(function NearbyVesselsTile({ vessels, loading, distanceUnits }: NearbyVesselsTileProps) {
  return (
    <Tile title="Nearby Vessels" icon={<Ship className="h-3.5 w-3.5 text-gauge-secondary" />}>
      <div className="mt-3 space-y-2">
        {vessels.map((vessel) => (
          <div key={vessel.id} className="flex items-center justify-between gap-2 rounded-md border bg-muted/45 px-3 py-2">
            <div className="min-w-0">
              <p className="truncate font-display text-lg uppercase leading-none text-foreground">{vessel.name}</p>
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
              <p className="font-display text-3xl leading-none text-gauge-secondary">{formatRange(vessel.range_m, distanceUnits)}</p>
              {typeof vessel.sog_knots === 'number' ? <p className="mt-1 text-xs text-gauge-secondary">{vessel.sog_knots.toFixed(1)} kts</p> : null}
            </div>
          </div>
        ))}

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

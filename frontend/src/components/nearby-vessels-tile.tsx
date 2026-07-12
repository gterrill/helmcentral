import { Ship } from 'lucide-react'
import { memo } from 'react'

import { Tile } from '@/components/ui/tile'
import type { DistanceUnits } from '@/config/app-config'
import type { NearbyVessel } from '@/hooks/use-nearby-vessels'

function formatAge(ageSeconds: number) {
  if (ageSeconds < 60) {
    return `${ageSeconds}s ago`
  }

  const minutes = Math.floor(ageSeconds / 60)
  return `${minutes}m ago`
}

function formatLastSeen(lastSeenAt: string, nowMs = Date.now()) {
  const parsedMs = Date.parse(lastSeenAt)
  if (!Number.isFinite(parsedMs)) {
    return 'unknown'
  }

  const deltaSeconds = Math.max(0, Math.floor((nowMs - parsedMs) / 1000))
  if (deltaSeconds < 60) {
    return `${deltaSeconds}s ago`
  }

  const minutes = Math.floor(deltaSeconds / 60)
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

type NearbyVesselsTileProps = {
  vessels: NearbyVessel[]
  loading: boolean
  distanceUnits: DistanceUnits
}

function formatRange(rangeFeet: number, distanceUnits: DistanceUnits) {
  if (distanceUnits === 'imperial') {
    return `${Math.round(rangeFeet)} ft`
  }

  const meters = rangeFeet / 3.28084
  return `${Math.round(meters)} m`
}

export const NearbyVesselsTile = memo(function NearbyVesselsTile({ vessels, loading, distanceUnits }: NearbyVesselsTileProps) {
  return (
    <Tile title="Nearby Vessels" icon={<Ship className="h-3.5 w-3.5 text-gauge-secondary" />}>
      <div className="mt-3 space-y-2">
        {vessels.map((vessel) => (
          <div key={vessel.name} className="flex items-center justify-between gap-2 rounded-md border bg-muted/45 px-3 py-2">
            <div className="min-w-0">
              <p className="truncate font-display text-lg uppercase leading-none text-foreground">{vessel.name}</p>
              <p className="mt-1 text-xs text-muted-foreground">({formatAge(vessel.age_seconds)})</p>
              {typeof vessel.seen_count === 'number' && vessel.seen_count > 0 ? (
                <p className="mt-1 text-xs text-muted-foreground">
                  Seen {vessel.seen_count}x before
                  {typeof vessel.last_seen_at === 'string' && vessel.last_seen_at.trim() !== '' ? `, last ${formatLastSeen(vessel.last_seen_at)}` : ''}
                </p>
              ) : null}
            </div>
            <div className="shrink-0 text-right">
              <p className="font-display text-3xl leading-none text-gauge-secondary">{formatRange(vessel.range_ft, distanceUnits)}</p>
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

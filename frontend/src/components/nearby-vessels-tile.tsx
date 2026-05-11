import { Ship } from 'lucide-react'

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

export function NearbyVesselsTile({ vessels, loading, distanceUnits }: NearbyVesselsTileProps) {
  return (
    <Tile title="Nearby Vessels" icon={<Ship className="h-3.5 w-3.5 text-secondary" />}>
      <div className="mt-3 space-y-2">
        {vessels.map((vessel) => (
          <div key={vessel.name} className="flex items-center justify-between gap-2 rounded-md border bg-muted/45 px-3 py-2">
            <div className="min-w-0">
              <p className="truncate font-display text-lg uppercase leading-none text-foreground">{vessel.name}</p>
              <p className="mt-1 text-xs text-muted-foreground">({formatAge(vessel.age_seconds)})</p>
            </div>
            <div className="shrink-0 text-right">
              <p className="font-display text-3xl leading-none text-secondary">{formatRange(vessel.range_ft, distanceUnits)}</p>
              {typeof vessel.sog_knots === 'number' ? <p className="mt-1 text-xs text-secondary">{vessel.sog_knots.toFixed(1)} kts</p> : null}
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
}

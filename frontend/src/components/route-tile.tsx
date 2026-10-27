import { Navigation } from 'lucide-react'
import { memo } from 'react'
import { Tile } from '@/components/ui/tile'
import { useRouteActivation, type ActiveRouteStatus } from '@/hooks/use-route-activation'
import { etaToDestination, etaToRouteEnd } from '@/lib/next-waypoint'
import { formatEtaHours, formatNm } from '@/lib/route-calc'
import { useVesselFields } from '@/hooks/use-vessel-state'

interface RouteTileProps {
  status: ActiveRouteStatus
  latitude: number | null
  longitude: number | null
  speedKts: number
}

interface RouteReadout {
  name: string
  distanceM: number | null
  etaHours: number | null
  note: string | null
}

const MS_PER_HOUR = 3_600_000

/** What SignalK says the boat is heading for: its active route, else the bare next waypoint, else nothing. */
function readout(status: ActiveRouteStatus, latitude: number | null, longitude: number | null, speedKts: number): RouteReadout | null {
  if (status.state === 'unknown') return null
  const now = new Date()
  const havePosition = latitude !== null && longitude !== null
  const sog = speedKts > 0 ? speedKts : null

  if (status.state === 'active' && status.route.waypoints.length > 0) {
    const eta = havePosition ? etaToRouteEnd(latitude, longitude, status, sog, now) : null
    return {
      name: status.route.name.trim() !== '' ? status.route.name : 'Active route',
      distanceM: eta ? eta.distanceM : null,
      etaHours: eta?.etaAt ? (eta.etaAt.getTime() - now.getTime()) / MS_PER_HOUR : null,
      note: null,
    }
  }

  if (status.destination) {
    const eta = havePosition ? etaToDestination(latitude, longitude, status.destination, sog, now) : null
    return {
      name: status.destination.name.trim() !== '' ? status.destination.name : 'Next waypoint',
      distanceM: eta ? eta.distanceM : null,
      etaHours: eta?.etaAt ? (eta.etaAt.getTime() - now.getTime()) / MS_PER_HOUR : null,
      note: status.state === 'inactive' ? (status.routeError ?? null) : null,
    }
  }

  return null
}

export const RouteTile = memo(function RouteTile({ status, latitude, longitude, speedKts }: RouteTileProps) {
  const data = readout(status, latitude, longitude, speedKts)

  return (
    <div data-testid="route-tile">
      <Tile title="Route" icon={<Navigation className="h-3.5 w-3.5 text-gauge-secondary" />}>
        {data ? (
          <>
            <p className="truncate font-display text-2xl leading-none text-gauge-primary">{data.name}</p>
            <div className="mt-2 flex items-center gap-4 text-sm text-muted-foreground tabular-nums">
              <span>{data.distanceM !== null ? formatNm(data.distanceM) : '--'}</span>
              <span>ETA {data.etaHours !== null ? formatEtaHours(data.etaHours) : '--'}</span>
            </div>
            {data.note ? <p className="mt-2 truncate text-xs text-muted-foreground">{data.note}</p> : null}
          </>
        ) : (
          <>
            <p data-testid="route-tile-empty" className="font-display text-2xl leading-none text-gauge-primary">--</p>
            {status.state === 'unknown' && status.reason === 'error' ? <p className="mt-2 text-xs text-muted-foreground">Route unavailable</p> : null}
          </>
        )}
      </Tile>
    </div>
  )
})

const ROUTE_FIELDS = ['latitude', 'longitude', 'speedOverGroundKts'] as const

/** The Route tile: what SignalK is navigating, from its poll, and the fix and speed from vessel state. */
export const LiveRouteTile = memo(function LiveRouteTile() {
  const { status } = useRouteActivation()
  const { latitude, longitude, speedOverGroundKts } = useVesselFields(ROUTE_FIELDS)
  return <RouteTile status={status} latitude={latitude} longitude={longitude} speedKts={speedOverGroundKts ?? 0} />
})

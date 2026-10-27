import { lazy, Suspense, memo, useMemo, type ComponentProps } from 'react'

import { type PoiMapActiveRoute, type PoiMapActiveRouteWaypoint, type PoiMapTileProps } from '@/components/poi-map-tile-impl'
import { TRAIL_POLL_MS } from '@/config/app-config'
import { useAisCollisionAlarms } from '@/hooks/use-alarms'
import { useNearbyVesselList } from '@/hooks/use-nearby-vessels'
import { useRouteActivation } from '@/hooks/use-route-activation'
import { useSelfTrail } from '@/hooks/use-server-trails'
import { useVesselFields } from '@/hooks/use-vessel-state'
import { nextWaypoint, traversalOrder } from '@/lib/next-waypoint'

// Same kiosk bundle-split reasoning as assistant-markdown.tsx /
// help-markdown.tsx: this widget imports maplibre-gl and react-map-gl at
// module scope, and every /kiosk page loaded that whole map-vendor chunk
// (1048 KB raw / 280 KB gzip) at startup even on pages with no map at all,
// because App.tsx mounts PoiMapTile eagerly for any dashboard that has a
// Nearby widget configured. Behind a dynamic import, map-vendor only loads
// once a page that actually renders this tile does.
const PoiMapTileImpl = lazy(() => import('./poi-map-tile-impl'))

export type { PoiMapTileProps, PoiMapActiveRoute, PoiMapActiveRouteWaypoint }

export function PoiMapTile(props: PoiMapTileProps) {
  return (
    <Suspense
      fallback={
        <div
          className="flex h-full min-h-[160px] items-center justify-center rounded-xl border bg-card text-[10px] uppercase tracking-[0.14em] text-muted-foreground"
          data-testid="poi-map-tile-loading"
        >
          Loading map…
        </div>
      }
    >
      <PoiMapTileImpl {...props} />
    </Suspense>
  )
}

const POI_FIELDS = ['latitude', 'longitude', 'headingTrue', 'gnssCriticalAlert', 'positionLastUpdateAgeS'] as const

/**
 * The route SignalK is navigating, as the Nearby map draws it: its waypoints in
 * traversal order and the index of the next one. Null with no active route, or
 * one with no waypoints. `waypoints` is traversalOrder's array, not the
 * authored order, because the next index counts in traversal order (a reversed
 * activation makes the two differ; see next-waypoint.ts).
 */
function useActiveRouteLayer() {
  const { status } = useRouteActivation()
  return useMemo(() => {
    if (status.state !== 'active') return null
    const waypoint = nextWaypoint(status)
    const traversal = traversalOrder(status)
    if (!waypoint || !traversal) return null
    return { name: status.route.name.trim() !== '' ? status.route.name : 'Active route', waypoints: traversal, nextIndex: waypoint.index }
  }, [status])
}

/** The Nearby map tile: own ship from vessel state, the AIS contacts, the collision colours, the active route, and the boat's trail. The trail polls while the tile is mounted, and only when Show trail is on. */
export const LivePoiMapTile = memo(function LivePoiMapTile(props: Omit<ComponentProps<typeof PoiMapTile>,
  'aisCollisionAlarms' | 'selfTrail' | 'nearbyVessels' | 'activeRoute' | (typeof POI_FIELDS)[number]>) {
  const ownShip = useVesselFields(POI_FIELDS)
  const activeRoute = useActiveRouteLayer()
  const nearbyVessels = useNearbyVesselList()
  const aisCollisionAlarms = useAisCollisionAlarms()
  const selfTrail = useSelfTrail(TRAIL_POLL_MS, props.config.showTrail === true)
  return <PoiMapTile {...props} {...ownShip} activeRoute={activeRoute} nearbyVessels={nearbyVessels} aisCollisionAlarms={aisCollisionAlarms} selfTrail={selfTrail} />
})

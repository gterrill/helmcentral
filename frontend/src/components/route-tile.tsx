import { Navigation } from 'lucide-react'
import { memo } from 'react'
import { Tile } from '@/components/ui/tile'
import type { Route } from '@/hooks/use-routes'
import { calculateLegs, calculateRouteTotals, formatNm, formatEtaHours } from '@/lib/route-calc'

interface RouteTileProps {
  speedKts: number
  routes: Route[]
  dashboardRouteId: string | null
  onOpen?: () => void
}

const DEFAULT_PLANNING_SPEED_KTS = 6

export const RouteTile = memo(function RouteTile({ speedKts, routes, dashboardRouteId, onOpen }: RouteTileProps) {
  const route = dashboardRouteId ? routes.find((r) => r.id === dashboardRouteId) : undefined
  if (!route) return null

  const effectiveSpeed = speedKts > 0 ? speedKts : DEFAULT_PLANNING_SPEED_KTS
  const legs = calculateLegs(route.waypoints, effectiveSpeed)
  const totals = calculateRouteTotals(legs)

  return (
    <div
      onClick={onOpen}
      className={onOpen ? 'cursor-pointer transition-opacity hover:opacity-80' : undefined}
      data-testid="route-tile"
    >
      <Tile title="Route" icon={<Navigation className="h-3.5 w-3.5 text-gauge-secondary" />}>
        <p className="truncate font-display text-2xl leading-none text-gauge-primary">{route.name}</p>
        <div className="mt-2 flex items-center gap-4 text-sm text-muted-foreground">
          <span>{formatNm(totals.totalDistanceM)}</span>
          <span>ETA {formatEtaHours(totals.totalEtaHours)}</span>
        </div>
      </Tile>
    </div>
  )
})

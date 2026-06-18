import { ArrowDown, ArrowUp, Trash2 } from 'lucide-react'
import type { RouteLeg, RouteTotals } from '@/lib/route-calc'
import { formatNm, formatEtaHours } from '@/lib/route-calc'
import type { RouteWaypoint } from '@/hooks/use-routes'

export interface RouteSummaryPanelProps {
  waypoints: RouteWaypoint[]
  legs: RouteLeg[]
  totals: RouteTotals
  speedKts: number
  onSpeedChange: (speedKts: number) => void
  onMoveWaypoint: (index: number, direction: 'up' | 'down') => void
  onDeleteWaypoint: (index: number) => void
}

export function RouteSummaryPanel({
  waypoints,
  legs,
  totals,
  speedKts,
  onSpeedChange,
  onMoveWaypoint,
  onDeleteWaypoint,
}: RouteSummaryPanelProps) {
  return (
    <div className="space-y-3">
      <label className="flex items-center gap-2 text-xs uppercase tracking-[0.1em] text-muted-foreground">
        Planning speed
        <input
          type="number"
          min={0}
          step={0.5}
          value={speedKts}
          onChange={(e) => onSpeedChange(Math.max(0, Number(e.target.value)))}
          className="w-20 rounded-md border border-border bg-background/80 px-2 py-1 text-sm font-semibold text-foreground"
          aria-label="Planning speed in knots"
        />
        kts
      </label>

      {waypoints.length === 0 ? (
        <p className="py-4 text-center text-sm text-muted-foreground">
          Click the map to start adding waypoints.
        </p>
      ) : (
        <ul className="space-y-1.5" data-testid="route-waypoint-list">
          {waypoints.map((wp, idx) => {
            const leg = legs[idx]
            return (
              <li key={idx}>
                <div className="flex items-center justify-between gap-2 rounded-md border border-border bg-background/60 px-3 py-2">
                  <div className="flex items-center gap-2">
                    <span className="flex h-6 w-6 items-center justify-center rounded-full bg-primary text-xs font-bold text-primary-foreground">
                      {idx + 1}
                    </span>
                    <span className="text-sm font-medium text-foreground">
                      {wp.name ?? `Waypoint ${idx + 1}`}
                    </span>
                  </div>
                  <div className="flex items-center gap-1">
                    <button
                      type="button"
                      onClick={() => onMoveWaypoint(idx, 'up')}
                      disabled={idx === 0}
                      aria-label={`Move waypoint ${idx + 1} up`}
                      className="flex h-7 w-7 items-center justify-center rounded-md text-muted-foreground hover:bg-muted/50 disabled:opacity-30"
                    >
                      <ArrowUp className="h-3.5 w-3.5" />
                    </button>
                    <button
                      type="button"
                      onClick={() => onMoveWaypoint(idx, 'down')}
                      disabled={idx === waypoints.length - 1}
                      aria-label={`Move waypoint ${idx + 1} down`}
                      className="flex h-7 w-7 items-center justify-center rounded-md text-muted-foreground hover:bg-muted/50 disabled:opacity-30"
                    >
                      <ArrowDown className="h-3.5 w-3.5" />
                    </button>
                    <button
                      type="button"
                      onClick={() => onDeleteWaypoint(idx)}
                      aria-label={`Delete waypoint ${idx + 1}`}
                      className="flex h-7 w-7 items-center justify-center rounded-md text-muted-foreground hover:bg-red-500/10 hover:text-red-600"
                    >
                      <Trash2 className="h-3.5 w-3.5" />
                    </button>
                  </div>
                </div>
                {leg && (
                  <div className="ml-7 mt-1 flex items-center gap-3 px-1 text-xs text-muted-foreground">
                    <span>{formatNm(leg.distanceM)}</span>
                    <span>{Math.round(leg.bearingDeg)}°</span>
                    <span>ETA {formatEtaHours(leg.etaHours)}</span>
                  </div>
                )}
              </li>
            )
          })}
        </ul>
      )}

      {legs.length > 0 && (
        <div className="flex items-center justify-between rounded-md border border-border bg-muted/30 px-3 py-2 text-sm">
          <span className="font-semibold text-foreground">Total</span>
          <span className="text-muted-foreground">
            {formatNm(totals.totalDistanceM)} · ETA {formatEtaHours(totals.totalEtaHours)}
          </span>
        </div>
      )}
    </div>
  )
}

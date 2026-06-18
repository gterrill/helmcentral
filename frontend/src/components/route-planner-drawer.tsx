import { useState } from 'react'
import { Star, Plus, ArrowLeft, Trash2, Navigation, CircleAlert } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { RoutePlannerMap } from '@/components/route-planner-map'
import { RouteSummaryPanel } from '@/components/route-summary-panel'
import type { Route, RouteWaypoint } from '@/hooks/use-routes'
import type { ActiveRouteStatus } from '@/hooks/use-route-activation'
import { calculateLegs, calculateRouteTotals, formatNm, formatEtaHours } from '@/lib/route-calc'

interface RoutePlannerDrawerProps {
  isDarkTheme: boolean
  currentSpeedKts?: number | null
  vesselLat?: number | null
  vesselLon?: number | null
  routes: Route[]
  loading: boolean
  error: string | null
  createRoute: (name: string, waypoints: RouteWaypoint[]) => Promise<Route | null>
  updateRoute: (id: string, patch: Partial<Pick<Route, 'name' | 'waypoints'>>) => Promise<Route | null>
  deleteRoute: (id: string) => Promise<boolean>
  dashboardRouteId: string | null
  onSetDashboardRouteId: (id: string | null) => void
  activationStatus: ActiveRouteStatus
  activating: boolean
  deactivating: boolean
  activateError: string | null
  onActivate: (id: string) => Promise<boolean>
  onDeactivate: () => Promise<boolean>
}

const DEFAULT_PLANNING_SPEED_KTS = 6

export function RoutePlannerDrawer({
  isDarkTheme,
  currentSpeedKts = null,
  vesselLat = null,
  vesselLon = null,
  routes,
  loading,
  error,
  createRoute,
  updateRoute,
  deleteRoute,
  dashboardRouteId,
  onSetDashboardRouteId,
  activationStatus,
  activating,
  deactivating,
  activateError,
  onActivate,
  onDeactivate,
}: RoutePlannerDrawerProps) {
  const [editingId, setEditingId] = useState<string | 'new' | null>(null)
  const [draftName, setDraftName] = useState('')
  const [draftWaypoints, setDraftWaypoints] = useState<RouteWaypoint[]>([])
  const [speedKts, setSpeedKts] = useState(
    currentSpeedKts !== null && currentSpeedKts > 0 ? currentSpeedKts : DEFAULT_PLANNING_SPEED_KTS,
  )
  const [isSaving, setIsSaving] = useState(false)
  const [pendingRouteId, setPendingRouteId] = useState<string | null>(null)

  const legs = calculateLegs(draftWaypoints, speedKts)
  const totals = calculateRouteTotals(legs)

  function startNewRoute() {
    setEditingId('new')
    setDraftName('')
    setDraftWaypoints([])
  }

  function startEditRoute(route: Route) {
    setEditingId(route.id)
    setDraftName(route.name)
    setDraftWaypoints(route.waypoints)
  }

  function cancelEdit() {
    setEditingId(null)
    setDraftName('')
    setDraftWaypoints([])
  }

  async function handleSave() {
    const name = draftName.trim() || 'Untitled Route'
    if (draftWaypoints.length === 0) return
    setIsSaving(true)
    try {
      if (editingId === 'new') {
        await createRoute(name, draftWaypoints)
      } else if (editingId) {
        await updateRoute(editingId, { name, waypoints: draftWaypoints })
      }
      cancelEdit()
    } finally {
      setIsSaving(false)
    }
  }

  async function handleDelete(id: string) {
    await deleteRoute(id)
    if (dashboardRouteId === id) onSetDashboardRouteId(null)
    if (editingId === id) cancelEdit()
  }

  function handleMoveWaypoint(index: number, direction: 'up' | 'down') {
    const targetIndex = direction === 'up' ? index - 1 : index + 1
    if (targetIndex < 0 || targetIndex >= draftWaypoints.length) return
    const next = [...draftWaypoints]
    ;[next[index], next[targetIndex]] = [next[targetIndex], next[index]]
    setDraftWaypoints(next)
  }

  function handleDeleteWaypoint(index: number) {
    setDraftWaypoints(draftWaypoints.filter((_, i) => i !== index))
  }

  function toggleDashboardRoute(id: string) {
    onSetDashboardRouteId(dashboardRouteId === id ? null : id)
  }

  async function handleToggleActivate(routeId: string, isActive: boolean) {
    setPendingRouteId(routeId)
    try {
      if (isActive) {
        await onDeactivate()
      } else {
        await onActivate(routeId)
      }
    } finally {
      setPendingRouteId(null)
    }
  }

  if (editingId !== null) {
    return (
      <div className="flex h-full flex-col gap-3">
        <div className="flex items-center gap-2">
          <button
            type="button"
            onClick={cancelEdit}
            aria-label="Back to route list"
            className="flex h-8 w-8 items-center justify-center rounded-md text-muted-foreground hover:bg-muted/50"
          >
            <ArrowLeft className="h-4 w-4" />
          </button>
          <input
            type="text"
            value={draftName}
            onChange={(e) => setDraftName(e.target.value)}
            placeholder="Route name"
            className="flex-1 rounded-md border border-border bg-background/80 px-3 py-1.5 text-sm font-semibold text-foreground"
            aria-label="Route name"
          />
        </div>

        <div className="grid flex-1 grid-cols-1 gap-3 md:grid-cols-[1.4fr_1fr]">
          <RoutePlannerMap
            waypoints={draftWaypoints}
            onWaypointsChange={setDraftWaypoints}
            isDarkTheme={isDarkTheme}
            vesselLat={vesselLat}
            vesselLon={vesselLon}
            className="min-h-[320px]"
          />
          <RouteSummaryPanel
            waypoints={draftWaypoints}
            legs={legs}
            totals={totals}
            speedKts={speedKts}
            onSpeedChange={setSpeedKts}
            onMoveWaypoint={handleMoveWaypoint}
            onDeleteWaypoint={handleDeleteWaypoint}
          />
        </div>

        <div className="flex justify-end gap-2">
          <Button type="button" variant="outline" onClick={cancelEdit}>
            Cancel
          </Button>
          <Button type="button" onClick={handleSave} disabled={isSaving || draftWaypoints.length === 0}>
            {isSaving ? 'Saving…' : 'Save Route'}
          </Button>
        </div>
      </div>
    )
  }

  return (
    <div className="space-y-3">
      <div className="flex items-center justify-between gap-2">
        <div className="flex items-center gap-2">
          <h3 className="text-xs font-semibold uppercase tracking-[0.16em] text-muted-foreground">Saved Routes</h3>
          {activationStatus.state === 'unknown' && (
            <span
              className="flex items-center gap-1 text-[10px] text-muted-foreground"
              title="Could not confirm the active route from SignalK"
            >
              <CircleAlert className="h-3 w-3" /> status unknown
            </span>
          )}
        </div>
        <Button type="button" size="sm" onClick={startNewRoute}>
          <Plus className="h-4 w-4" /> New Route
        </Button>
      </div>

      {activationStatus.state === 'active' && activationStatus.routeId === null && (
        <p className="rounded-md border border-amber-500/30 bg-amber-500/10 px-3 py-2 text-xs text-amber-700">
          A route is active on SignalK that isn&apos;t one of your saved routes.
        </p>
      )}
      {activateError && (
        <p className="rounded-md border border-red-500/30 bg-red-500/10 px-3 py-2 text-xs text-red-600">{activateError}</p>
      )}

      {loading && routes.length === 0 && (
        <p className="py-8 text-center text-sm text-muted-foreground">Loading routes…</p>
      )}
      {error && <p className="py-2 text-center text-sm text-amber-600">{error}</p>}
      {!loading && routes.length === 0 && !error && (
        <p className="py-8 text-center text-sm text-muted-foreground">
          No saved routes yet. Tap &quot;New Route&quot; to plan one.
        </p>
      )}

      <ul className="space-y-2">
        {routes.map((route) => {
          const routeLegs = calculateLegs(route.waypoints, speedKts)
          const routeTotals = calculateRouteTotals(routeLegs)
          const isOnDashboard = dashboardRouteId === route.id
          const isActive = activationStatus.state === 'active' && activationStatus.routeId === route.id
          const isPending = pendingRouteId === route.id && (activating || deactivating)
          const activationBusy = activating || deactivating
          return (
            <li key={route.id} className="rounded-md border border-border bg-background/60 px-3 py-2">
              <div className="flex items-center justify-between gap-2">
                <button type="button" onClick={() => startEditRoute(route)} className="flex-1 text-left">
                  <p className="flex items-center gap-1.5 text-sm font-semibold text-foreground">
                    {route.name}
                    {isActive && (
                      <span className="rounded bg-emerald-600/15 px-1.5 py-0.5 text-[10px] font-semibold text-emerald-600">
                        ACTIVE
                      </span>
                    )}
                  </p>
                  <p className="text-xs text-muted-foreground">
                    {route.waypoints.length} waypoints · {formatNm(routeTotals.totalDistanceM)} · ETA {formatEtaHours(routeTotals.totalEtaHours)}
                  </p>
                </button>
                <div className="flex items-center gap-1">
                  <button
                    type="button"
                    onClick={() => handleToggleActivate(route.id, isActive)}
                    disabled={activationBusy && !isPending}
                    aria-label={isActive ? `Deactivate ${route.name}` : `Activate ${route.name}`}
                    className={`flex h-8 w-8 items-center justify-center rounded-md disabled:opacity-30 ${isActive ? 'text-emerald-600 hover:bg-emerald-500/10' : 'text-muted-foreground hover:bg-muted/50'}`}
                  >
                    <Navigation className="h-4 w-4" fill={isActive ? 'currentColor' : 'none'} />
                  </button>
                  <button
                    type="button"
                    onClick={() => toggleDashboardRoute(route.id)}
                    aria-label={isOnDashboard ? `Remove ${route.name} from dashboard` : `Show ${route.name} on dashboard`}
                    className={`flex h-8 w-8 items-center justify-center rounded-md ${isOnDashboard ? 'text-primary' : 'text-muted-foreground hover:bg-muted/50'}`}
                  >
                    <Star className="h-4 w-4" fill={isOnDashboard ? 'currentColor' : 'none'} />
                  </button>
                  <button
                    type="button"
                    onClick={() => handleDelete(route.id)}
                    aria-label={`Delete ${route.name}`}
                    className="flex h-8 w-8 items-center justify-center rounded-md text-muted-foreground hover:bg-red-500/10 hover:text-red-600"
                  >
                    <Trash2 className="h-4 w-4" />
                  </button>
                </div>
              </div>
            </li>
          )
        })}
      </ul>
    </div>
  )
}

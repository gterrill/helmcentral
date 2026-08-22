import { Anchor, ChevronLeft, ChevronRight } from 'lucide-react'
import { useCallback, useEffect, useMemo, useRef, useState, type CSSProperties } from 'react'
import type { AnchorConfig } from '@/config/app-config'
import type { AnchorWatchState } from '@/hooks/use-anchor-watch'
import type { TideToday } from '@/hooks/use-tide-today'
import type { GustWindow } from '@/lib/gust-windows'
import type { SeabedType, SeaState } from '@/lib/catenary'
import { buildRodePlan, catenaryMethod, scopeRatio, scopeStatus, type RodePlanInput, type ScopeStatus } from '@/lib/rode-plan'
import { Button } from '@/components/ui/button'
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarGroupContent,
  SidebarGroupLabel,
  SidebarHeader,
} from '@/components/ui/sidebar'

const RODE_PLANNER_OPEN_KEY = 'anchorWatch.rodePlanner.open'
const RODE_PLANNER_CONTENT_ID = 'rode-planner-content'
const METERS_TO_FEET = 3.28084

export interface AnchorRodePlannerProps {
  anchorState: AnchorWatchState
  rodeDeployedM: number
  seaState: SeaState
  seabedType: SeabedType
  depthM: number | null
  windSpeedApparentKts: number | null
  maxGustKts: Record<GustWindow, number | null>
  tide: TideToday | null
  isImperial: boolean
  anchorConfig: AnchorConfig
  bowOffsetM: number
  onUpdateRodeAndConditions: (rodeDeployedM: number, seaState: SeaState, seabedType: SeabedType) => Promise<void>
  onApplyAlarmRadius: (radiusMeters: number) => Promise<void>
}

function toDisplayDistance(meters: number, isImperial: boolean): number {
  return isImperial ? meters * METERS_TO_FEET : meters
}

function scopeBadgeClass(status: ScopeStatus): string {
  switch (status) {
    case 'ok':
      return 'border-emerald-500/40 bg-emerald-500/10 text-emerald-400'
    case 'low':
      return 'border-amber-500/40 bg-amber-500/10 text-amber-400'
    case 'insufficient':
      return 'border-red-500/40 bg-red-500/10 text-red-400'
    default:
      return 'border-muted bg-muted/40 text-muted-foreground'
  }
}

function scopeBadgeLabel(status: ScopeStatus): string {
  switch (status) {
    case 'ok':
      return 'Scope OK'
    case 'low':
      return 'Scope Low'
    case 'insufficient':
      return 'Scope Insufficient'
    default:
      return 'Scope Unknown'
  }
}

export function AnchorRodePlanner({
  anchorState,
  rodeDeployedM,
  seaState,
  seabedType,
  depthM,
  windSpeedApparentKts,
  maxGustKts,
  tide,
  isImperial,
  anchorConfig,
  bowOffsetM,
  onUpdateRodeAndConditions,
  onApplyAlarmRadius,
}: AnchorRodePlannerProps) {
  const [open, setOpen] = useState<boolean>(() => globalThis.localStorage?.getItem(RODE_PLANNER_OPEN_KEY) === 'true')

  useEffect(() => {
    globalThis.localStorage?.setItem(RODE_PLANNER_OPEN_KEY, String(open))
  }, [open])

  const isInactive = anchorState === 'none'
  const inactiveReason = 'Set anchor watch to record deployed rode and apply an alarm radius.'

  const [windOverride, setWindOverride] = useState<number | null>(null)
  const [pendingSeaState, setPendingSeaState] = useState<SeaState>(seaState)
  const [pendingSeabedType, setPendingSeabedType] = useState<SeabedType>(seabedType)
  const [pendingRode, setPendingRode] = useState<number>(Math.max(0, toDisplayDistance(rodeDeployedM, isImperial)))
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  useEffect(() => { setPendingSeaState(seaState) }, [seaState])
  useEffect(() => { setPendingSeabedType(seabedType) }, [seabedType])
  useEffect(() => { setPendingRode(Math.max(0, toDisplayDistance(rodeDeployedM, isImperial))) }, [rodeDeployedM, isImperial])

  const gustSeed1h = maxGustKts['1h']
  const windSeedSource: 'gust' | 'apparent' | null = gustSeed1h !== null ? 'gust' : windSpeedApparentKts !== null ? 'apparent' : null
  const windSeed = gustSeed1h ?? windSpeedApparentKts
  const windKts = windOverride ?? windSeed

  const planInput: RodePlanInput = useMemo(() => ({
    sounderDepthM: depthM,
    bowRollerHeightM: anchorConfig.bowRollerHeightM,
    tide,
    windKts,
    seaState: pendingSeaState,
    seabedType: pendingSeabedType,
    chainSizeMm: anchorConfig.chainSizeMm,
    chainOnboardM: anchorConfig.chainOnboardM,
    windageAreaM2: anchorConfig.windageAreaM2,
    hullType: anchorConfig.hullType,
  }), [depthM, tide, windKts, pendingSeaState, pendingSeabedType, anchorConfig])

  const method = useMemo(() => catenaryMethod(planInput), [planInput])
  const plan = useMemo(() => buildRodePlan(planInput), [planInput])

  const currentScope = rodeDeployedM > 0 && plan !== null ? scopeRatio(rodeDeployedM, plan.depthFromHawseM) : null
  const status = rodeDeployedM > 0 ? scopeStatus(currentScope, plan?.recommendedScope ?? null) : null

  // The swing circle is rode + bow offset + the hull's own length. loaM defaults
  // to 0 and is genuinely unset on real installs, so treating it as zero would
  // report a circle smaller than the boat and let "Apply as alarm radius" shrink
  // a correct alarm circle. Surface the missing input instead of computing with it.
  const loaConfigured = anchorConfig.loaM > 0
  const swingRadiusM = plan !== null && loaConfigured
    ? plan.recommendedRodeM + bowOffsetM + anchorConfig.loaM
    : null

  const handlePersist = useCallback((rodeDisplay: number, nextSeaState: SeaState, nextSeabedType: SeabedType) => {
    // No masking fallback: PATCH /api/anchor-watch 404s with no active watch
    // (backend/anchor.go), so the call site is guarded rather than firing and
    // swallowing the error. Inactive edits stay local-only (see state above).
    if (isInactive) return
    if (debounceRef.current) clearTimeout(debounceRef.current)
    debounceRef.current = setTimeout(() => {
      const normalizedDisplay = Number.isFinite(rodeDisplay) ? Math.max(0, rodeDisplay) : 0
      const rodeMeters = isImperial ? normalizedDisplay / METERS_TO_FEET : normalizedDisplay
      void onUpdateRodeAndConditions(rodeMeters, nextSeaState, nextSeabedType)
    }, 800)
  }, [isImperial, isInactive, onUpdateRodeAndConditions])

  const handleRodeChange = useCallback((raw: string) => {
    const parsed = Number(raw)
    const nextDisplay = Number.isFinite(parsed) ? Math.max(0, parsed) : 0
    setPendingRode(nextDisplay)
    handlePersist(nextDisplay, pendingSeaState, pendingSeabedType)
  }, [handlePersist, pendingSeaState, pendingSeabedType])

  const handleSeaStateChange = useCallback((value: SeaState) => {
    setPendingSeaState(value)
    handlePersist(pendingRode, value, pendingSeabedType)
  }, [handlePersist, pendingRode, pendingSeabedType])

  const handleSeabedChange = useCallback((value: SeabedType) => {
    setPendingSeabedType(value)
    handlePersist(pendingRode, pendingSeaState, value)
  }, [handlePersist, pendingRode, pendingSeaState])

  const handleApplyAlarmRadius = useCallback(() => {
    if (isInactive || swingRadiusM === null) return
    void onApplyAlarmRadius(swingRadiusM)
  }, [isInactive, swingRadiusM, onApplyAlarmRadius])

  const unit = isImperial ? 'ft' : 'm'

  if (!open) {
    return (
      <button
        type="button"
        aria-expanded={false}
        aria-controls={RODE_PLANNER_CONTENT_ID}
        aria-label="Expand Rode Planner"
        onClick={() => setOpen(true)}
        className="flex w-10 shrink-0 flex-col items-center gap-2 rounded-xl border bg-sidebar py-3 text-sidebar-foreground hover:bg-sidebar-accent"
      >
        <Anchor className="h-4 w-4" />
        <ChevronLeft className="h-4 w-4" />
      </button>
    )
  }

  return (
    <div className="w-full shrink-0 lg:w-[--sidebar-width]" style={{ '--sidebar-width': '20rem' } as CSSProperties}>
      <Sidebar side="right" collapsible="none" className="h-full w-full rounded-xl border">
        <SidebarHeader className="flex-row items-center justify-between border-b">
          <span className="px-1 text-[10px] font-semibold uppercase tracking-[0.16em] text-muted-foreground">Rode Planner</span>
          <Button
            type="button"
            variant="ghost"
            size="icon"
            aria-label="Collapse rode planner"
            aria-expanded
            aria-controls={RODE_PLANNER_CONTENT_ID}
            className="h-7 w-7"
            onClick={() => setOpen(false)}
          >
            <ChevronRight className="h-4 w-4" />
          </Button>
        </SidebarHeader>

        <SidebarContent id={RODE_PLANNER_CONTENT_ID}>
          <SidebarGroup>
            <SidebarGroupLabel>Conditions</SidebarGroupLabel>
            <SidebarGroupContent className="flex flex-col gap-2">
              <label className="rounded-md border bg-background/60 px-3 py-2 text-left">
                <p className="text-[10px] uppercase tracking-[0.16em] text-muted-foreground">Depth</p>
                <p className="font-display text-lg text-gauge-secondary tabular-nums">
                  {plan !== null ? toDisplayDistance(plan.planningDepthM, isImperial).toFixed(1) : '—'}
                  <span className="ml-1 text-xs text-muted-foreground">{unit}</span>
                </p>
                <p className="text-[10px] text-muted-foreground">
                  {plan === null
                    ? 'no depth reading'
                    : plan.depthSource === 'tide'
                      ? `sounder ${depthM !== null ? toDisplayDistance(depthM, isImperial).toFixed(1) : '—'} ${unit} + tide to high`
                      : 'sounder only — no tide station'}
                </p>
              </label>

              <label className="rounded-md border bg-background/60 px-3 py-2 text-left">
                <p className="text-[10px] uppercase tracking-[0.16em] text-muted-foreground">Wind (kts)</p>
                <input
                  type="number"
                  min={0}
                  value={windKts ?? ''}
                  placeholder="—"
                  onChange={(e) => {
                    const parsed = Number(e.target.value)
                    setWindOverride(e.target.value === '' ? null : (Number.isFinite(parsed) ? parsed : null))
                  }}
                  className="mt-1 w-full rounded-md border bg-background/70 px-2 py-1.5 text-sm font-display tabular-nums focus:outline-none focus:ring-1 focus:ring-ring"
                />
                <p className="mt-1 text-[10px] text-muted-foreground">
                  {windOverride !== null
                    ? 'edited'
                    : windSeedSource === 'gust'
                      ? 'seeded from 1h max gust'
                      : windSeedSource === 'apparent'
                        ? 'seeded from apparent wind'
                        : 'no wind data'}
                </p>
              </label>

              <label className="rounded-md border bg-background/60 px-3 py-2 text-left">
                <p className="text-[10px] uppercase tracking-[0.16em] text-muted-foreground">Sea State</p>
                <select
                  aria-label="Sea state"
                  className="mt-1 w-full rounded-md border bg-background/70 px-2 py-1.5 text-sm focus:outline-none focus:ring-1 focus:ring-ring"
                  value={pendingSeaState}
                  onChange={(e) => handleSeaStateChange(e.target.value as SeaState)}
                >
                  <option value="calm">Calm</option>
                  <option value="choppy">Choppy</option>
                  <option value="rough">Rough</option>
                  <option value="storm">Storm</option>
                </select>
              </label>

              <label className="rounded-md border bg-background/60 px-3 py-2 text-left">
                <p className="text-[10px] uppercase tracking-[0.16em] text-muted-foreground">Seabed</p>
                <select
                  aria-label="Seabed"
                  className="mt-1 w-full rounded-md border bg-background/70 px-2 py-1.5 text-sm focus:outline-none focus:ring-1 focus:ring-ring"
                  value={pendingSeabedType}
                  onChange={(e) => handleSeabedChange(e.target.value as SeabedType)}
                >
                  <option value="sand">Sand</option>
                  <option value="mud">Mud</option>
                  <option value="rock">Rock</option>
                  <option value="grass">Grass</option>
                </select>
              </label>
            </SidebarGroupContent>
          </SidebarGroup>

          <SidebarGroup>
            <SidebarGroupLabel>{method?.label ?? 'Catenary Method'}</SidebarGroupLabel>
            <SidebarGroupContent>
              {method?.unavailableReason ? (
                <p className="rounded-md border bg-background/60 px-3 py-2 text-xs text-muted-foreground">
                  Unavailable — {method.unavailableReason}
                </p>
              ) : (
                <div className="rounded-md border bg-background/60 px-3 py-2">
                  <p className="text-[10px] uppercase tracking-[0.16em] text-muted-foreground">Pay Out</p>
                  <p className="font-display text-3xl text-gauge-primary tabular-nums">
                    {Math.round(toDisplayDistance(method!.recommendedRodeM, isImperial))}
                    <span className="ml-1 text-base text-muted-foreground">{unit}</span>
                  </p>
                  <div className="mt-2 flex items-center justify-between gap-2 text-xs text-muted-foreground">
                    <span>Scope {method!.scopeRatio.toFixed(1)}:1</span>
                    {swingRadiusM !== null && (
                      <span>Swing {Math.round(toDisplayDistance(swingRadiusM, isImperial))} {unit}</span>
                    )}
                  </div>
                  {!loaConfigured && (
                    <p className="mt-2 rounded-md border border-amber-500/40 bg-amber-500/10 px-2 py-1 text-[10px] text-amber-500">
                      Set boat length (LOA) in Settings → Anchor to get a swing radius.
                    </p>
                  )}
                  {plan?.exceedsChainOnboard && (
                    <p className="mt-2 rounded-md border border-amber-500/40 bg-amber-500/10 px-2 py-1 text-[10px] text-amber-500">
                      Needs {Math.round(toDisplayDistance(plan.recommendedRodeM, isImperial))} {unit} — only {Math.round(toDisplayDistance(anchorConfig.chainOnboardM, isImperial))} {unit} aboard
                    </p>
                  )}
                  <p className="mt-2 text-[10px] text-muted-foreground">{method!.note}</p>
                </div>
              )}
            </SidebarGroupContent>
          </SidebarGroup>

          <SidebarGroup>
            <SidebarGroupLabel>Deployed</SidebarGroupLabel>
            <SidebarGroupContent className="flex flex-col gap-2">
              <label className="rounded-md border bg-background/60 px-3 py-2 text-left">
                <p className="text-[10px] uppercase tracking-[0.16em] text-muted-foreground">Rode Deployed ({unit})</p>
                <input
                  aria-label="Rode deployed"
                  type="number"
                  min={0}
                  step={isImperial ? 5 : 1}
                  value={Number.isFinite(pendingRode) ? pendingRode : 0}
                  onChange={(e) => handleRodeChange(e.target.value)}
                  disabled={isInactive}
                  className="mt-1 w-full rounded-md border bg-background/70 px-2 py-1.5 text-sm font-display tabular-nums focus:outline-none focus:ring-1 focus:ring-ring disabled:opacity-50"
                />
              </label>

              {rodeDeployedM > 0 && status !== null && (
                <div className={`rounded-md border px-2 py-1 text-center text-[11px] font-semibold uppercase tracking-[0.12em] ${scopeBadgeClass(status)}`}>
                  {scopeBadgeLabel(status)}
                </div>
              )}

              {isInactive && (
                <p className="text-[10px] text-muted-foreground">{inactiveReason}</p>
              )}
            </SidebarGroupContent>
          </SidebarGroup>
        </SidebarContent>

        <SidebarFooter className="border-t">
          <Button
            type="button"
            variant="secondary"
            size="sm"
            disabled={isInactive || swingRadiusM === null}
            onClick={handleApplyAlarmRadius}
          >
            Apply as alarm radius
          </Button>
        </SidebarFooter>
      </Sidebar>
    </div>
  )
}

import { ChevronLeft, ChevronRight, Link } from 'lucide-react'
import { useCallback, useEffect, useMemo, useRef, useState, type CSSProperties } from 'react'
import type { AnchorConfig } from '@/config/app-config'
import type { AnchorWatchState } from '@/hooks/use-anchor-watch'
import type { TideToday } from '@/hooks/use-tide-today'
import type { GustWindow } from '@/lib/gust-windows'
import type { SeabedType, SeaState } from '@/lib/catenary'
import { buildRodePlan, resolvePlanningWindBand, rodeMethods, scopeRatio, scopeStatus, WIND_BANDS, type RodePlanInput, type ScopeStatus } from '@/lib/rode-plan'
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
  // SignalK's design.length.overall (ADR 0047). Null when unpublished — this
  // is the LOA fallback source when settings.anchor.loa_m is unset (0).
  vesselLengthOverallM: number | null
  // The forecast band, controlled by the host (App.tsx) so the tile's and
  // drawer's Scope rows can plan against the same band the operator picked
  // here instead of each seeding its own raw wind. Null means "no explicit
  // pick yet" — resolvePlanningWindBand falls back to the live seed.
  windBandId: string | null
  onWindBandChange: (bandId: string) => void
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
  vesselLengthOverallM,
  windBandId,
  onWindBandChange,
  onUpdateRodeAndConditions,
  onApplyAlarmRadius,
}: AnchorRodePlannerProps) {
  const [open, setOpen] = useState<boolean>(() => globalThis.localStorage?.getItem(RODE_PLANNER_OPEN_KEY) === 'true')

  useEffect(() => {
    globalThis.localStorage?.setItem(RODE_PLANNER_OPEN_KEY, String(open))
  }, [open])

  const isInactive = anchorState === 'none'
  const inactiveReason = 'Set anchor watch to record deployed rode and apply an alarm radius.'

  const [pendingSeaState, setPendingSeaState] = useState<SeaState>(seaState)
  const [pendingSeabedType, setPendingSeabedType] = useState<SeabedType>(seabedType)
  const [pendingRode, setPendingRode] = useState<number>(Math.max(0, toDisplayDistance(rodeDeployedM, isImperial)))
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  useEffect(() => { setPendingSeaState(seaState) }, [seaState])
  useEffect(() => { setPendingSeabedType(seabedType) }, [seabedType])
  useEffect(() => { setPendingRode(Math.max(0, toDisplayDistance(rodeDeployedM, isImperial))) }, [rodeDeployedM, isImperial])

  // The dropdown starts on whichever band the live seed (1h gust, else
  // apparent) falls in, and stays there until the operator picks another —
  // planning for the forecast band, not the reading at this instant.
  // resolvePlanningWindBand is the one place this rule lives, shared with
  // the map's Scope row (computeScopeRecommendation) so a band picked here
  // reaches it instead of it seeding its own raw wind.
  const selectedBand = resolvePlanningWindBand(maxGustKts, windSpeedApparentKts, windBandId)
  // Null only when there is no wind reading and no choice yet, which the
  // methods below report as "no wind data" rather than inventing a band.
  const windKts = selectedBand?.planKts ?? null

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

  const methodResults = useMemo(() => rodeMethods.map((method) => method(planInput)), [planInput])
  const plan = useMemo(() => buildRodePlan(planInput), [planInput])

  const currentScope = rodeDeployedM > 0 && plan !== null ? scopeRatio(rodeDeployedM, plan.depthFromHawseM) : null
  const status = rodeDeployedM > 0 ? scopeStatus(currentScope, plan?.recommendedScope ?? null) : null

  // The swing circle is rode + bow offset + the hull's own length. loaM defaults
  // to 0 and is genuinely unset on real installs, so treating it as zero would
  // report a circle smaller than the boat and let "Apply as alarm radius" shrink
  // a correct alarm circle. Surface the missing input instead of computing with it.
  //
  // LOA precedence (ADR 0047): an explicit settings.anchor.loa_m override always
  // wins when set; otherwise fall back to SignalK's design.length.overall, which
  // is unaffected by which sensor/antenna published it — unlike gps_from_bow_m,
  // there is no equivalent trap here, so this source is safe to adopt.
  const loaSource: 'settings' | 'signalk' | null = anchorConfig.loaM > 0
    ? 'settings'
    : vesselLengthOverallM !== null && vesselLengthOverallM > 0
      ? 'signalk'
      : null
  const resolvedLoaM = loaSource === 'settings'
    ? anchorConfig.loaM
    : loaSource === 'signalk'
      ? vesselLengthOverallM!
      : null
  const loaConfigured = resolvedLoaM !== null

  // Apply as alarm radius must use the swing of whichever method the operator
  // has configured (anchor.scope_method), never the other one's figure — the
  // panel shows both methods' swings side by side, and applying the one the
  // operator isn't looking at would contradict the caption right above the
  // button (ADR 0059 §4, ADR 0047). rodeMethods always yields one result for
  // 'catenary' and one for 'ratio' (see rodeMethods above), and scopeMethod is
  // typed to exactly those two ids, so this always finds a match.
  const configuredMethodResult = methodResults.find((result) => result !== null && result.id === anchorConfig.scopeMethod)!
  const configuredSwingRadiusM = !configuredMethodResult.unavailableReason && resolvedLoaM !== null
    ? configuredMethodResult.recommendedRodeM + bowOffsetM + resolvedLoaM
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
    if (isInactive || configuredSwingRadiusM === null) return
    void onApplyAlarmRadius(configuredSwingRadiusM)
  }, [isInactive, configuredSwingRadiusM, onApplyAlarmRadius])

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
        <Link className="h-4 w-4" />
        <ChevronLeft className="h-4 w-4" />
      </button>
    )
  }

  return (
    <div className="w-full shrink-0 lg:w-[--sidebar-width]" style={{ '--sidebar-width': '20rem' } as CSSProperties}>
      <Sidebar side="right" collapsible="none" className="h-full w-full rounded-xl border">
        <SidebarHeader className="flex-row items-center justify-between border-b">
          {/* A chain link, the rode itself — same icon the collapsed rail
              shows, so the expanded and collapsed states read as one control. */}
          <span className="inline-flex items-center gap-1.5 px-1 text-[10px] font-semibold uppercase tracking-[0.16em] text-muted-foreground">
            <Link className="h-3.5 w-3.5 shrink-0" />
            Rode Planner
          </span>
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
            {/* 2x2 from `lg` up, where this is a 20rem sidebar: depth beside
                wind (the two planning inputs), sea state beside seabed (the two
                holding inputs). Below `lg` the planner is full-width above the
                map and keeps the single-column stack. min-w-0 on each cell so a
                long caption can't widen its 1fr track past half the sidebar. */}
            <SidebarGroupContent className="grid grid-cols-1 gap-2 lg:grid-cols-2">
              <label className="min-w-0 rounded-md border bg-background/60 px-3 py-2 text-left">
                <p className="text-[10px] uppercase tracking-[0.16em] text-muted-foreground">Depth</p>
                <p className="font-display text-lg text-gauge-secondary tabular-nums">
                  {plan !== null ? toDisplayDistance(plan.planningDepthM, isImperial).toFixed(1) : '—'}
                  <span className="ml-1 text-xs text-muted-foreground">{unit}</span>
                </p>
                {/* The headline figure is the planning depth, so the caption
                    only has to say which of the two it is. The sounder-only
                    case still names its reason: the substitution has to stay
                    visible (ADR 0047), it just doesn't need the arithmetic. */}
                <p className="text-[10px] text-muted-foreground">
                  {plan === null
                    ? 'no depth reading'
                    : plan.depthSource === 'tide'
                      ? 'Tide adjusted'
                      : 'sounder only — no tide station'}
                </p>
              </label>

              <label className="min-w-0 rounded-md border bg-background/60 px-3 py-2 text-left">
                <p className="text-[10px] uppercase tracking-[0.16em] text-muted-foreground">Wind</p>
                <select
                  aria-label="Forecast wind"
                  className="mt-1 w-full rounded-md border bg-background/70 px-2 py-1.5 text-sm focus:outline-none focus:ring-1 focus:ring-ring"
                  value={selectedBand?.id ?? ''}
                  onChange={(e) => onWindBandChange(e.target.value)}
                >
                  {/* Only reachable with no wind reading at all; picking any
                      band replaces it and it never comes back. */}
                  {selectedBand === null && <option value="">—</option>}
                  {WIND_BANDS.map((band) => (
                    <option key={band.id} value={band.id}>{band.label}</option>
                  ))}
                </select>
              </label>

              <label className="min-w-0 rounded-md border bg-background/60 px-3 py-2 text-left">
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

              <label className="min-w-0 rounded-md border bg-background/60 px-3 py-2 text-left">
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

          {methodResults.map((result) => {
            if (result === null) return null
            // Swing (that method's own recommended rode + bow offset + LOA)
            // renders in every group — each method proposes its own circle.
            // Chain-onboard is genuinely per-method — each method can
            // recommend a different rode, so each can separately outgrow
            // what's aboard — and stays here. LOA provenance and its warning
            // describe the single shared LOA input, not anything specific to
            // a method, so they render once in SidebarFooter instead,
            // directly above Apply, where they gate it (ADR 0059 §4).
            const methodSwingRadiusM = !result.unavailableReason && resolvedLoaM !== null
              ? result.recommendedRodeM + bowOffsetM + resolvedLoaM
              : null
            return (
              <SidebarGroup key={result.id}>
                <SidebarGroupLabel>{result.label}</SidebarGroupLabel>
                <SidebarGroupContent>
                  {result.unavailableReason ? (
                    <p className="rounded-md border bg-background/60 px-3 py-2 text-xs text-muted-foreground">
                      Unavailable — {result.unavailableReason}
                    </p>
                  ) : (
                    <div className="rounded-md border bg-background/60 px-3 py-2">
                      <p className="text-[10px] uppercase tracking-[0.16em] text-muted-foreground">Pay Out</p>
                      <p className="font-display text-3xl text-gauge-primary tabular-nums">
                        {Math.round(toDisplayDistance(result.recommendedRodeM, isImperial))}
                        <span className="ml-1 text-base text-muted-foreground">{unit}</span>
                      </p>
                      <div className="mt-2 flex items-center justify-between gap-2 text-xs text-muted-foreground">
                        <span>Scope {result.scopeRatio.toFixed(1)}:1</span>
                        {methodSwingRadiusM !== null && (
                          <span>Swing {Math.round(toDisplayDistance(methodSwingRadiusM, isImperial))} {unit}</span>
                        )}
                      </div>
                      {result.recommendedRodeM > anchorConfig.chainOnboardM && (
                        <p className="mt-2 rounded-md border border-amber-500/40 bg-amber-500/10 px-2 py-1 text-[10px] text-amber-500">
                          Needs {Math.round(toDisplayDistance(result.recommendedRodeM, isImperial))} {unit} — only {Math.round(toDisplayDistance(anchorConfig.chainOnboardM, isImperial))} {unit} aboard
                        </p>
                      )}
                      <p className="mt-2 text-[10px] text-muted-foreground">{result.note}</p>
                    </div>
                  )}
                </SidebarGroupContent>
              </SidebarGroup>
            )
          })}

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
          {/* LOA is a shared input, not a per-method fact, so its provenance
              and warning are stated once here — directly above the control
              they gate — rather than repeated inside every method group
              (ADR 0059 §4). */}
          {resolvedLoaM !== null && loaSource !== null && (
            <p className="mt-1 text-[10px] text-muted-foreground">
              LOA {Number(toDisplayDistance(resolvedLoaM, isImperial).toFixed(1))} {unit} from {loaSource === 'settings' ? 'settings' : 'SignalK'}
            </p>
          )}
          {!loaConfigured && (
            <p className="mt-2 rounded-md border border-amber-500/40 bg-amber-500/10 px-2 py-1 text-[10px] text-amber-500">
              Set boat length (LOA) in Settings → Anchor to get a swing radius.
            </p>
          )}
          {/* Names which method's figure Apply will use, or why it can't yet
              (ADR 0047, ADR 0059 §4) — the operator should never have to
              guess which of the two swings shown above just got applied.
              When LOA itself is unresolved the warning above already says
              why, so this slot stays quiet instead of repeating it. */}
          {configuredMethodResult.unavailableReason ? (
            <p className="text-[10px] text-muted-foreground">
              {configuredMethodResult.label} unavailable — {configuredMethodResult.unavailableReason}
            </p>
          ) : configuredSwingRadiusM !== null ? (
            <p className="text-[10px] text-muted-foreground">
              Applies the {configuredMethodResult.label} swing, {Math.round(toDisplayDistance(configuredSwingRadiusM, isImperial))} {unit}
            </p>
          ) : null}
          <Button
            type="button"
            variant="secondary"
            size="sm"
            disabled={isInactive || configuredSwingRadiusM === null}
            onClick={handleApplyAlarmRadius}
          >
            Apply as alarm radius
          </Button>
        </SidebarFooter>
      </Sidebar>
    </div>
  )
}

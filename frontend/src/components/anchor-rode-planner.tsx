import { ChevronLeft, ChevronRight, Link } from 'lucide-react'
import { useCallback, useEffect, useMemo, useRef, useState, type CSSProperties } from 'react'
import type { AnchorConfig, ScopeMethod } from '@/config/app-config'
import type { AnchorWatchState } from '@/hooks/use-anchor-watch'
import type { TideToday } from '@/hooks/use-tide-today'
import type { GustWindow } from '@/lib/gust-windows'
import type { SeabedType, SeaState } from '@/lib/catenary'
import {
  buildRodePlan,
  maxExpectedDepthM,
  resolvePlanningDepth,
  resolvePlanningWindBand,
  rodeMethods,
  scopeRatio,
  scopeStatus,
  tideHeightFtOrNull,
  WIND_BANDS,
  type PlanningDepthDatum,
  type RodeMethodResult,
  type RodePlanInput,
  type ScopeStatus,
} from '@/lib/rode-plan'
import { Button } from '@/components/ui/button'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
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
  // The live sounder reading — consulted directly while not anchored (ADR
  // 0063 resolvePlanningDepth), but no longer the only depth this component
  // plans against.
  depthM: number | null
  // The planning depth: seeded from the depth at the moment the anchor was
  // dropped and editable from there, and the tide height alongside it — a
  // contemporaneous pair (ADR 0063), never re-derived from the current tide
  // reading. Both null when nothing has been recorded (legacy record, or no
  // reading was available at the moment of drop).
  planningDepthM: number | null
  planningTideHeightFt: number | null
  // Routes to a PATCH when anchored, to React state when not (App.tsx
  // decides which).
  onPlanningDepthChange: (depthM: number, tideHeightFt: number | null) => void
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
  planningDepthM,
  planningTideHeightFt,
  onPlanningDepthChange,
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
  const isAnchored = !isInactive
  const inactiveReason = 'Set anchor watch to record deployed rode and apply an alarm radius.'

  const [pendingSeaState, setPendingSeaState] = useState<SeaState>(seaState)
  const [pendingSeabedType, setPendingSeabedType] = useState<SeabedType>(seabedType)
  const [pendingRode, setPendingRode] = useState<number>(Math.max(0, toDisplayDistance(rodeDeployedM, isImperial)))
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  // Depth gets its own debounce ref, not handlePersist/debounceRef's: that
  // one's `if (isInactive) return` guard exists because PATCH 404s with no
  // watch. For depth, inactive is a valid destination (session state in
  // App.tsx), not a dead end — see persistPlanningDepth below.
  const depthDebounceRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  useEffect(() => { setPendingSeaState(seaState) }, [seaState])
  useEffect(() => { setPendingSeabedType(seabedType) }, [seabedType])
  useEffect(() => { setPendingRode(Math.max(0, toDisplayDistance(rodeDeployedM, isImperial))) }, [rodeDeployedM, isImperial])

  // debounceRef never had unmount cleanup before this change, and adding a
  // second timer doubles the exposure: a stray PATCH firing after Raise
  // unmounts the drawer would 404 against a deleted watch.
  useEffect(() => {
    return () => {
      if (debounceRef.current) clearTimeout(debounceRef.current)
      if (depthDebounceRef.current) clearTimeout(depthDebounceRef.current)
    }
  }, [])

  // The one place depth precedence lives (ADR 0063) — the recorded planning
  // depth while anchored, the live sounder while not, with
  // anchored-and-nothing-recorded resolving to null rather than silently
  // falling back to the live sounder.
  const resolvedDepth: PlanningDepthDatum | null = useMemo(() => resolvePlanningDepth({
    isAnchored,
    liveDepthM: depthM,
    planningDepthM,
    planningTideHeightFt,
    tide,
  }), [isAnchored, depthM, planningDepthM, planningTideHeightFt, tide])

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
    depth: resolvedDepth,
    isAnchored,
    bowRollerHeightM: anchorConfig.bowRollerHeightM,
    tide,
    windKts,
    seaState: pendingSeaState,
    seabedType: pendingSeabedType,
    chainSizeMm: anchorConfig.chainSizeMm,
    chainOnboardM: anchorConfig.chainOnboardM,
    windageAreaM2: anchorConfig.windageAreaM2,
    hullType: anchorConfig.hullType,
  }), [resolvedDepth, isAnchored, tide, windKts, pendingSeaState, pendingSeabedType, anchorConfig])

  const methodResults = useMemo(() => rodeMethods.map((method) => method(planInput)), [planInput])
  const plan = useMemo(() => buildRodePlan(planInput), [planInput])

  // The configured method leads the tab strip. It is the one "Apply as alarm
  // radius" uses (ADR 0059 §4), so it is the one the planner should open on —
  // the operator shouldn't have to hunt for the figure the button below will
  // apply. Sort is stable, so the other method keeps its rodeMethods order.
  const orderedMethodResults = useMemo(() => {
    const present = methodResults.filter((result): result is RodeMethodResult => result !== null)
    return [...present].sort((a, b) =>
      Number(b.id === anchorConfig.scopeMethod) - Number(a.id === anchorConfig.scopeMethod))
  }, [methodResults, anchorConfig.scopeMethod])

  // Seeded from settings and re-seeded when settings change, the same shape
  // as pendingSeaState/pendingSeabedType above: changing the method in
  // Settings should move the planner to that tab rather than strand it on a
  // method the operator has moved off.
  const [activeMethodId, setActiveMethodId] = useState<ScopeMethod>(anchorConfig.scopeMethod)
  useEffect(() => { setActiveMethodId(anchorConfig.scopeMethod) }, [anchorConfig.scopeMethod])

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

  // ADR 0063 — the editable Depth input holds the RAW reading
  // (resolvedDepth.depthM), never plan.planningDepthM, which is already
  // tide-corrected: typing over a corrected figure and storing the result
  // would feed the rise back in and compound it on every render. The
  // corrected figure moves into the caption below instead.
  const seedDepthM = resolvedDepth?.depthM ?? null
  const [depthInputValue, setDepthInputValue] = useState<string>('')

  // Re-seeds from the datum's VALUE, not the object — resolvedDepth is a
  // fresh object every render (an unchanged 10s poll still builds a new
  // one), so keying this off seedDepthM (a primitive) is what lets a poll
  // that returns the same number write an identical string and React bail
  // out, rather than clobbering the field mid-typing.
  useEffect(() => {
    setDepthInputValue(seedDepthM !== null ? toDisplayDistance(seedDepthM, isImperial).toFixed(1) : '')
  }, [seedDepthM, isImperial])

  const persistPlanningDepth = useCallback((nextDepthM: number) => {
    // Stamped with the tide right now, at the moment it was entered —
    // useTideToday never returns null, so tideHeightFtOrNull is the one
    // place the -1-sentinel-to-null rule lives (ADR 0063).
    const tideHeightFt = tideHeightFtOrNull(tide)
    if (isInactive) {
      // No PATCH target when inactive — the planning depth lives in
      // App.tsx's React state (a pre-drop what-if), a valid destination
      // rather than a dead end, so it fires immediately instead of
      // debouncing a write that has nothing to 404 against.
      onPlanningDepthChange(nextDepthM, tideHeightFt)
      return
    }
    if (depthDebounceRef.current) clearTimeout(depthDebounceRef.current)
    depthDebounceRef.current = setTimeout(() => {
      onPlanningDepthChange(nextDepthM, tideHeightFt)
    }, 800)
  }, [isInactive, onPlanningDepthChange, tide])

  const handleDepthInputChange = useCallback((raw: string) => {
    setDepthInputValue(raw)
    // A momentarily empty field mid-typing is not a request to persist
    // anything — the field always holds a value once one has been typed, so
    // an in-progress edit simply doesn't persist yet.
    if (raw.trim() === '') return
    const parsed = Number(raw)
    if (!Number.isFinite(parsed) || parsed <= 0) return
    const nextDepthM = isImperial ? parsed / METERS_TO_FEET : parsed
    persistPlanningDepth(nextDepthM)
  }, [isImperial, persistPlanningDepth])

  // Whether the rise to the next high got added — the caption's only job is
  // to say so; the corrected figure itself isn't shown here (ADR 0047 still
  // requires the visible fallback to sounder-only when uncorrected).
  const isDepthTideCorrected = useMemo(() => maxExpectedDepthM(resolvedDepth, tide) !== null, [resolvedDepth, tide])

  const depthCaption = (() => {
    if (resolvedDepth === null) {
      return isAnchored ? 'No depth entered — type the depth' : 'no depth reading'
    }
    return isDepthTideCorrected ? 'Tide adjusted' : 'sounder only — no tide station'
  })()

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
                <p className="text-[10px] uppercase tracking-[0.16em] text-muted-foreground">Depth ({unit})</p>
                {/* Holds the RAW reading, never plan.planningDepthM — that
                    figure is already tide-corrected, so typing over it and
                    storing the result would feed the rise back in and
                    compound it on every render (ADR 0063's highest-
                    consequence trap here). The corrected figure lives in the
                    caption below instead. Empty (not prefilled with live
                    depth) when anchored with nothing recorded — that is the
                    strict no-fallback rule made visible, and this input is
                    the remedy. */}
                <input
                  aria-label="Depth"
                  type="number"
                  step={isImperial ? 1 : 0.1}
                  value={depthInputValue}
                  onChange={(e) => handleDepthInputChange(e.target.value)}
                  className="mt-1 w-full rounded-md border bg-background/70 px-2 py-1.5 text-sm font-display tabular-nums focus:outline-none focus:ring-1 focus:ring-ring"
                />
                <p className="mt-1 text-[10px] text-muted-foreground">{depthCaption}</p>
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

          {/* One method on screen at a time. Two full recommendations stacked
              in a 20rem sidebar pushed the deployed-rode input and Apply below
              the fold, and the operator only plans against one method anyway —
              the other is there to compare, not to read alongside. The
              configured method leads and opens first (orderedMethodResults
              above), so the panel starts on the figure Apply will use. */}
          <SidebarGroup>
            <SidebarGroupContent>
              <Tabs value={activeMethodId} onValueChange={(value) => setActiveMethodId(value as ScopeMethod)}>
                {/* The line variant, not the default pill: --sidebar-background
                    is var(--card) — the same 8% lightness as --background — so a
                    bg-background pill would be invisible against the panel it
                    sits on, and --muted being lighter would make the *unselected*
                    tabs the ones that stand out. */}
                <TabsList variant="line">
                  {orderedMethodResults.map((result) => (
                    <TabsTrigger key={result.id} value={result.id} className="text-xs">
                      {result.label}
                    </TabsTrigger>
                  ))}
                </TabsList>

                {orderedMethodResults.map((result) => {
                  // Swing (that method's own recommended rode + bow offset + LOA)
                  // renders in every panel — each method proposes its own circle.
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
                    <TabsContent key={result.id} value={result.id}>
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
                    </TabsContent>
                  )
                })}
              </Tabs>
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

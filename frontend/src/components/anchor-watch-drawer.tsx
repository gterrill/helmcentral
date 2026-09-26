import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { ArrowDown, ArrowUp } from 'lucide-react'
import { toast } from 'sonner'
import type { AnchorConfig } from '@/config/app-config'
import { DRAG_BUFFER_METERS, type AnchorWatchState } from '@/hooks/use-anchor-watch'
import type { AlarmState } from '@/hooks/use-alarms'
import type { AnchorPlacemark } from '@/hooks/use-anchor-placemarks'
import type { NearbyVessel } from '@/hooks/use-nearby-vessels'
import type { RadarInfo, RadarSource, RadarTarget } from '@/hooks/use-radar-targets'
import type { TrailPoint } from '@/hooks/use-server-trails'
import type { TideToday } from '@/hooks/use-tide-today'
import type { GustWindow } from '@/lib/gust-windows'
import type { SeabedType, SeaState } from '@/lib/catenary'
import { AnchorAdjustBar, type AnchorAdjustChip } from '@/components/anchor-adjust-bar'
import { AnchorDropRaiseButton } from '@/components/anchor-drop-raise-button'
import { AnchorRodePlanner } from '@/components/anchor-rode-planner'
import { AnchorWatchMap, type AnchorAdjustDraft, type AnchorWatchMapHandle } from '@/components/anchor-watch-map'
import { Button } from '@/components/ui/button'
import { useAnchorAdjustCommit, type AnchorAdjustTarget } from '@/hooks/use-anchor-adjust-commit'
import { alarmRadiusBounds, adjustWarningActive, buildAdjustCommitTargets, clampRadiusM, radiusStepM, snapRadiusM, type AnchorAdjustRestore } from '@/lib/anchor-adjust'
import { computeLowWaterClearance, lowWaterClearanceReasonLabel, underKeelPhrase } from '@/lib/low-water-clearance'
import { haversineMeters } from '@/lib/geo'
import { computeScopeRecommendation, computeSwingRadiusM, resolveLoaM } from '@/lib/rode-plan'
import { estimateDepthAtNextTurn, nextTideExtreme } from '@/lib/tide-estimate'
import { isRetryableAnchorError } from '@/lib/anchor-request'
import { formatDataAge, isStale } from '@/lib/staleness'
import { hasWebGL2 } from '@/lib/webgl'
import { cn } from '@/lib/utils'
import { feetToMeters, metersToFeet } from '@/lib/units'

/** A tide height in feet, converted to the host's chosen unit — matches depth-tide-tile.tsx's own conversion. */
function tideValueDisplay(heightFt: number, isImperial: boolean): string {
  return isImperial ? heightFt.toFixed(1) : feetToMeters(heightFt).toFixed(2)
}

/**
 * A depth-shaped figure (the hero reading, or the Est. high/low projection)
 * in the host's chosen unit — matches depth-tide-tile.tsx's own conversion,
 * one decimal place either way.
 */
function depthValueDisplay(meters: number, isImperial: boolean): string {
  return (isImperial ? metersToFeet(meters) : meters).toFixed(1)
}

interface AnchorWatchDrawerProps {
  // Resolved by the caller (live fix, falling back to the anchor point), and
  // left null only when neither is available — see the no-fix placeholder
  // below rather than being handed a fabricated 0,0.
  vesselLat: number | null
  vesselLon: number | null
  // Whether vesselLat/vesselLon above are a genuine live fix, as opposed to
  // App.tsx's own anchor-point fallback (`latitude ?? anchorWatch.anchorLat`)
  // or the backend's -1/-1 "no fix" sentinel (which passes an ordinary
  // range check and so isn't null either — see gnss_critical_alert).
  // Defaults true so every existing caller that predates this field keeps
  // its prior behaviour. Adjust's own "Alarm would sound now" check
  // (warningActive below) must not evaluate against a fake boat position —
  // see its own comment for what goes wrong otherwise.
  hasGpsFix: boolean
  vesselHeadingDeg: number | null
  anchorLat: number | null
  anchorLon: number | null
  radiusMeters: number
  depthMeters: number | null
  // Seconds since the depth feed last reported, or null if unknown — feeds
  // the header's own hero-depth staleness badge, the same isStale/
  // formatDataAge contract depth-tide-tile.tsx's own Depth reading uses.
  depthLastUpdateAgeS: number | null
  currentDriftKts: number | null
  currentSetDeg: number | null
  currentDriftImpactKts?: number | null
  distanceMeters: number | null
  bearingDeg: number | null
  bowOffsetM?: number
  bowOffsetApplied?: boolean
  bowOffsetReason?: string
  /** -1 when not read. Adjust's own Undo restore payload needs this alongside the three bowOffset* props above (code-review finding, round 2). Optional/defaulted so every existing caller keeps compiling unchanged. */
  headingAtSetDeg?: number
  /** Empty until the background resolver pins one. Same Undo-restore use as headingAtSetDeg. */
  placeName?: string
  // The watch's set_at, passed straight through to the map: it centres on
  // the anchor whenever the session changes.
  anchorSetAt: string | null
  // useAnchorWatch's own `loaded` — whether the first GET /api/anchor-watch
  // has actually resolved, passed straight through to the map so it can
  // tell "no watch" from "haven't heard back yet" (anchorSetAt alone can't).
  anchorStateKnown: boolean
  // useAnchorWatch's own `error` — set when the persisted watch could not be
  // read back (a damaged anchor_watch.json), naming the file path and the
  // parse error. Optional/nullable so every existing caller that predates
  // this field keeps compiling unchanged.
  error?: string | null
  vesselTrail: () => TrailPoint[]
  aisVessels: NearbyVessel[]
  aisTrails: () => Map<string, TrailPoint[]>
  // Optional, mirroring AnchorWatchMapProps — passed straight through to the
  // map, which colours an AIS marker red while a collision alarm is in force
  // for that vessel id (ADR 0088).
  aisCollisionAlarms?: ReadonlyMap<string, AlarmState>
  // Optional, mirroring AnchorWatchMapProps — a caller with no mayara
  // integration wired up simply omits it.
  radarTargets?: RadarTarget[]
  // Optional, mirroring AnchorWatchMapProps — a caller with no mayara
  // integration wired up simply omits it.
  radars?: RadarInfo[]
  radarSource?: RadarSource
  isDarkTheme: boolean
  showImageryLayer: boolean
  onImageryToggle: (enabled: boolean) => void
  showRadarEcho: boolean
  onRadarEchoToggle: (enabled: boolean) => void
  // Kept for the Rode Planner's "Apply as alarm radius" button
  // (onApplyAlarmRadius below) — the one radius write that predates Adjust
  // mode and still stands alongside it, since the planner's own sidebar
  // isn't part of the Adjust flow.
  onRadiusChange: (radiusMeters: number) => Promise<void>
  // Adjust mode's (ADR 0136) one write: one atomic PATCH of position +
  // radius (useAnchorWatch's own adjustAnchor), used both by Set and by the
  // Undo toast's own re-send of the pre-Adjust values.
  adjustAnchor: (params: AnchorAdjustTarget) => Promise<void>
  onClearAnchor: () => Promise<void> | void
  placemarks?: AnchorPlacemark[]
  onPlacemarkCreate?: (lat: number, lon: number) => void
  onPlacemarkRemove?: (id: string) => void
  isImperial: boolean
  onDropAnchor: () => void
  canDrop: boolean
  anchorState: AnchorWatchState
  rodeDeployedM: number
  seaState: SeaState
  seabedType: SeabedType
  windSpeedApparentKts: number | null
  maxGustKts: Record<GustWindow, number | null>
  tide: TideToday | null
  anchorConfig: AnchorConfig
  vesselLengthOverallM: number | null
  // The vessel's maximum design draft (ADR 0135), read from SignalK. Feeds
  // the low-water clearance warning alongside anchorConfig's own
  // minClearanceAtLowM margin - null when SignalK publishes none, which
  // reads as the warning's own "No draft from the boat" state rather than a
  // silent zero-draft substitution.
  vesselDraftM: number | null
  // Shared with the tile and the Rode Planner (App.tsx owns the state) so
  // all three plan against the same forecast band. Null when there's no
  // explicit pick — computeScopeRecommendation falls back to the live seed.
  windBandId: string | null
  onWindBandChange: (bandId: string) => void
  onUpdateRodeAndConditions: (rodeDeployedM: number, seaState: SeaState, seabedType: SeabedType) => Promise<void>
  // The resolved planning depth (App.tsx owns it — ADR 0063): the persisted
  // watch record while anchored, the not-anchored session what-if otherwise.
  // Feeds both the map's Scope row here and the Rode Planner below it, so the
  // two can't disagree.
  planningDepthM: number | null
  planningTideHeightFt: number | null
  onPlanningDepthChange: (depthM: number, tideHeightFt: number | null) => Promise<void>
}

export function AnchorWatchDrawer({
  vesselLat,
  vesselLon,
  hasGpsFix,
  vesselHeadingDeg,
  anchorLat,
  anchorLon,
  radiusMeters,
  depthMeters,
  depthLastUpdateAgeS,
  currentDriftKts,
  currentSetDeg,
  currentDriftImpactKts = null,
  distanceMeters,
  bearingDeg,
  bowOffsetM = 0,
  bowOffsetApplied = false,
  bowOffsetReason = '',
  headingAtSetDeg = -1,
  placeName = '',
  anchorSetAt,
  anchorStateKnown,
  error = null,
  vesselTrail,
  aisVessels,
  aisTrails,
  aisCollisionAlarms,
  radarTargets,
  radars,
  radarSource,
  isDarkTheme,
  showImageryLayer,
  onImageryToggle,
  showRadarEcho,
  onRadarEchoToggle,
  onRadiusChange,
  onClearAnchor,
  placemarks,
  onPlacemarkCreate,
  onPlacemarkRemove,
  isImperial,
  onDropAnchor,
  canDrop,
  anchorState,
  rodeDeployedM,
  seaState,
  seabedType,
  windSpeedApparentKts,
  maxGustKts,
  tide,
  anchorConfig,
  vesselLengthOverallM,
  vesselDraftM,
  windBandId,
  onWindBandChange,
  onUpdateRodeAndConditions,
  planningDepthM,
  planningTideHeightFt,
  onPlanningDepthChange,
  adjustAnchor,
}: AnchorWatchDrawerProps) {
  const isAnchored = anchorState !== 'none'

  // The Rode Planner's "Apply as alarm radius" is the one surviving radius
  // write in this header now that the interim −/+ stepper (ADR 0133 Phase
  // 1) is gone. useAnchorWatch's updateRadius throws on a failed PATCH
  // rather than silently no-op'ing (impeccable P1); this is where that
  // failure actually gets shown — a toast with Retry, not an unhandled
  // rejection with nothing on screen to show for it. No pending-target
  // bookkeeping is needed here any more: that existed only to make rapid
  // repeated stepper presses step from the latest requested value rather
  // than a stale prop, and Apply is a single, deliberate press, not a
  // stepper.
  const applyRadius = useCallback((nextRadiusMeters: number): Promise<void> => {
    return onRadiusChange(nextRadiusMeters).catch((error: unknown) => {
      const message = error instanceof Error ? error.message : 'Request failed'
      toast.error('Could not set alarm radius', {
        description: message,
        action: isRetryableAnchorError(error)
          ? { label: 'Retry', onClick: () => { void applyRadius(nextRadiusMeters) } }
          : undefined,
      })
    })
  }, [onRadiusChange])

  // Rendered by the map's metric overlay as the Scope row, under Current
  // (ADR 0059 §3) — shared with the tile via computeScopeRecommendation, and
  // with the Rode Planner below via windBandId, so all three plan against
  // the same forecast band and the same resolved depth (ADR 0063).
  const scopeRecommendation = useMemo(
    () =>
      computeScopeRecommendation({
        isAnchored,
        liveDepthM: depthMeters,
        planningDepthM,
        planningTideHeightFt,
        tide,
        maxGustKts,
        windSpeedApparentKts,
        seaState,
        seabedType,
        anchorConfig,
        selectedWindBandId: windBandId,
      }),
    [
      isAnchored,
      depthMeters,
      planningDepthM,
      planningTideHeightFt,
      tide,
      maxGustKts,
      windSpeedApparentKts,
      seaState,
      seabedType,
      anchorConfig,
      windBandId,
    ],
  )

  // Anchor Watch's low-water clearance warning (ADR 0135): projects the
  // depth at the boat's current position (the live sounder, not the
  // resolved planning depth scopeRecommendation above uses) forward to the
  // next low tide and compares it against the operator's configured margin.
  const lowWaterClearance = useMemo(
    () =>
      computeLowWaterClearance({
        depthM: depthMeters,
        tide,
        draftM: vesselDraftM,
        marginM: anchorConfig.minClearanceAtLowM,
        now: new Date(),
      }),
    [depthMeters, tide, vesselDraftM, anchorConfig.minClearanceAtLowM],
  )

  const depthStale = isStale(depthLastUpdateAgeS)
  const depthStaleLabel = formatDataAge(depthLastUpdateAgeS)
  const tideDirection = tide?.tide_direction ?? null
  const tideIsRising = tideDirection === 'Rising'
  const nextTurn = tide !== null ? nextTideExtreme(tide) : null
  const estimatedDepthAtNextTurn = tide !== null ? estimateDepthAtNextTurn(depthMeters, tide) : null

  // ── Adjust mode (ADR 0136) ────────────────────────────────────────────────
  // Only the Anchor Watch page (this drawer) ever gets Adjust — the
  // dashboard tile and the wall kiosk don't render this component at all, so
  // there's nothing further to gate there. canUseMapForAdjust mirrors
  // AnchorWatchMap's own canRenderMap check exactly (both call hasWebGL2()
  // directly rather than threading a prop, since it's a static, memoised
  // browser capability, not something that changes per-render).
  const canUseMapForAdjust = hasWebGL2()
  const [adjustActive, setAdjustActive] = useState(false)
  const [adjustDraft, setAdjustDraft] = useState<AnchorAdjustDraft | null>(null)
  const [confirmingSet, setConfirmingSet] = useState(false)
  const mapHandleRef = useRef<AnchorWatchMapHandle>(null)
  const headerAdjustButtonRef = useRef<HTMLButtonElement>(null)
  // The watch's own values the instant Adjust opened: what Set's "previous"
  // (and so Undo) restores. A ref, not state — it must never itself trigger
  // a re-render, and it is set once on entry and read once on Set, nothing
  // in between needs to react to it changing. Typed with lat/lon required
  // (not AnchorAdjustTarget, whose lat/lon are optional for a radius-only
  // commit target) — openAdjust below only ever populates this once
  // anchorLat/anchorLon are known non-null, and buildAdjustCommitTargets
  // needs a real position to diff the draft against.
  //
  // `facts` (code-review finding, round 2) is this same committed point's
  // bow-offset/heading/place-name facts, captured at the same instant for
  // the same reason — Undo re-sends this position as another
  // position-changing PATCH, and without carrying these along too the
  // backend would stamp its own "placed by hand in Adjust" defaults onto a
  // position Undo is putting back exactly where it was, not making a new
  // hand-placed move (buildAdjustCommitTargets' own `restore`).
  const adjustOpenedFromRef = useRef<{ lat: number; lon: number; radiusMeters: number; facts: AnchorAdjustRestore } | null>(null)

  const { loaM: resolvedLoaM } = resolveLoaM(anchorConfig.loaM, vesselLengthOverallM)
  // The Adjust mode's own radius ceiling/floor (ADR 0133's amendment) — chain
  // onboard plus resolved LOA, the same precedence rule the Rode Planner's
  // own swing circle already uses (lib/rode-plan.ts's resolveLoaM).
  const adjustBounds = useMemo(
    () => alarmRadiusBounds({ chainOnboardM: anchorConfig.chainOnboardM, loaM: resolvedLoaM }),
    [anchorConfig.chainOnboardM, resolvedLoaM],
  )
  const adjustMaxDisabledReason = adjustBounds.maxReason === null
    ? 'Chain onboard + boat length'
    : 'Chain onboard, without boat length'
  const adjustStepM = radiusStepM(isImperial)

  const { committing: adjustCommitting, commit: commitAdjust } = useAnchorAdjustCommit({ adjustAnchor, isImperial })

  // No WebGL2: there is no map, so no map-corner Move icon (AnchorWatchMap's
  // own canRenderMap gating) — this text button is that state's entry point
  // instead. Anchor-gated: there is nothing to adjust with no anchor set.
  const showAdjustTextButton = isAnchored && !canUseMapForAdjust

  // Set when the committed radius, at the moment Adjust opened, was above
  // the current chain+LOA ceiling (possible from before that ceiling
  // existed, e.g. the old −/+ stepper) — the bar's own visible "Was X m,
  // above the maximum" line (code-review finding), so the operator sees why
  // the draft they're looking at is smaller than what they last set rather
  // than a silently substituted number.
  const [aboveMaxOriginalRadiusM, setAboveMaxOriginalRadiusM] = useState<number | null>(null)

  const openAdjust = useCallback(() => {
    if (anchorLat === null || anchorLon === null) return
    adjustOpenedFromRef.current = {
      lat: anchorLat,
      lon: anchorLon,
      radiusMeters,
      facts: { bowOffsetM, bowOffsetApplied, bowOffsetReason, headingAtSetDeg, placeName },
    }
    // Clamped, not the raw committed value: a radius above the current
    // ceiling (chain onboard + LOA) must never be the starting draft — the
    // no-map path has no camera/ring to clamp it for later, and this is also
    // what the with-map path's own AnchorWatchMap entry effect must now
    // match (code-review finding: the two used to disagree, the ring
    // showing the clamped ground radius while this seed reported the raw
    // one as the draft).
    const clampedRadiusM = clampRadiusM(radiusMeters, adjustBounds)
    setAdjustDraft({ lat: anchorLat, lon: anchorLon, radiusM: clampedRadiusM })
    setAboveMaxOriginalRadiusM(radiusMeters > adjustBounds.maxM ? radiusMeters : null)
    setConfirmingSet(false)
    setAdjustActive(true)
  }, [anchorLat, anchorLon, radiusMeters, adjustBounds, bowOffsetM, bowOffsetApplied, bowOffsetReason, headingAtSetDeg, placeName])

  const closeAdjust = useCallback(() => {
    setAdjustActive(false)
    setAdjustDraft(null)
    setAboveMaxOriginalRadiusM(null)
    setConfirmingSet(false)
    adjustOpenedFromRef.current = null
    // AnchorWatchMap's own entry/exit effect returns focus to the Move icon
    // in the with-map path; this is the same courtesy for the no-map path,
    // whose entry point is this header's own text button instead.
    if (!canUseMapForAdjust) headerAdjustButtonRef.current?.focus()
  }, [canUseMapForAdjust])

  // Exit paths beyond Cancel/Set/Escape: raising the anchor, or the watch
  // disappearing under the operator (another client raised it, or the
  // server auto-raised it — ADR 0099). A draft with no committed anchor left
  // to compare against, undo against, or eventually PATCH over is
  // meaningless — closing here is the fail-fast response, not a frozen
  // session nobody can Set or Cancel out of usefully.
  useEffect(() => {
    if (adjustActive && !isAnchored) closeAdjust()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [isAnchored])

  const handleAdjustDraftChange = useCallback((draft: AnchorAdjustDraft) => {
    setAdjustDraft(draft)
  }, [])

  // "Alarm would sound now" — the boat's live distance from the DRAFT anchor
  // against the DRAFT radius, mirroring useAnchorWatch's own dragging
  // predicate exactly (lib/anchor-adjust.ts's adjustWarningActive).
  //
  // Gated on hasGpsFix (code-review finding): vesselLat/vesselLon here are
  // App.tsx's own fix-or-anchor-point value, and with no live fix it can be
  // the anchor's own position (`latitude ?? anchorWatch.anchorLat`, distance
  // ~0 — the warning would wrongly never fire) or the backend's -1/-1 "no
  // fix" sentinel (thousands of km away — the warning would wrongly always
  // fire). Neither is the boat. Whenever hasGpsFix is true, vesselLat/
  // vesselLon are guaranteed to be the real fix (App.tsx's fallback and the
  // sentinel both only apply when there is no fix), so this still reads the
  // same prop, just doesn't trust it without that guarantee.
  const warningActive = adjustActive && adjustDraft !== null && hasGpsFix && vesselLat !== null && vesselLon !== null
    && adjustWarningActive(haversineMeters(vesselLat, vesselLon, adjustDraft.lat, adjustDraft.lon), adjustDraft.radiusM, DRAG_BUFFER_METERS)

  // Resets the moment the condition clears (the plan's own requirement) — a
  // "Set anyway" armed by a boat position or a radius that no longer trips
  // the warning would otherwise sit there needing an un-tap that has nothing
  // left to confirm.
  useEffect(() => {
    if (!warningActive) setConfirmingSet(false)
  }, [warningActive])

  // Both the bar's own +/- and its chip taps land here, and both give an
  // ABSOLUTE target (not a delta) — clamped and snapped to a whole display
  // unit exactly the way the map's own onMove-driven draft already is
  // (lib/anchor-adjust.ts's clampRadiusM/snapRadiusM), so the readout never
  // shows a difference between a value reached by pinch and one reached by
  // tapping + . With a map, the camera drives the actual number (this only
  // eases zoom via the ref; handleAdjustDraftChange reports the result). With
  // no map there is no camera to ease, so this writes the draft directly.
  const applyAdjustRadiusTarget = useCallback((targetRadiusMRaw: number) => {
    const targetRadiusM = snapRadiusM(clampRadiusM(targetRadiusMRaw, adjustBounds), isImperial)
    if (canUseMapForAdjust) {
      mapHandleRef.current?.setAdjustRadius(targetRadiusM)
    } else {
      setAdjustDraft((current) => (current ? { ...current, radiusM: targetRadiusM } : current))
    }
  }, [adjustBounds, isImperial, canUseMapForAdjust])

  const handleStepRadius = useCallback((deltaM: number) => {
    if (!adjustDraft) return
    // With a map, base the step on AnchorWatchMap's own pending-target
    // handle, not adjustDraft.radiusM (code-review finding): the draft only
    // updates once the camera's 150ms ease reports back through a
    // moveend/move event, which a rapid second tap/hold (usePressRepeat's
    // 80ms interval) can outrun — recomputing from adjustDraft.radiusM in
    // that window bases the next step on a stale, mid-ease value instead of
    // where the previous step was actually headed, landing off the exact
    // step grid. See AnchorWatchMapHandle.getAdjustRadiusTarget's own doc
    // comment. With no map there is no ease/lag at all (applyAdjustRadiusTarget
    // writes the draft synchronously), so adjustDraft.radiusM is already
    // exactly right there.
    const baseRadiusM = canUseMapForAdjust
      ? mapHandleRef.current?.getAdjustRadiusTarget() ?? adjustDraft.radiusM
      : adjustDraft.radiusM
    applyAdjustRadiusTarget(baseRadiusM + deltaM)
  }, [adjustDraft, applyAdjustRadiusTarget, canUseMapForAdjust])

  const handleApplyChip = useCallback((valueM: number) => {
    applyAdjustRadiusTarget(valueM)
  }, [applyAdjustRadiusTarget])

  // Set — Enter (via the map's own scoped keydown) and the bar's Set button
  // both call this exact function, so the "needs a second tap while the
  // warning shows" state is never split across two code paths that could
  // disagree about whether it's armed.
  const handleAdjustSet = useCallback(() => {
    if (!adjustDraft || !adjustOpenedFromRef.current) return
    if (warningActive && !confirmingSet) {
      setConfirmingSet(true)
      return
    }
    const previous = adjustOpenedFromRef.current
    // lat/lon travel only when the draft position actually moved
    // (lib/anchor-adjust.ts's buildAdjustCommitTargets, 0.5 m tolerance) —
    // the common case (the bar's own +/-/chips, or the whole no-WebGL2 path,
    // which can only ever change radius) must PATCH radius_meters alone, or
    // the backend treats it as a genuine reposition: resets the self trail
    // and requires a fresh SignalK publish that 502s if SignalK is down for
    // a change that never touched position (code-review finding). Undo
    // mirrors the same decision, built from the same pair, so it can't
    // restore a position Set itself never sent.
    const { set, undo } = buildAdjustCommitTargets(
      previous,
      { lat: adjustDraft.lat, lon: adjustDraft.lon, radiusMeters: adjustDraft.radiusM },
      previous.facts,
    )
    commitAdjust({ draft: set, previous: undo, onSuccess: closeAdjust })
  }, [adjustDraft, warningActive, confirmingSet, commitAdjust, closeAdjust])

  const handleAdjustCancel = useCallback(() => {
    closeAdjust()
  }, [closeAdjust])

  // Chips: "fail visibly, no substitution" (the plan's own words) — each
  // carries its own reason rather than falling back to a computed default
  // when an input is missing.
  const rodeLoaChip: AnchorAdjustChip = useMemo(() => {
    if (rodeDeployedM <= 0) return { label: 'Rode + LOA', valueM: null, reason: 'no rode out recorded' }
    if (resolvedLoaM === null) return { label: 'Rode + LOA', valueM: null, reason: 'no boat length' }
    return { label: 'Rode + LOA', valueM: rodeDeployedM + resolvedLoaM, reason: null }
  }, [rodeDeployedM, resolvedLoaM])

  const plannerSwingChip: AnchorAdjustChip = useMemo(() => {
    if (scopeRecommendation.unavailableReason) {
      return { label: 'Planner swing', valueM: null, reason: scopeRecommendation.unavailableReason }
    }
    const swingM = computeSwingRadiusM(scopeRecommendation.recommendedRodeM, bowOffsetM, resolvedLoaM)
    return swingM === null
      ? { label: 'Planner swing', valueM: null, reason: 'no boat length' }
      : { label: 'Planner swing', valueM: swingM, reason: null }
  }, [scopeRecommendation, bowOffsetM, resolvedLoaM])

  const adjustDisplayRadiusM = adjustDraft?.radiusM ?? radiusMeters

  return (
    <div className="flex h-full flex-col gap-3">
      {/* The backend puts a damaged anchor_watch.json into an explicit error
          state rather than an invented or empty watch — never silently
          shown as the ordinary "no watch set" page. Drop the anchor again to
          recover: setAnchorHere always overwrites the file. */}
      {error && (
        <div role="alert" className="rounded-md border border-red-500/60 bg-red-500/10 px-3 py-2 text-xs text-red-400">
          <p className="font-semibold">Saved anchor watch unreadable</p>
          <p className="mt-1">{error} Drop anchor to start a new watch.</p>
        </div>
      )}
      {bowOffsetApplied && (
        <div className="rounded-md border border-border bg-background/60 px-3 py-2 text-xs text-muted-foreground">
          Anchor point corrected {Math.round(bowOffsetM)}m forward of GPS — radius should cover rode + {Math.round(bowOffsetM)}m.
        </div>
      )}
      {!bowOffsetApplied && bowOffsetReason === 'heading unavailable' && (
        <div className="rounded-md border border-amber-500/40 bg-amber-500/10 px-3 py-2 text-xs text-amber-500">
          Anchor point not bow-corrected — no heading from SignalK.
        </div>
      )}

      <div className="flex min-h-0 flex-1 flex-col gap-3 lg:flex-row">
        <div
          className={cn(
            'flex min-w-0 flex-1 flex-col gap-3',
            // Adjust mode takes over the whole viewport on phones — page
            // chrome (this header, the rode planner sidebar, the Drop/Raise
            // row) hidden, map + bottom bar filling the screen. At lg and up
            // it stays in place in the drawer layout (the plan's own
            // requirement): the fixed positioning and the chrome-hiding
            // below both drop out at the lg breakpoint.
            //
            // z-70, not z-50: the app shell's own top header is z-60 (App.tsx)
            // and its live-alarm banner z-55, both above a bare z-50 — a
            // full-screen takeover has to outrank the chrome it's replacing,
            // not sit under it. z-70 matches the Sheet component's own layer
            // (components/ui/sheet.tsx), the closest existing precedent for
            // "this view now owns the whole screen."
            adjustActive && 'fixed inset-0 z-70 bg-background p-3 lg:static lg:z-auto lg:bg-transparent lg:p-0',
          )}
        >
          {/* Header (ADR 0133's amendment): live depth as the hero KPI — what
              you care about at the helm is the water under you, not how far
              off the hook you are (Distance moved into the map's own
              metrics panel instead) — with tide context beside it, and the
              low-water clearance warning (ADR 0135, PR #39) folded in below
              rather than living as its own strip above this one. Kept
              compact on purpose: the map is the focus.

              Not gated on isAnchored, matching the low-water clearance
              line's own prior behaviour (and anchor-watch-tile.tsx's
              equivalent): depth and the water under the keel matter while
              deciding where to drop, not only once the hook is down. Only
              the Adjust text button below is anchor-gated — there is
              nothing to adjust with no anchor set. Hidden on phones during
              Adjust (see the wrapper's own comment above); still shown at
              lg and up. */}
          <div
            data-testid="anchor-watch-header"
            className={cn('rounded-md border bg-background/60 px-3 py-3', adjustActive && 'hidden lg:block')}
          >
            <div className="flex flex-wrap items-start gap-4">
                <div className={cn('min-w-0', depthStale && 'grayscale')}>
                  <p className="flex items-center gap-1 text-[10px] uppercase tracking-[0.16em] text-muted-foreground">
                    Depth
                    {depthStale && (
                      <span
                        data-testid="anchor-watch-depth-stale-badge"
                        title={depthStaleLabel ? `No update for ${depthStaleLabel}` : 'Source has stopped updating'}
                        className="shrink-0 rounded-xs border border-amber-500/40 bg-amber-500/10 px-1.5 py-0.5 text-[10px] leading-none text-amber-600 dark:text-amber-400"
                      >
                        Stale{depthStaleLabel ? ` ${depthStaleLabel}` : ''}
                      </span>
                    )}
                  </p>
                  <p className="font-display text-4xl leading-none tabular-nums text-gauge-primary">
                    {depthMeters !== null ? depthValueDisplay(depthMeters, isImperial) : '—'}
                    <span className="ml-2 align-baseline text-lg text-muted-foreground">
                      {depthMeters !== null ? (isImperial ? 'ft' : 'm') : 'unavailable'}
                    </span>
                  </p>
                </div>

                <div className="min-w-0 flex-1">
                  {tide !== null && nextTurn !== null ? (
                    <>
                      <p className="truncate text-[10px] uppercase tracking-[0.16em] text-muted-foreground">
                        Tide{tide.station_name ? ` · ${tide.station_name.toUpperCase()}` : ''}
                      </p>
                      <p className="mt-1 inline-flex items-center gap-1 text-sm font-semibold text-gauge-secondary">
                        {tideIsRising ? <ArrowUp className="h-3.5 w-3.5" /> : <ArrowDown className="h-3.5 w-3.5" />}
                        {tide.tide_direction}
                      </p>
                      <p className="mt-1 truncate text-xs text-foreground">
                        {nextTurn.isHigh ? 'High' : 'Low'}
                        {/* nextTurn only ever holds an extreme tideExtremesByTime
                            already found usable (lib/tide-estimate.ts drops the
                            -1-sentinel/no-time case before this reads it), so its
                            height is always real here - including a genuine
                            negative low, which must still show its figure rather
                            than being read as "no data" the way the -1 sentinel
                            once was. */}
                        <span className="text-muted-foreground"> {tideValueDisplay(nextTurn.heightFt, isImperial)} {isImperial ? 'ft' : 'm'}</span>
                        {' · '}
                        {new Date(nextTurn.time).toLocaleTimeString('en-US', { hour: 'numeric', minute: '2-digit' })}
                      </p>
                      {estimatedDepthAtNextTurn !== null && (
                        <p className="truncate text-xs text-foreground">
                          <span className="text-muted-foreground">{tideIsRising ? 'Est. high' : 'Est. low'}</span>{' '}
                          <span className="font-semibold text-gauge-secondary">
                            {depthValueDisplay(estimatedDepthAtNextTurn, isImperial)} {isImperial ? 'ft' : 'm'}
                          </span>
                        </p>
                      )}
                    </>
                  ) : (
                    <p className="text-[11px] text-muted-foreground">No tide station</p>
                  )}
                </div>

                {showAdjustTextButton && (
                  <Button ref={headerAdjustButtonRef} type="button" variant="outline" onClick={openAdjust} className="shrink-0">
                    Adjust
                  </Button>
                )}
            </div>

            {/* Anchor Watch's low-water clearance warning (ADR 0135): the
                depth at the boat's current position, projected to the next
                low tide, against the operator's configured margin. A
                display warning, not an alarm rule — moved in from its own
                strip above the map (PR #39). */}
            {lowWaterClearance.status === 'too_shallow' && (
              <div
                data-testid="low-water-clearance"
                className="mt-2 truncate rounded-md border border-red-500/40 bg-red-500/10 px-2 py-1 text-xs text-red-600 dark:text-red-400"
              >
                Too shallow at low water · {underKeelPhrase(lowWaterClearance.clearanceM)} at{' '}
                {new Date(lowWaterClearance.lowTideTime).toLocaleTimeString('en-US', { hour: 'numeric', minute: '2-digit' })}
              </div>
            )}
            {lowWaterClearance.status === 'ok' && (
              <p data-testid="low-water-clearance" className="mt-2 truncate text-[11px] text-muted-foreground">
                {underKeelPhrase(lowWaterClearance.clearanceM)} at low water ·{' '}
                {new Date(lowWaterClearance.lowTideTime).toLocaleTimeString('en-US', { hour: 'numeric', minute: '2-digit' })}
              </p>
            )}
            {lowWaterClearance.status === 'unknown' && (
              <p data-testid="low-water-clearance" className="mt-2 truncate text-[11px] text-muted-foreground">
                {lowWaterClearanceReasonLabel(lowWaterClearance.reason)}
              </p>
            )}
          </div>
          {/* Skipped outright in the no-WebGL2 Adjust path: there's no map to
              show, and the header's own text button already opens the bar
              only (the plan's own "Moving the anchor needs the map" state) —
              rendering AnchorWatchMap's WebGL2 fallback message underneath
              the bar's identical notice would just repeat it. */}
          {!(adjustActive && !canUseMapForAdjust) && (
          <div className="min-h-0 flex-1 rounded-xl border bg-background/70">
            {vesselLat !== null && vesselLon !== null ? (
              <AnchorWatchMap
                ref={mapHandleRef}
                vesselLat={vesselLat}
                vesselLon={vesselLon}
                vesselHeadingDeg={vesselHeadingDeg}
                anchorLat={anchorLat}
                anchorLon={anchorLon}
                anchorSetAt={anchorSetAt}
                anchorStateKnown={anchorStateKnown}
                radiusMeters={radiusMeters}
                depthMeters={depthMeters}
                currentDriftKts={currentDriftKts}
                currentSetDeg={currentSetDeg}
                currentDriftImpactKts={currentDriftImpactKts}
                distanceMeters={distanceMeters}
                bearingDeg={bearingDeg}
                scopeRecommendation={scopeRecommendation}
                isImperial={isImperial}
                vesselTrail={vesselTrail}
                aisVessels={aisVessels}
                aisTrails={aisTrails}
                aisCollisionAlarms={aisCollisionAlarms}
                radarTargets={radarTargets}
                radars={radars}
                radarSource={radarSource}
                isDarkTheme={isDarkTheme}
                showImageryLayer={showImageryLayer}
                onImageryToggle={onImageryToggle}
                showRadarEcho={showRadarEcho}
                onRadarEchoToggle={onRadarEchoToggle}
                // The tile trims its own in-map stack to fullscreen+zoom
                // (design critique item 3); this drawer IS the fullscreen
                // view, so it gets satellite/radar/recentre back — every
                // control stays reachable, just relocated.
                expandedControls
                // Namespaces the persisted fitted-zoom key (code-review
                // finding) so this drawer's own fit for its own (much
                // larger) size never clobbers, or gets clobbered by, the
                // dashboard tile's.
                viewKey="drawer"
                placemarks={placemarks}
                onPlacemarkCreate={onPlacemarkCreate}
                onPlacemarkRemove={onPlacemarkRemove}
                className="h-full w-full"
                onAdjust={openAdjust}
                adjustActive={adjustActive}
                adjustRadiusBounds={adjustBounds}
                onAdjustDraftChange={handleAdjustDraftChange}
                onAdjustSetKey={handleAdjustSet}
                onAdjustCancelKey={handleAdjustCancel}
              />
            ) : (
              <div className="flex h-full items-center justify-center text-sm text-muted-foreground">
                No GPS fix
              </div>
            )}
          </div>
          )}

          {/* Adjust mode's bottom bar replaces the Drop/Raise row for the
              length of the session — the two are mutually exclusive (there
              is nothing to drop or raise mid-Adjust). Prominence otherwise
              follows state (see anchor-watch-tile.tsx): Drop is the idle
              state's one action and gets a generous centered target; Raise
              is a confirmed departure chore and sits compact at the trailing
              edge rather than spanning the whole map column. */}
          {adjustActive ? (
            <AnchorAdjustBar
              radiusM={adjustDisplayRadiusM}
              isImperial={isImperial}
              atMin={adjustDisplayRadiusM <= adjustBounds.minM}
              atMax={adjustDisplayRadiusM >= adjustBounds.maxM}
              maxDisabledReason={adjustMaxDisabledReason}
              stepM={adjustStepM}
              onStepRadius={handleStepRadius}
              chips={[rodeLoaChip, plannerSwingChip]}
              onApplyChip={handleApplyChip}
              warningActive={warningActive}
              confirmingSet={confirmingSet}
              committing={adjustCommitting}
              onCancel={handleAdjustCancel}
              onSet={handleAdjustSet}
              noMapNotice={!canUseMapForAdjust}
              aboveMaxOriginalRadiusM={aboveMaxOriginalRadiusM}
              noFixNotice={!hasGpsFix}
            />
          ) : (
            <div className={anchorState === 'none' ? 'flex justify-center' : 'flex justify-end'}>
              <AnchorDropRaiseButton
                className={anchorState === 'none' ? 'w-full max-w-xs' : undefined}
                anchorActive={anchorState !== 'none'}
                canDrop={canDrop}
                onDrop={onDropAnchor}
                onRaise={onClearAnchor}
              />
            </div>
          )}
        </div>

        {/* Hidden entirely during Adjust: on phones the fixed full-screen
            layer above already covers it, and at lg+ it would otherwise sit
            beside a map whose whole point is undivided attention while
            positioning the anchor. */}
        {!adjustActive && (
        <AnchorRodePlanner
          anchorState={anchorState}
          rodeDeployedM={rodeDeployedM}
          seaState={seaState}
          seabedType={seabedType}
          depthM={depthMeters}
          planningDepthM={planningDepthM}
          planningTideHeightFt={planningTideHeightFt}
          onPlanningDepthChange={onPlanningDepthChange}
          windSpeedApparentKts={windSpeedApparentKts}
          maxGustKts={maxGustKts}
          tide={tide}
          isImperial={isImperial}
          anchorConfig={anchorConfig}
          bowOffsetM={bowOffsetM}
          vesselLengthOverallM={vesselLengthOverallM}
          windBandId={windBandId}
          onWindBandChange={onWindBandChange}
          onUpdateRodeAndConditions={onUpdateRodeAndConditions}
          onApplyAlarmRadius={applyRadius}
        />
        )}
      </div>
    </div>
  )
}

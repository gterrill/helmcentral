import type { HullType } from '@/config/app-config'
import type { TideToday } from '@/hooks/use-tide-today'
import { calculateCatenary, type SeabedType, type SeaState } from '@/lib/catenary'
import type { GustWindow } from '@/lib/gust-windows'
import { CHART_DATUM, currentTideHeightKnown, normaliseTideDatum, tideHeightFtKnown, type TideCapture, type TideDatum } from '@/lib/tide-datum'
import { feetToMeters, toDisplayDistance } from '@/lib/units'

/**
 * No rode method may recommend a scope below this. Below roughly 3:1 the rode
 * angle pulls the anchor out of the seabed whatever the load model says, so the
 * floor is applied here, not in calculateCatenary, which stays pure physics.
 */
export const MIN_SCOPE_RATIO = 3

export type ScopeStatus = 'ok' | 'low' | 'insufficient' | 'unknown'

/**
 * A planning depth is only meaningful as a contemporaneous pair: a reading
 * plus the tide height at the instant it was taken (ADR 0063). The tide
 * height is never re-derived from the live tide station later — mixing a
 * reading from one moment with the tide from another is the unsafe bug this
 * type exists to prevent (see maxExpectedDepthM).
 */
export interface PlanningDepthDatum {
  depthM: number
  /** The tide height at the moment this depth was read, or null if no tide
   * reading accompanied it (e.g. no tide station configured yet, or the
   * station hadn't reported). Never the tide right now. */
  tideHeightFt: number | null
  /** What tideHeightFt is measured from (ADR 0273). Left out, it is the datum of
   * the tide it is compared with. Set, it must match that tide's: a rise taken
   * between a height from mean sea level and one from chart datum is no rise. */
  tideDatum?: TideDatum
}

export interface RodePlanInput {
  depth: PlanningDepthDatum | null
  /** Drives depthUnavailableReason's wording when depth is null, and is the
   * precedence rule resolvePlanningDepth already applied to produce `depth`
   * — carried here rather than re-derived so a caller can't disagree with
   * itself between building the datum and explaining its absence. */
  isAnchored: boolean
  bowRollerHeightM: number
  tide: TideToday | null
  windKts: number | null
  seaState: SeaState
  seabedType: SeabedType
  chainSizeMm: number
  chainOnboardM: number
  windageAreaM2: number
  hullType: HullType
}

export interface RodePlan {
  planningDepthM: number
  /** Whether the rise to the next high was actually added. False whenever
   * the datum carried no tide stamp of its own. */
  tideCorrected: boolean
  depthFromHawseM: number
  recommendedRodeM: number
  recommendedScope: number
  /** True when MIN_SCOPE_RATIO overrode the raw catenary recommendation — callers must surface this, not substitute the floored figure silently. */
  scopeFloorApplied: boolean
  exceedsChainOnboard: boolean
  chainRemainingM: number
  /** a = H / w (m), the catenary parameter the recommendation used, for drawing the rode's shape. */
  catenaryParameterM: number
}

/**
 * useTideToday NEVER returns null — it returns defaultTide with -1
 * sentinels for "the station hasn't reported yet", so `tide === null` never
 * fires and the real check is currentTideHeightKnown (lib/tide-datum.ts): at or
 * above zero on chart datum, any real number from mean sea level (ADR 0273).
 * This is the one place that check lives; every caller that needs "the tide
 * right now, or nothing" (the two anchor-drop capture sites and the planner's
 * Depth input, plus this module's own live-datum construction below) shares it
 * rather than re-deriving the sentinel rule.
 */
export function tideHeightFtOrNull(tide: TideToday | null): number | null {
  if (tide === null) return null
  return currentTideHeightKnown(tide) ? tide.current_tide_height_ft : null
}

/**
 * The tide right now as one value to record with a depth: its height and the
 * datum it is measured from, or null when there is no reading. One value, so a
 * datum is never recorded without a height.
 */
export function tideCapture(tide: TideToday | null): TideCapture | null {
  const ft = tideHeightFtOrNull(tide)
  return ft === null || tide === null ? null : { ft, datum: normaliseTideDatum(tide.height_datum) }
}

export interface PlanningDepthInputs {
  isAnchored: boolean
  liveDepthM: number | null
  planningDepthM: number | null
  /** The tide stamped on the recorded planning depth, or null when none was. */
  planningTide: TideCapture | null
  tide: TideToday | null
}

/**
 * The one place depth precedence lives (ADR 0063), shaped deliberately like
 * the existing resolvePlanningWindBand below:
 *
 * 1. Anchor down: the recorded planning depth — seeded from the depth at the
 *    moment the anchor was dropped, and editable from there. Nothing
 *    recorded: null, never a silent fall back to live depth — that
 *    substitution is what made a good set read "Scope Insufficient" because
 *    the boat swung over a hole.
 * 2. No anchor down: the live sounder, tide-stamped from right now — you are
 *    deciding what to pay out, so "now" is the correct reading.
 */
export function resolvePlanningDepth(inputs: PlanningDepthInputs): PlanningDepthDatum | null {
  if (inputs.isAnchored) {
    if (inputs.planningDepthM === null) return null
    return { depthM: inputs.planningDepthM, tideHeightFt: inputs.planningTide?.ft ?? null, tideDatum: inputs.planningTide?.datum ?? CHART_DATUM }
  }
  if (inputs.liveDepthM === null) return null
  return { depthM: inputs.liveDepthM, tideHeightFt: tideHeightFtOrNull(inputs.tide) }
}

/**
 * True when the depth was saved against a tide from one datum and the tide now
 * on screen is from another. No rise can be worked out then, and that is a
 * different thing from having no tide station.
 */
export function planningTideDatumDiffers(datum: PlanningDepthDatum | null, tide: TideToday | null): boolean {
  if (datum === null || tide === null || datum.tideHeightFt === null || datum.tideDatum === undefined) return false
  return datum.tideDatum !== normaliseTideDatum(tide.height_datum)
}

/**
 * Datum depth plus the rise to the next high, reusing the arithmetic already
 * proven in depth-tide-tile.tsx. A falling tide (or one already past the next
 * high) never *reduces* the planning depth — that would be a silent unsafe
 * substitution — so the rise is clamped to zero rather than allowed negative.
 *
 * The rise is measured from `datum.tideHeightFt` — the tide at the moment the
 * depth was read — never from `tide.current_tide_height_ft`. Mixing a depth
 * reading from one moment with the tide reading from another is wrong in the
 * unsafe direction (too little chain) whenever the tide has risen since
 * (ADR 0063); this is the one arithmetic site that guards against it.
 *
 * Returns null when the datum, its own tide stamp, or the high-tide forecast
 * is unavailable. Callers must fall back to the raw datum depth explicitly
 * and visibly — no silent substitution here.
 */
export function maxExpectedDepthM(datum: PlanningDepthDatum | null, tide: TideToday | null): number | null {
  if (datum === null || tide === null) return null
  const tideDatum = normaliseTideDatum(tide.height_datum)
  if (datum.tideHeightFt === null || !tideHeightFtKnown(datum.tideHeightFt, tideDatum)) return null
  if (datum.tideDatum !== undefined && datum.tideDatum !== tideDatum) return null
  if (!tideHeightFtKnown(tide.high_tide_height_ft, tideDatum)) return null

  const riseFt = tide.high_tide_height_ft - datum.tideHeightFt
  const riseM = feetToMeters(Math.max(0, riseFt))
  return datum.depthM + riseM
}

/**
 * Composes a datum with the tide forecast into the figure the rest of the
 * module plans against, plus whether that figure actually got corrected.
 * Extracted so buildRodePlan and ratioMethod share one arithmetic site
 * instead of each re-deriving it (they used to diverge here) — "both sites
 * must change" becomes "both sites already share".
 */
function planningDepthWithTide(datum: PlanningDepthDatum, tide: TideToday | null): { planningDepthM: number; tideCorrected: boolean } {
  const tideDepthM = maxExpectedDepthM(datum, tide)
  return tideDepthM !== null
    ? { planningDepthM: tideDepthM, tideCorrected: true }
    : { planningDepthM: datum.depthM, tideCorrected: false }
}

/**
 * The figure the Depth field displays and lets the operator type over (ADR
 * 0063's amendment): depth at the current location at the next high tide,
 * plus bow roller height — depth-from-hawse at high water. This is *not*
 * what the rest of the module plans against internally (planningDepthWithTide
 * omits the bow height, and buildRodePlan adds it separately as
 * depthFromHawseM) — it exists purely as the operator-facing figure, with
 * rawDepthFromPlanningFigureM below as its exact inverse so typing over it
 * round-trips without compounding the tide correction.
 *
 * null only when the datum itself is null (nothing recorded/no live
 * reading) — the same "fail visibly, no fallback" rule every other
 * datum-shaped function in this module follows.
 */
export function planningFigureM(datum: PlanningDepthDatum | null, tide: TideToday | null, bowRollerHeightM: number): number | null {
  if (datum === null) return null
  return (maxExpectedDepthM(datum, tide) ?? datum.depthM) + bowRollerHeightM
}

/**
 * The exact inverse of planningFigureM, used to turn a typed Depth-field
 * figure back into the raw depth that gets persisted (ADR 0063's pair rule:
 * a depth is stored with the tide height at the moment it was read). Seeding
 * the input with the corrected figure and then storing the typed value as
 * raw would feed the rise back in and compound it on every render — this
 * function is the fix: it subtracts bow height and the same rise
 * planningFigureM would have added, so a persisted datum stamped with
 * `tideNowFt` reads back through planningFigureM to the figure the operator
 * typed.
 *
 * The rise is computed by asking maxExpectedDepthM for the rise alone: a
 * zero-depth datum stamped with `tideNowFt` (the tide at the moment of
 * typing, i.e. what the persisted datum will itself carry) returns exactly
 * the rise, or null under precisely the conditions planningFigureM's own
 * call falls back to zero rise — reusing that function rather than
 * re-deriving the null ladder keeps the two in lockstep by construction.
 *
 * Returns null when the typed figure doesn't clear bow height plus the tide
 * rise — a raw depth of zero or less isn't a sensible reading, and this is
 * the fail-visibly signal the caller uses to refuse the persist rather than
 * storing a nonsensical number.
 */
export function rawDepthFromPlanningFigureM(
  figureM: number,
  tideNowFt: number | null,
  tide: TideToday | null,
  bowRollerHeightM: number,
): number | null {
  const riseM = maxExpectedDepthM({ depthM: 0, tideHeightFt: tideNowFt }, tide) ?? 0
  const rawDepthM = figureM - bowRollerHeightM - riseM
  return rawDepthM > 0 ? rawDepthM : null
}

/** The division inlined in the old tile: current rode over depth-from-hawse. */
export function scopeRatio(rodeM: number, depthFromHawseM: number): number | null {
  if (depthFromHawseM <= 0) return null
  return rodeM / depthFromHawseM
}

/**
 * The ok / low (>=85%) / insufficient / unknown ladder, lifted verbatim from
 * the old tile. "unknown" — not a fail — whenever either scope is unavailable,
 * so a missing input can never render as the red "insufficient" state. The
 * planner's badge goes through deployedScopeBadge, which checks the shown Pay
 * Out figure before this ladder.
 */
export function scopeStatus(currentScope: number | null, recommendedScope: number | null): ScopeStatus {
  if (currentScope === null || recommendedScope === null) return 'unknown'
  if (currentScope >= recommendedScope) return 'ok'
  if (currentScope >= recommendedScope * 0.85) return 'low'
  return 'insufficient'
}

/**
 * The Pay Out figure as the planner prints it: the rode in the operator's unit,
 * rounded to a whole number. The headline and the badge both read it, so they
 * can't disagree.
 */
export function payOutDisplayValue(rodeM: number, isImperial: boolean): number {
  return Math.round(toDisplayDistance(rodeM, isImperial))
}

/**
 * The scope badge for a deployed rode against one method's figure: null with no
 * rode out or no figure to judge against; otherwise "ok" when the rode reaches
 * the Pay Out figure as shown, else how short it is. The gap is in display
 * units rather than metres, so a figure typed in feet and stored as metres
 * can't come back a hair short; the 1e-6 absorbs the float noise of that round
 * trip. A paid-out rode of 55 against a shown 55 is ok even when the raw
 * recommendation was 55.3. Short goes through the scopeStatus ladder, and the
 * shortfall rounds up so paying out that many clears the badge.
 */
export type DeployedScopeBadge = { status: 'ok' } | { status: 'low' | 'insufficient'; shortfall: number }

export function deployedScopeBadge(
  rodeDeployedM: number,
  result: RodeMethodResult | null,
  isImperial: boolean,
): DeployedScopeBadge | null {
  if (rodeDeployedM <= 0 || result === null || result.unavailableReason || result.depthFromHawseM === undefined) return null
  const gap = payOutDisplayValue(result.recommendedRodeM, isImperial) - toDisplayDistance(rodeDeployedM, isImperial)
  if (gap <= 1e-6) return { status: 'ok' }
  const status = scopeStatus(scopeRatio(rodeDeployedM, result.depthFromHawseM), result.scopeRatio)
  if (status === 'low' || status === 'insufficient') return { status, shortfall: Math.ceil(gap - 1e-6) }
  // Short of the shown figure yet at or above the recommended scope: a hair short.
  return { status: 'low', shortfall: 1 }
}

/**
 * Composes tide-corrected depth with the catenary calculation. Returns null
 * whenever the recommendation itself can't be computed (missing depth/wind,
 * or calculateCatenary rejecting an unconfigured input) — callers render an
 * explicit "why" rather than a dash for that case (see RodeMethod below).
 */
export function buildRodePlan(input: RodePlanInput): RodePlan | null {
  if (input.depth === null || input.windKts === null) return null

  const { planningDepthM, tideCorrected } = planningDepthWithTide(input.depth, input.tide)

  const catenary = calculateCatenary({
    depthM: planningDepthM,
    bowRollerHeightM: input.bowRollerHeightM,
    windKts: input.windKts,
    seaState: input.seaState,
    seabedType: input.seabedType,
    chainSizeMm: input.chainSizeMm,
    windageAreaM2: input.windageAreaM2,
    hullType: input.hullType,
  })
  if (catenary === null) return null

  const depthFromHawseM = planningDepthM + input.bowRollerHeightM

  const flooredRodeM = MIN_SCOPE_RATIO * depthFromHawseM
  const scopeFloorApplied = catenary.recommendedRodeM < flooredRodeM
  const recommendedRodeM = Math.max(catenary.recommendedRodeM, flooredRodeM)
  const recommendedScope = Math.max(catenary.minScopeRatio, MIN_SCOPE_RATIO)

  const exceedsChainOnboard = recommendedRodeM > input.chainOnboardM
  const chainRemainingM = input.chainOnboardM - recommendedRodeM

  return {
    planningDepthM,
    tideCorrected,
    depthFromHawseM,
    recommendedRodeM,
    recommendedScope,
    scopeFloorApplied,
    exceedsChainOnboard,
    chainRemainingM,
    catenaryParameterM: catenary.catenaryParameterM,
  }
}

/**
 * Extension point (ADR 0047 §2a). `catenaryMethod` and `ratioMethod` below
 * are the two `RodeMethod` implementations; `rodeMethods` is the array the
 * planner component maps over into one `SidebarGroup` per result.
 */
export interface RodeMethodResult {
  id: 'catenary' | 'ratio'
  label: string
  recommendedRodeM: number
  scopeRatio: number
  /** True when the minimum scope ratio set the figure, not the method's own calculation. Absent when unavailable. */
  scopeFloorApplied?: boolean
  /** a = H / w (m) from the plan's catenary model, the load every rode is drawn with; null when there is no plan. */
  catenaryParameterM: number | null
  /** What to tell the operator when this method's rode is too short for that load; null for a method that can't be. */
  liftWarning: string | null
  /** Bow roller to seabed (m) at the planning depth the method worked from. Absent when unavailable. */
  depthFromHawseM?: number
  /** Whether the rise to the next high was part of that planning depth. Absent when unavailable. */
  tideCorrected?: boolean
  /** Set when the method cannot answer — the caption names *why*, never a bare dash. */
  unavailableReason?: string
}

/** The caller builds the plan once and shares it, so every method sees the same load. */
export type RodeMethod = (input: RodePlanInput, plan?: RodePlan | null) => RodeMethodResult | null

/**
 * Shared by both methods' unavailable-depth branch: anchored with nothing
 * recorded is a different, more actionable reason than simply having no
 * reading at all — the planner prints the reason in the value slot, so this
 * needs no new rendering, only the right words.
 */
function depthUnavailableReason(isAnchored: boolean): string {
  return isAnchored ? 'no depth entered' : 'no depth reading'
}

function catenaryUnavailableReason(input: RodePlanInput): string {
  if (input.depth === null) return depthUnavailableReason(input.isAnchored)
  if (input.windKts === null) return 'no wind data'
  if (input.bowRollerHeightM <= 0) return 'bow roller height not configured'
  if (input.chainSizeMm <= 0) return 'chain size not configured'
  if (input.windageAreaM2 <= 0) return 'windage area not configured'
  return 'catenary calculation unavailable'
}

export const catenaryMethod: RodeMethod = (input, plan = buildRodePlan(input)) => {

  if (plan === null) {
    return {
      id: 'catenary',
      label: 'Catenary',
      recommendedRodeM: 0,
      scopeRatio: 0,
      catenaryParameterM: null,
      liftWarning: null,
      unavailableReason: catenaryUnavailableReason(input),
    }
  }

  return {
    id: 'catenary',
    label: 'Catenary',
    recommendedRodeM: plan.recommendedRodeM,
    scopeRatio: plan.recommendedScope,
    scopeFloorApplied: plan.scopeFloorApplied,
    depthFromHawseM: plan.depthFromHawseM,
    tideCorrected: plan.tideCorrected,
    catenaryParameterM: plan.catenaryParameterM,
    // Its rode is at least the suspended length that just touches the anchor, so it can't lift.
    liftWarning: null,
  }
}

const RATIO_WIND_THRESHOLD_KTS = 20
const RATIO_HIGH = 7
// MIN_SCOPE_RATIO (3) holds here by construction, not by a runtime clamp:
// RATIO_NORMAL is the smallest ratio this method ever returns, and 5 > 3.
// A clamp that can never bind would be dead code. If you lower RATIO_NORMAL
// toward or below MIN_SCOPE_RATIO, this method needs the same floor buildRodePlan applies.
const RATIO_NORMAL = 5

function ratioUnavailableReason(input: RodePlanInput): string {
  if (input.depth === null) return depthUnavailableReason(input.isAnchored)
  if (input.windKts === null) return 'no wind data'
  if (input.bowRollerHeightM <= 0) return 'bow roller height not configured'
  return 'ratio calculation unavailable'
}

/**
 * The user's ratio rule (ADR 0047 §2a): pay out (planning depth + bow roller
 * height) x 7 in a blow, x 5 otherwise. Reads only depth, tide, wind, and bow
 * roller height from RodePlanInput — chain size, windage area, sea state,
 * seabed type, and hull type never factor in, unlike catenaryMethod.
 */
export const ratioMethod: RodeMethod = (input, plan = buildRodePlan(input)) => {
  if (input.depth === null || input.windKts === null || input.bowRollerHeightM <= 0) {
    return {
      id: 'ratio',
      label: 'Ratio',
      recommendedRodeM: 0,
      scopeRatio: 0,
      catenaryParameterM: null,
      liftWarning: null,
      unavailableReason: ratioUnavailableReason(input),
    }
  }

  const { planningDepthM, tideCorrected } = planningDepthWithTide(input.depth, input.tide)

  const ratio = input.windKts >= RATIO_WIND_THRESHOLD_KTS ? RATIO_HIGH : RATIO_NORMAL
  const depthFromHawseM = planningDepthM + input.bowRollerHeightM
  const recommendedRodeM = depthFromHawseM * ratio

  return {
    id: 'ratio',
    label: 'Ratio',
    recommendedRodeM,
    scopeRatio: ratio,
    depthFromHawseM,
    tideCorrected,
    // The load is the plan's, whatever the ratio rule says: only the rode differs.
    catenaryParameterM: plan?.catenaryParameterM ?? null,
    liftWarning: 'The ratio rule is too short for the planned conditions: the anchor would be lifted off the bottom.',
  }
}

/** The array the planner component maps over, one SidebarGroup per result (ADR 0047 §2a). */
export const rodeMethods: RodeMethod[] = [catenaryMethod, ratioMethod]

/**
 * LOA precedence (ADR 0047), extracted from anchor-rode-planner.tsx so the
 * Adjust mode's radius ceiling (lib/anchor-adjust.ts's alarmRadiusBounds) and
 * the planner's own swing circle resolve the operator's boat length exactly
 * the same way: an explicit `settings.anchor.loa_m` override always wins when
 * set (> 0); otherwise SignalK's `design.length.overall`, which — unlike
 * `gps_from_bow_m` — carries no per-sensor offset trap, so it's safe to adopt
 * directly. Neither present: null, not a silent zero — a caller computing a
 * swing circle or a radius ceiling from a zero LOA would report a circle
 * smaller than the boat.
 */
export function resolveLoaM(settingsLoaM: number, vesselLengthOverallM: number | null): number | null {
  if (settingsLoaM > 0) return settingsLoaM
  if (vesselLengthOverallM !== null && vesselLengthOverallM > 0) return vesselLengthOverallM
  return null
}

/**
 * The swing circle a method's own recommended rode implies: rode + bow
 * offset + the hull's own length. Callers pass the effective bow offset, with
 * any doubling for a mark that sits at the GPS already applied. `loaM` null (unresolved — see resolveLoaM
 * above) returns null rather than silently computing a circle smaller than
 * the boat.
 */
export function computeSwingRadiusM(recommendedRodeM: number, bowOffsetM: number, loaM: number | null): number | null {
  return loaM !== null ? recommendedRodeM + bowOffsetM + loaM : null
}

/**
 * Planning-wind seed rule (ADR 0047): prefer the 1h max gust — plan for the
 * forecast gust, not the breeze at the moment the hook goes down — falling
 * back to apparent wind, then to no seed. Extracted from
 * anchor-rode-planner.tsx so the rule exists in exactly one place.
 */
export function seedPlanningWindKts(
  maxGustKts: Record<GustWindow, number | null>,
  apparentKts: number | null,
): { windKts: number | null; source: 'gust' | 'apparent' | null } {
  const gust1h = maxGustKts['1h']
  if (gust1h !== null) return { windKts: gust1h, source: 'gust' }
  if (apparentKts !== null) return { windKts: apparentKts, source: 'apparent' }
  return { windKts: null, source: null }
}

export interface WindBand {
  id: string
  label: string
  minKts: number
  /** null on the open-ended top band. */
  maxKts: number | null
  /**
   * The figure the plan actually uses for this band: the top of it. A forecast
   * of "15 to 20" means rigging for 20, and the load term is v², so planning at
   * the middle of a band would under-rode most of the range it covers. The open
   * band has no top, so it plans at its floor — the only honest number
   * available — and says so by being labelled 50+.
   */
  planKts: number
}

/**
 * Forecast wind bands offered by the rode planner. You do not know the wind to
 * a tenth of a knot hours ahead; you know roughly which band tonight sits in,
 * which is what a forecast gives you and all this calculation needs.
 */
export const WIND_BANDS: WindBand[] = [
  { id: '0-10', label: '0 – 10 kts', minKts: 0, maxKts: 10, planKts: 10 },
  { id: '10-15', label: '10 – 15 kts', minKts: 10, maxKts: 15, planKts: 15 },
  { id: '15-20', label: '15 – 20 kts', minKts: 15, maxKts: 20, planKts: 20 },
  { id: '20-25', label: '20 – 25 kts', minKts: 20, maxKts: 25, planKts: 25 },
  { id: '25-30', label: '25 – 30 kts', minKts: 25, maxKts: 30, planKts: 30 },
  { id: '30-40', label: '30 – 40 kts', minKts: 30, maxKts: 40, planKts: 40 },
  { id: '40-50', label: '40 – 50 kts', minKts: 40, maxKts: 50, planKts: 50 },
  { id: '50+', label: '50+ kts', minKts: 50, maxKts: null, planKts: 50 },
]

/**
 * The band a live reading falls in, used to preselect the planner's dropdown
 * from the 1h gust. Bands are lower-inclusive, so a reading sitting exactly on
 * a boundary belongs to the stronger band rather than the calmer one — the same
 * convention ratioMethod's `>= 20` threshold already follows.
 *
 * Returns null with no reading rather than guessing the calmest band, which
 * would silently drive a real recommendation from an absent input.
 */
export function windBandForKts(kts: number | null): WindBand | null {
  if (kts === null || !Number.isFinite(kts)) return null
  return (
    WIND_BANDS.find((band) => kts >= band.minKts && (band.maxKts === null || kts < band.maxKts))
    // Below the first band's floor only a nonsensical negative can land, so it
    // clamps to the calmest band rather than reporting "no wind data".
    ?? WIND_BANDS[0]
  )
}

/**
 * The single band-selection rule behind the Rode Planner's dropdown. An explicit `selectedBandId` — the operator's own choice —
 * always wins, even one that doesn't name a real band: that falls back to
 * the live-seeded band rather than throwing or silently landing on the
 * calmest one. With no selection at all, the live seed (seedPlanningWindKts
 * -> windBandForKts) drives it; with neither a selection nor a live seed,
 * there's nothing to plan against and this returns null.
 */
export function resolvePlanningWindBand(
  maxGustKts: Record<GustWindow, number | null>,
  apparentKts: number | null,
  selectedBandId: string | null,
): WindBand | null {
  const { windKts: seedKts } = seedPlanningWindKts(maxGustKts, apparentKts)
  const seededBand = windBandForKts(seedKts)
  if (selectedBandId === null) return seededBand
  return WIND_BANDS.find((band) => band.id === selectedBandId) ?? seededBand
}

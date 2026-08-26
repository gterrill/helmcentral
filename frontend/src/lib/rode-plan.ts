import type { AnchorConfig, HullType } from '@/config/app-config'
import type { TideToday } from '@/hooks/use-tide-today'
import { calculateCatenary, type SeabedType, type SeaState } from '@/lib/catenary'
import type { GustWindow } from '@/lib/gust-windows'

const METERS_PER_FOOT = 3.28084

/**
 * No rode method may ever recommend a scope below this, regardless of what
 * its own math says. calculateCatenary has no floor built in — fed a calm
 * night at 3kt wind it recommended 0.6:1, mathematically correct for the
 * modelled load but nautically indefensible: below roughly 3:1 the rode
 * angle pulls the anchor upward out of the seabed regardless of how little
 * horizontal load the catenary math says it's holding. The floor is a
 * composition-layer policy (ADR 0047's "composition" over catenary.ts's
 * "physics"), applied here rather than in calculateCatenary, so the physics
 * function stays pinned and policy-free and its characterization tests keep
 * asserting the raw, unfloored numbers.
 */
export const MIN_SCOPE_RATIO = 3

export type ScopeStatus = 'ok' | 'low' | 'insufficient' | 'unknown'

export interface RodePlanInput {
  sounderDepthM: number | null
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
  depthSource: 'tide' | 'sounder'
  depthFromHawseM: number
  recommendedRodeM: number
  recommendedScope: number
  /** True when MIN_SCOPE_RATIO overrode the raw catenary recommendation — callers must surface this, not substitute the floored figure silently. */
  scopeFloorApplied: boolean
  exceedsChainOnboard: boolean
  chainRemainingM: number
}

/**
 * Sounder depth plus the rise to the next high, reusing the arithmetic already
 * proven in depth-tide-tile.tsx. A falling tide (or one already past the next
 * high) never *reduces* the planning depth — that would be a silent unsafe
 * substitution — so the rise is clamped to zero rather than allowed negative.
 *
 * Returns null when depth or tide data is unavailable, or when the tide
 * station hasn't reported a reading (the -1 sentinel used throughout the tide
 * hook). Callers must fall back to the raw sounder depth explicitly and
 * visibly — no silent substitution here.
 */
export function maxExpectedDepthM(sounderDepthM: number | null, tide: TideToday | null): number | null {
  if (sounderDepthM === null || tide === null) return null
  if (tide.current_tide_height_ft < 0 || tide.high_tide_height_ft < 0) return null

  const riseFt = tide.high_tide_height_ft - tide.current_tide_height_ft
  const riseM = Math.max(0, riseFt) / METERS_PER_FOOT
  return sounderDepthM + riseM
}

/** The division inlined in the old tile: current rode over depth-from-hawse. */
export function scopeRatio(rodeM: number, depthFromHawseM: number): number | null {
  if (depthFromHawseM <= 0) return null
  return rodeM / depthFromHawseM
}

/**
 * The ok / low (>=85%) / insufficient / unknown ladder, lifted verbatim from
 * the old tile. "unknown" — not a fail — whenever either scope is unavailable,
 * so a missing input can never render as the red "insufficient" state.
 */
export function scopeStatus(currentScope: number | null, recommendedScope: number | null): ScopeStatus {
  if (currentScope === null || recommendedScope === null) return 'unknown'
  if (currentScope >= recommendedScope) return 'ok'
  if (currentScope >= recommendedScope * 0.85) return 'low'
  return 'insufficient'
}

/**
 * Composes tide-corrected depth with the catenary calculation. Returns null
 * whenever the recommendation itself can't be computed (missing depth/wind,
 * or calculateCatenary rejecting an unconfigured input) — callers render an
 * explicit "why" rather than a dash for that case (see RodeMethod below).
 */
export function buildRodePlan(input: RodePlanInput): RodePlan | null {
  if (input.sounderDepthM === null || input.windKts === null) return null

  const tideDepthM = maxExpectedDepthM(input.sounderDepthM, input.tide)
  const planningDepthM = tideDepthM ?? input.sounderDepthM
  const depthSource: 'tide' | 'sounder' = tideDepthM !== null ? 'tide' : 'sounder'

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
    depthSource,
    depthFromHawseM,
    recommendedRodeM,
    recommendedScope,
    scopeFloorApplied,
    exceedsChainOnboard,
    chainRemainingM,
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
  note: string
  /** Set when the method cannot answer — the caption names *why*, never a bare dash. */
  unavailableReason?: string
}

export type RodeMethod = (input: RodePlanInput) => RodeMethodResult | null

/**
 * The depth + bow-height clause shared by both methods' notes. Scope is
 * computed against depth-from-hawse (depth + bow roller height), not depth
 * alone, so the caption has to name both terms of that sum rather than just
 * the depth half of it.
 */
function depthAndBowHeightNote(planningDepthM: number, depthSource: 'tide' | 'sounder', bowRollerHeightM: number): string {
  const depthLabel = depthSource === 'tide' ? '(tide-corrected)' : '(sounder only)'
  return `Depth ${planningDepthM.toFixed(1)} m ${depthLabel} + ${bowRollerHeightM.toFixed(1)} m bow height`
}

function catenaryUnavailableReason(input: RodePlanInput): string {
  if (input.sounderDepthM === null) return 'no depth reading'
  if (input.windKts === null) return 'no wind data'
  if (input.bowRollerHeightM <= 0) return 'bow roller height not configured'
  if (input.chainSizeMm <= 0) return 'chain size not configured'
  if (input.windageAreaM2 <= 0) return 'windage area not configured'
  return 'catenary calculation unavailable'
}

export const catenaryMethod: RodeMethod = (input) => {
  const plan = buildRodePlan(input)

  if (plan === null) {
    return {
      id: 'catenary',
      label: 'Catenary Method',
      recommendedRodeM: 0,
      scopeRatio: 0,
      note: '',
      unavailableReason: catenaryUnavailableReason(input),
    }
  }

  const depthNote = depthAndBowHeightNote(plan.planningDepthM, plan.depthSource, input.bowRollerHeightM)
  const floorNote = plan.scopeFloorApplied ? ` · ${MIN_SCOPE_RATIO}:1 minimum` : ''

  return {
    id: 'catenary',
    label: 'Catenary Method',
    recommendedRodeM: plan.recommendedRodeM,
    scopeRatio: plan.recommendedScope,
    note: `${depthNote} · Wind ${Math.round(input.windKts ?? 0)} kts · Chain ${input.chainSizeMm}mm · Hull ${input.hullType.replace('_', ' ')}${floorNote}`,
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
  if (input.sounderDepthM === null) return 'no depth reading'
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
export const ratioMethod: RodeMethod = (input) => {
  if (input.sounderDepthM === null || input.windKts === null || input.bowRollerHeightM <= 0) {
    return {
      id: 'ratio',
      label: 'Ratio Method',
      recommendedRodeM: 0,
      scopeRatio: 0,
      note: '',
      unavailableReason: ratioUnavailableReason(input),
    }
  }

  const tideDepthM = maxExpectedDepthM(input.sounderDepthM, input.tide)
  const planningDepthM = tideDepthM ?? input.sounderDepthM
  const depthSource: 'tide' | 'sounder' = tideDepthM !== null ? 'tide' : 'sounder'

  const ratio = input.windKts >= RATIO_WIND_THRESHOLD_KTS ? RATIO_HIGH : RATIO_NORMAL
  const recommendedRodeM = (planningDepthM + input.bowRollerHeightM) * ratio

  const depthNote = depthAndBowHeightNote(planningDepthM, depthSource, input.bowRollerHeightM)
  const ratioNote = ratio === RATIO_HIGH ? `${ratio}:1 (wind ≥ ${RATIO_WIND_THRESHOLD_KTS} kts)` : `${ratio}:1`

  return {
    id: 'ratio',
    label: 'Ratio Method',
    recommendedRodeM,
    scopeRatio: ratio,
    note: `${depthNote} · Wind ${Math.round(input.windKts)} kts · ${ratioNote}`,
  }
}

/** The array the planner component maps over, one SidebarGroup per result (ADR 0047 §2a). */
export const rodeMethods: RodeMethod[] = [catenaryMethod, ratioMethod]

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
 * The single band-selection rule shared by the Rode Planner's dropdown and
 * computeScopeRecommendation (below), so an operator's pick in one view
 * reaches the other rather than each deriving its own band and quietly
 * disagreeing. An explicit `selectedBandId` — the operator's own choice —
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

/**
 * The single recommendation a host renders (map overlay's Scope row, née the
 * tile's below-map readout — ADR 0059 §3): resolves the shared wind band the
 * same way the Rode Planner does (resolvePlanningWindBand), builds the
 * RodePlanInput from the band's planKts, and picks catenaryMethod vs
 * ratioMethod from anchorConfig.scopeMethod. Extracted so the tile and drawer
 * hosts, which both already hold every one of these inputs, don't duplicate
 * this composition.
 *
 * selectedWindBandId is required, not defaulted: a silent default here is
 * exactly how the tile's raw-seed plan and the planner's banded plan drifted
 * apart before this function existed — every caller must say explicitly
 * which band (if any) the operator has chosen.
 */
export function computeScopeRecommendation(args: {
  depthMeters: number | null
  tide: TideToday | null
  maxGustKts: Record<GustWindow, number | null>
  windSpeedApparentKts: number | null
  seaState: SeaState
  seabedType: SeabedType
  anchorConfig: AnchorConfig
  selectedWindBandId: string | null
}): RodeMethodResult {
  const windKts = resolvePlanningWindBand(args.maxGustKts, args.windSpeedApparentKts, args.selectedWindBandId)?.planKts ?? null
  const planInput: RodePlanInput = {
    sounderDepthM: args.depthMeters,
    bowRollerHeightM: args.anchorConfig.bowRollerHeightM,
    tide: args.tide,
    windKts,
    seaState: args.seaState,
    seabedType: args.seabedType,
    chainSizeMm: args.anchorConfig.chainSizeMm,
    chainOnboardM: args.anchorConfig.chainOnboardM,
    windageAreaM2: args.anchorConfig.windageAreaM2,
    hullType: args.anchorConfig.hullType,
  }
  const method = args.anchorConfig.scopeMethod === 'catenary' ? catenaryMethod : ratioMethod
  // catenaryMethod/ratioMethod never actually return null — both always
  // answer with either a figure or an unavailableReason (see their bodies
  // above) — but RodeMethod's signature stays broader for a future method
  // that might genuinely have nothing to say. The assertion documents that
  // guarantee rather than widening this function's return type to match.
  return method(planInput)!
}

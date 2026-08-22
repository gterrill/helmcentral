import type { HullType } from '@/config/app-config'
import type { TideToday } from '@/hooks/use-tide-today'
import { calculateCatenary, type SeabedType, type SeaState } from '@/lib/catenary'

const METERS_PER_FOOT = 3.28084

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
  const exceedsChainOnboard = catenary.recommendedRodeM > input.chainOnboardM
  const chainRemainingM = input.chainOnboardM - catenary.recommendedRodeM

  return {
    planningDepthM,
    depthSource,
    depthFromHawseM,
    recommendedRodeM: catenary.recommendedRodeM,
    recommendedScope: catenary.minScopeRatio,
    exceedsChainOnboard,
    chainRemainingM,
  }
}

/**
 * Extension point (ADR 0047 §2a): a second calculation method (the user's
 * ratio method) plugs in here later as a second `RodeMethod` function, plus
 * one entry in the array the planner component maps over. Only one method
 * exists today, so the component renders that one array entry directly
 * rather than behind a registry.
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

  const depthNote = plan.depthSource === 'tide'
    ? `Depth ${plan.planningDepthM.toFixed(1)} m (tide-corrected)`
    : `Depth ${plan.planningDepthM.toFixed(1)} m (sounder only)`

  return {
    id: 'catenary',
    label: 'Catenary Method',
    recommendedRodeM: plan.recommendedRodeM,
    scopeRatio: plan.recommendedScope,
    note: `${depthNote} · Wind ${Math.round(input.windKts ?? 0)} kts · Chain ${input.chainSizeMm}mm · Hull ${input.hullType.replace('_', ' ')}`,
  }
}

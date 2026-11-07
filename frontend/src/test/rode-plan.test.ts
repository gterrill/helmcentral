import { describe, expect, it } from 'vitest'
import { calculateCatenary } from '@/lib/catenary'
import {
  buildRodePlan,
  catenaryMethod,
  computeSwingRadiusM,
  deployedScopeStatus,
  maxExpectedDepthM,
  MIN_SCOPE_RATIO,
  planningFigureM,
  payOutDisplayValue,
  ratioMethod,
  rawDepthFromPlanningFigureM,
  resolveLoaM,
  resolvePlanningDepth,
  resolvePlanningWindBand,
  rodeMethods,
  rodeShortfallDisplay,
  scopeRatio,
  scopeStatus,
  scopeTargetOf,
  seedPlanningWindKts,
  tideHeightFtOrNull,
  WIND_BANDS,
  windBandForKts,
  type RodePlanInput,
} from '@/lib/rode-plan'
import type { TideToday } from '@/hooks/use-tide-today'
import type { GustWindow } from '@/lib/gust-windows'
import { feetToMeters } from '@/lib/units'

// Mirrors the unexported constant of the same name in rode-plan.ts — used
// only to build test fixtures in feet from a figure in metres, never to
// duplicate the module's own arithmetic.
const METERS_PER_FOOT = 3.28084

function makeTide(overrides: Partial<TideToday> = {}): TideToday {
  return {
    datetime: new Date(0).toISOString(),
    current_tide_height_ft: 2,
    tide_direction: 'Rising',
    high_tide_time: new Date(0).toISOString(),
    high_tide_height_ft: 5,
    low_tide_time: new Date(0).toISOString(),
    low_tide_height_ft: 0.5,
    station_name: 'Test Station',
    provider: 'test',
    ...overrides,
  }
}

// Not-anchored, live-depth-only baseline (no tide stamp on the datum, so no
// tide correction happens unless a test overrides `depth` too) — mirrors what
// the old baseInput's `sounderDepthM: 5, tide: null` meant before this change.
const baseInput: RodePlanInput = {
  depth: { depthM: 5, tideHeightFt: null },
  isAnchored: false,
  bowRollerHeightM: 1,
  tide: null,
  windKts: 20,
  seaState: 'calm',
  seabedType: 'sand',
  chainSizeMm: 10,
  chainOnboardM: 50,
  windageAreaM2: 20,
  hullType: 'power_mono',
}

// ---------------------------------------------------------------------------
// calculateCatenary — characterization tests. This function had zero coverage
// prior to this change; these pin its existing (correct, per ADR 0047) output
// so future edits can't silently change the recommended-rode math.
// ---------------------------------------------------------------------------
describe('calculateCatenary (characterization)', () => {
  it('case 1: 5m depth, 20kt wind, calm/sand, power_mono', () => {
    const result = calculateCatenary({
      depthM: 5,
      bowRollerHeightM: 1,
      windKts: 20,
      seaState: 'calm',
      seabedType: 'sand',
      chainSizeMm: 10,
      windageAreaM2: 20,
      hullType: 'power_mono',
    })
    expect(result).not.toBeNull()
    expect(result!.recommendedRodeM).toBeCloseTo(30.655137917018845, 6)
    expect(result!.minScopeRatio).toBeCloseTo(5.109189652836474, 6)
    expect(result!.horizontalLoadN).toBeCloseTo(1478.3495863536964, 6)
  })

  it('case 2: 10m depth, 35kt storm/mud, power_cat', () => {
    const result = calculateCatenary({
      depthM: 10,
      bowRollerHeightM: 1.5,
      windKts: 35,
      seaState: 'storm',
      seabedType: 'mud',
      chainSizeMm: 12,
      windageAreaM2: 35,
      hullType: 'power_cat',
    })
    expect(result).not.toBeNull()
    expect(result!.recommendedRodeM).toBeCloseTo(134.7093784202972, 6)
    expect(result!.minScopeRatio).toBeCloseTo(11.713858993069323, 6)
    expect(result!.horizontalLoadN).toBeCloseTo(22059.383009467023, 6)
  })

  it('case 3: 3m depth, 15kt choppy/rock, sail_mono', () => {
    const result = calculateCatenary({
      depthM: 3,
      bowRollerHeightM: 0.5,
      windKts: 15,
      seaState: 'choppy',
      seabedType: 'rock',
      chainSizeMm: 8,
      windageAreaM2: 15,
      hullType: 'sail_mono',
    })
    expect(result).not.toBeNull()
    expect(result!.recommendedRodeM).toBeCloseTo(19.649163597369846, 6)
    expect(result!.minScopeRatio).toBeCloseTo(5.61404674210567, 6)
    expect(result!.horizontalLoadN).toBeCloseTo(638.1218181622763, 6)
  })

  it('recommends the suspended length that just touches at the anchor: sqrt(h² + 2ah)', () => {
    const input = {
      depthM: 5,
      bowRollerHeightM: 1,
      windKts: 20,
      seaState: 'calm' as const,
      seabedType: 'sand' as const,
      chainSizeMm: 10,
      windageAreaM2: 20,
      hullType: 'power_mono' as const,
    }
    const result = calculateCatenary(input)!
    const h = 6
    // 10 mm chain: 2.3 kg/m dry, 0.87 of that submerged.
    const a = result.horizontalLoadN / (2.3 * 0.87 * 9.81)
    expect(result.catenaryParameterM).toBeCloseTo(a, 9)
    expect(result.recommendedRodeM).toBeCloseTo(Math.sqrt(h * h + 2 * a * h), 9)
    // The chain that long reaches the anchor with its tangent flat: enough to
    // reach the bottom, never shorter than the water is deep.
    expect(result.recommendedRodeM).toBeGreaterThan(h)
  })

  it('never recommends less than the depth to the hawse, however light the wind', () => {
    const result = calculateCatenary({
      depthM: 5,
      bowRollerHeightM: 1,
      windKts: 0.1,
      seaState: 'calm',
      seabedType: 'sand',
      chainSizeMm: 10,
      windageAreaM2: 20,
      hullType: 'power_mono',
    })!
    expect(result.minScopeRatio).toBeGreaterThan(1)
  })

  it('returns null for zero depth', () => {
    expect(calculateCatenary({ depthM: 0, bowRollerHeightM: 1, windKts: 20, seaState: 'calm', seabedType: 'sand', chainSizeMm: 10, windageAreaM2: 20, hullType: 'power_mono' })).toBeNull()
  })

  it('returns null for zero wind', () => {
    expect(calculateCatenary({ depthM: 5, bowRollerHeightM: 1, windKts: 0, seaState: 'calm', seabedType: 'sand', chainSizeMm: 10, windageAreaM2: 20, hullType: 'power_mono' })).toBeNull()
  })

  it('returns null for unconfigured windage area', () => {
    expect(calculateCatenary({ depthM: 5, bowRollerHeightM: 1, windKts: 20, seaState: 'calm', seabedType: 'sand', chainSizeMm: 10, windageAreaM2: 0, hullType: 'power_mono' })).toBeNull()
  })

  it('returns null for zero chain size', () => {
    expect(calculateCatenary({ depthM: 5, bowRollerHeightM: 1, windKts: 20, seaState: 'calm', seabedType: 'sand', chainSizeMm: 0, windageAreaM2: 20, hullType: 'power_mono' })).toBeNull()
  })

  it('returns null for negative wind', () => {
    expect(calculateCatenary({ depthM: 5, bowRollerHeightM: 1, windKts: -1, seaState: 'calm', seabedType: 'sand', chainSizeMm: 10, windageAreaM2: 20, hullType: 'power_mono' })).toBeNull()
  })
})

// ---------------------------------------------------------------------------
// tideHeightFtOrNull — useTideToday NEVER returns null; it returns
// defaultTide with -1 sentinels. This is the one place that -1-to-null
// normalization lives, so every caller (the two drop sites, the planner's
// override entry, and resolvePlanningDepth's own "live" datum) shares it.
// ---------------------------------------------------------------------------
describe('tideHeightFtOrNull', () => {
  it('returns the current tide height when the station has reported', () => {
    expect(tideHeightFtOrNull(makeTide({ current_tide_height_ft: 2.5 }))).toBe(2.5)
  })

  it('returns null on the -1 sentinel, not -1 itself', () => {
    expect(tideHeightFtOrNull(makeTide({ current_tide_height_ft: -1 }))).toBeNull()
  })

  it('returns null when there is no tide object at all', () => {
    expect(tideHeightFtOrNull(null)).toBeNull()
  })

  it('treats exactly 0 as a valid reading, not "unset" (the real check is >= 0, not !== -1)', () => {
    expect(tideHeightFtOrNull(makeTide({ current_tide_height_ft: 0 }))).toBe(0)
  })
})

// ---------------------------------------------------------------------------
// resolvePlanningDepth — the one place precedence lives (shaped like the
// existing resolvePlanningWindBand): anchored plans off the recorded
// planning depth; else (not anchored) live sounder; anchored + nothing
// recorded is a hard null, never a silent fall back to live depth.
// ---------------------------------------------------------------------------
describe('resolvePlanningDepth', () => {
  const tide = makeTide({ current_tide_height_ft: 3, high_tide_height_ft: 5 })

  it('rule 1: anchored uses the recorded planning depth, not the live sounder', () => {
    const result = resolvePlanningDepth({
      isAnchored: true,
      liveDepthM: 99,
      planningDepthM: 6,
      planningTideHeightFt: 1.5,
      tide,
    })
    expect(result).toEqual({ depthM: 6, tideHeightFt: 1.5 })
  })

  it('rule 2: not anchored uses the live sounder, tide-stamped from right now', () => {
    const result = resolvePlanningDepth({
      isAnchored: false,
      liveDepthM: 5,
      planningDepthM: null,
      planningTideHeightFt: null,
      tide,
    })
    expect(result).toEqual({ depthM: 5, tideHeightFt: 3 })
  })

  it('rule 3: anchored, nothing recorded -> null. Never a silent fall back to live depth', () => {
    const result = resolvePlanningDepth({
      isAnchored: true,
      liveDepthM: 5,
      planningDepthM: null,
      planningTideHeightFt: null,
      tide,
    })
    expect(result).toBeNull()
  })

  it('not anchored with no live reading is also null — nothing to plan against', () => {
    const result = resolvePlanningDepth({
      isAnchored: false,
      liveDepthM: null,
      planningDepthM: null,
      planningTideHeightFt: null,
      tide,
    })
    expect(result).toBeNull()
  })

  it('the live datum has no tide stamp when the tide station has no reading (sentinel -1)', () => {
    const result = resolvePlanningDepth({
      isAnchored: false,
      liveDepthM: 5,
      planningDepthM: null,
      planningTideHeightFt: null,
      tide: makeTide({ current_tide_height_ft: -1 }),
    })
    expect(result).toEqual({ depthM: 5, tideHeightFt: null })
  })
})

// ---------------------------------------------------------------------------
// maxExpectedDepthM
// ---------------------------------------------------------------------------
describe('maxExpectedDepthM', () => {
  it('adds the rise to the next high on a rising tide', () => {
    // datum's own tide-at-reading is 2ft, high 5ft -> rise 3ft -> 3/3.28084 = 0.914...m
    const datum = { depthM: 5, tideHeightFt: 2 }
    const result = maxExpectedDepthM(datum, makeTide({ high_tide_height_ft: 5 }))
    expect(result).toBeCloseTo(5 + 3 / 3.28084, 6)
  })

  it('clamps a falling tide so planning depth never drops below the datum reading', () => {
    // datum's tide-at-reading is 4ft, already above high 3ft (falling past the
    // high) -> rise would be negative; must clamp to 0, not subtract.
    const datum = { depthM: 5, tideHeightFt: 4 }
    const result = maxExpectedDepthM(datum, makeTide({ high_tide_height_ft: 3 }))
    expect(result).toBe(5)
  })

  it('returns null when the datum is unavailable', () => {
    expect(maxExpectedDepthM(null, makeTide())).toBeNull()
  })

  it('returns null when tide is unavailable', () => {
    expect(maxExpectedDepthM({ depthM: 5, tideHeightFt: 2 }, null)).toBeNull()
  })

  it('returns null when the datum itself has no tide stamp (e.g. nothing recorded at the moment of drop)', () => {
    expect(maxExpectedDepthM({ depthM: 5, tideHeightFt: null }, makeTide())).toBeNull()
  })

  it('returns null when the high-tide forecast has no reading (sentinel -1)', () => {
    const datum = { depthM: 5, tideHeightFt: 2 }
    expect(maxExpectedDepthM(datum, makeTide({ high_tide_height_ft: -1 }))).toBeNull()
  })

  // The datum-mixing regression: the bug this whole change fixes was mixing a
  // depth reading from one moment with the tide reading from another. This
  // pins that the rise is measured from the tide stamped alongside the
  // reading, never from whatever the tide happens to be right now.
  it('measures the rise from the tide at the reading, not the tide now', () => {
    const datum = { depthM: 6, tideHeightFt: 1 }
    // Risen from 1 ft to 4 ft since the reading; next high 5 ft.
    const tide = makeTide({ current_tide_height_ft: 4, high_tide_height_ft: 5 })
    expect(maxExpectedDepthM(datum, tide)).toBeCloseTo(6 + 4 / 3.28084, 6)     // correct
    expect(maxExpectedDepthM(datum, tide)).not.toBeCloseTo(6 + 1 / 3.28084, 6) // the bug: short, unsafe
  })
})

// ---------------------------------------------------------------------------
// planningFigureM — the figure the Depth field displays: depth at the next
// high tide plus bow roller height (depth-from-hawse at high water), not the
// raw sounder/recorded reading maxExpectedDepthM/buildRodePlan plan against
// internally. Deliberately thin: it's maxExpectedDepthM (falling back to the
// raw datum depth on the same null conditions) plus bowRollerHeightM.
// ---------------------------------------------------------------------------
describe('planningFigureM', () => {
  it('is null when the datum is null', () => {
    expect(planningFigureM(null, makeTide(), 1.5)).toBeNull()
  })

  it('adds the rise to the next high, plus bow height, when tide-corrected', () => {
    // datum's own tide-at-reading is 2ft, high 5ft -> rise 3ft -> 0.9144m
    const datum = { depthM: 5, tideHeightFt: 2 }
    const result = planningFigureM(datum, makeTide({ high_tide_height_ft: 5 }), 1.5)
    expect(result).toBeCloseTo(5 + 3 / 3.28084 + 1.5, 6)
  })

  it('falls back to the raw datum depth plus bow height when uncorrected (no tide stamp on the datum)', () => {
    const datum = { depthM: 5, tideHeightFt: null }
    const result = planningFigureM(datum, makeTide(), 1.5)
    expect(result).toBeCloseTo(5 + 1.5, 6)
  })

  // The worked example from the ADR: 1.0 m now at low tide, next high 3.0 m
  // above the tide at the reading, 1.5 m bow -> 5.5 m in the Depth field.
  it('the worked example: 1.0 m raw, 3.0 m rise to high, 1.5 m bow -> 5.5', () => {
    // tideHeightFt in feet, so pick a rise that comes out to exactly 3.0 m:
    // 3.0 m * 3.28084 ft/m = 9.84252 ft rise.
    const datum = { depthM: 1.0, tideHeightFt: 0 }
    const result = planningFigureM(datum, makeTide({ high_tide_height_ft: 3.0 * METERS_PER_FOOT }), 1.5)
    expect(result).toBeCloseTo(5.5, 6)
  })
})

// ---------------------------------------------------------------------------
// rawDepthFromPlanningFigureM — the exact inverse of planningFigureM, so an
// operator's typed figure (depth at high tide + bow height) can be converted
// back to the raw reading that gets persisted (ADR 0063's pair rule), without
// seeding the input with a corrected number and then storing the typed value
// as raw — that would compound the rise on every render.
// ---------------------------------------------------------------------------
describe('rawDepthFromPlanningFigureM', () => {
  it('the worked example inverted: figure 5.5, 3.0 m rise, 1.5 m bow -> raw 1.0', () => {
    const tide = makeTide({ high_tide_height_ft: 3.0 * METERS_PER_FOOT })
    const result = rawDepthFromPlanningFigureM(5.5, 0, tide, 1.5)
    expect(result).toBeCloseTo(1.0, 6)
  })

  it('rise is 0 when tideNowFt is null, matching maxExpectedDepthM\'s null conditions', () => {
    const tide = makeTide({ high_tide_height_ft: 5 })
    const result = rawDepthFromPlanningFigureM(6.5, null, tide, 1.5)
    expect(result).toBeCloseTo(6.5 - 1.5, 6)
  })

  it('rise is 0 when there is no tide forecast at all', () => {
    const result = rawDepthFromPlanningFigureM(6.5, 2, null, 1.5)
    expect(result).toBeCloseTo(6.5 - 1.5, 6)
  })

  it('clamps a falling tide to zero rise, same as maxExpectedDepthM', () => {
    // tideNowFt (4) already above the next high (3) -> rise would be
    // negative -> clamped to 0, not subtracted.
    const tide = makeTide({ high_tide_height_ft: 3 })
    const result = rawDepthFromPlanningFigureM(6.5, 4, tide, 1.5)
    expect(result).toBeCloseTo(6.5 - 1.5, 6)
  })

  it('returns null when the typed figure does not clear bow height plus tide rise', () => {
    // rise (3ft/3.28084m) + bow (1.5m) is the boundary; a figure comfortably
    // below that implies a raw depth <= 0, which is not a sensible reading.
    const tide = makeTide({ high_tide_height_ft: 5 })
    const boundaryM = 1.5 + 3 / 3.28084
    const result = rawDepthFromPlanningFigureM(boundaryM - 0.5, 2, tide, 1.5)
    expect(result).toBeNull()
  })

  it('returns null (not zero or negative) exactly at the boundary', () => {
    const tide = makeTide({ high_tide_height_ft: 5 })
    const riseM = 3 / 3.28084
    const result = rawDepthFromPlanningFigureM(1.5 + riseM, 2, tide, 1.5)
    expect(result).toBeNull()
  })
})

// ---------------------------------------------------------------------------
// planningFigureM / rawDepthFromPlanningFigureM round trip — persisting
// rawDepthFromPlanningFigureM(x, tideNow, tide, bow) stamped with tideNow,
// then reading planningFigureM back off that datum, must return x exactly:
// this is the exact inversion ADR 0063's amendment relies on to avoid
// compounding the tide correction on every render.
// ---------------------------------------------------------------------------
describe('planningFigureM / rawDepthFromPlanningFigureM round trip', () => {
  function roundTrip(figureM: number, tideNowFt: number | null, tide: TideToday | null, bowRollerHeightM: number): number | null {
    const rawDepthM = rawDepthFromPlanningFigureM(figureM, tideNowFt, tide, bowRollerHeightM)
    if (rawDepthM === null) return null
    const datum = { depthM: rawDepthM, tideHeightFt: tideNowFt }
    return planningFigureM(datum, tide, bowRollerHeightM)
  }

  it('round-trips exactly with a rising tide', () => {
    const tide = makeTide({ current_tide_height_ft: 2, high_tide_height_ft: 5 })
    expect(roundTrip(6.9, 2, tide, 1)).toBeCloseTo(6.9, 6)
  })

  it('round-trips exactly with no tide station at all', () => {
    expect(roundTrip(6.9, null, null, 1)).toBeCloseTo(6.9, 6)
  })

  it('round-trips exactly on a falling tide (rise clamped to 0)', () => {
    const tide = makeTide({ current_tide_height_ft: 4, high_tide_height_ft: 3 })
    expect(roundTrip(6.9, 4, tide, 1)).toBeCloseTo(6.9, 6)
  })

  it('round-trips the worked example: 5.5 m figure, 3.0 m rise, 1.5 m bow', () => {
    const tide = makeTide({ high_tide_height_ft: 3.0 * METERS_PER_FOOT })
    expect(roundTrip(5.5, 0, tide, 1.5)).toBeCloseTo(5.5, 6)
  })
})

// ---------------------------------------------------------------------------
// scopeRatio
// ---------------------------------------------------------------------------
describe('scopeRatio', () => {
  it('divides rode by hawse depth', () => {
    expect(scopeRatio(30, 6)).toBe(5)
  })

  it('returns null when hawse depth is zero or negative', () => {
    expect(scopeRatio(30, 0)).toBeNull()
    expect(scopeRatio(30, -1)).toBeNull()
  })
})

// ---------------------------------------------------------------------------
// scopeStatus — the ok / low (>=85%) / insufficient / unknown ladder
// ---------------------------------------------------------------------------
describe('scopeStatus', () => {
  it('is "ok" when current scope meets or exceeds the recommendation', () => {
    expect(scopeStatus(5, 5)).toBe('ok')
    expect(scopeStatus(6, 5)).toBe('ok')
  })

  it('is "low" at and above the 85% boundary but below the recommendation', () => {
    expect(scopeStatus(4.25, 5)).toBe('low') // exactly 85%
    expect(scopeStatus(4.5, 5)).toBe('low')
  })

  it('is "insufficient" below the 85% boundary', () => {
    expect(scopeStatus(4.2, 5)).toBe('insufficient')
    expect(scopeStatus(0, 5)).toBe('insufficient')
  })

  it('is "unknown" when either input is null — never a false pass or fail', () => {
    expect(scopeStatus(null, 5)).toBe('unknown')
    expect(scopeStatus(5, null)).toBe('unknown')
    expect(scopeStatus(null, null)).toBe('unknown')
  })

  it('grades against a floored recommendedScope — the badge sees 3:1, not the raw sub-floor ratio', () => {
    const plan = buildRodePlan({ ...baseInput, windKts: 3 })
    expect(plan).not.toBeNull()
    expect(plan!.recommendedScope).toBe(3)
    expect(scopeStatus(3, plan!.recommendedScope)).toBe('ok')
  })
})

// ---------------------------------------------------------------------------
// deployedScopeStatus — the deployed rode against the Pay Out figure as shown
// ---------------------------------------------------------------------------
describe('deployedScopeStatus', () => {
  const real = buildRodePlan(baseInput)!
  // Keeps the plan consistent: the scope follows the overridden rode.
  const planWith = (recommendedRodeM: number) => ({
    recommendedRodeM,
    depthFromHawseM: real.depthFromHawseM,
    scopeRatio: recommendedRodeM / real.depthFromHawseM,
  })

  it('is "ok" when the deployed rode equals the rounded-down shown figure', () => {
    expect(deployedScopeStatus(55, planWith(55.3), false)).toBe('ok')
  })

  it('does not call a rode one unit short of the shown figure "ok"', () => {
    expect(deployedScopeStatus(54, planWith(55.3), false)).not.toBe('ok')
  })

  it('judges against the rounded-up shown figure, not the raw recommendation', () => {
    expect(deployedScopeStatus(55, planWith(54.7), false)).toBe('ok')
    expect(deployedScopeStatus(54.6, planWith(54.7), false)).not.toBe('ok')
  })

  it('compares in feet under imperial, so a feet to metres round trip cannot read short', () => {
    const plan = planWith(33.3 / METERS_PER_FOOT) // shows 33 ft
    expect(deployedScopeStatus(33 / METERS_PER_FOOT, plan, true)).toBe('ok')
    expect(deployedScopeStatus(32 / METERS_PER_FOOT, plan, true)).not.toBe('ok')
  })

  it('falls through to the low / insufficient ladder when short of the shown figure', () => {
    const plan = planWith(55.3)
    const low = deployedScopeStatus(54, plan, false)
    expect(low).toBe(scopeStatus(scopeRatio(54, plan.depthFromHawseM), plan.scopeRatio))
    expect(deployedScopeStatus(1, plan, false)).toBe('insufficient')
  })

  it('is "unknown" with no target', () => {
    expect(deployedScopeStatus(55, null, false)).toBe('unknown')
  })
})

// ---------------------------------------------------------------------------
// buildRodePlan
// ---------------------------------------------------------------------------
describe('buildRodePlan', () => {
  it('uses the raw live depth with no tide correction', () => {
    const plan = buildRodePlan(baseInput)
    expect(plan).not.toBeNull()
    expect(plan!.tideCorrected).toBe(false)
    expect(plan!.planningDepthM).toBe(5)
    expect(plan!.depthFromHawseM).toBe(6)
    expect(plan!.recommendedRodeM).toBeCloseTo(30.655137917018845, 6)
    expect(plan!.recommendedScope).toBeCloseTo(5.109189652836474, 6)
    // The parameter the recommendation used, handed on for drawing the rode.
    expect(plan!.catenaryParameterM).toBeCloseTo(1478.3495863536964 / (2.3 * 0.87 * 9.81), 6)
  })

  it('uses the tide-corrected depth and reports tideCorrected true when the datum carries a tide stamp', () => {
    const plan = buildRodePlan({
      ...baseInput,
      depth: { depthM: 5, tideHeightFt: 2 },
      tide: makeTide({ current_tide_height_ft: 2, high_tide_height_ft: 5 }),
    })
    expect(plan).not.toBeNull()
    expect(plan!.tideCorrected).toBe(true)
    expect(plan!.planningDepthM).toBeCloseTo(5 + 3 / 3.28084, 6)
  })

  it('flags exceedsChainOnboard when the recommendation is more chain than is aboard', () => {
    const plan = buildRodePlan({ ...baseInput, chainOnboardM: 10 })
    expect(plan).not.toBeNull()
    expect(plan!.exceedsChainOnboard).toBe(true)
    expect(plan!.chainRemainingM).toBeCloseTo(10 - 30.655137917018845, 6)
  })

  it('does not flag exceedsChainOnboard when there is enough chain aboard', () => {
    const plan = buildRodePlan({ ...baseInput, chainOnboardM: 500 })
    expect(plan).not.toBeNull()
    expect(plan!.exceedsChainOnboard).toBe(false)
  })

  it('returns null when depth is unavailable', () => {
    expect(buildRodePlan({ ...baseInput, depth: null })).toBeNull()
  })

  it('returns null when wind is unavailable', () => {
    expect(buildRodePlan({ ...baseInput, windKts: null })).toBeNull()
  })

  it('returns null when the catenary calc itself is unavailable (e.g. no windage configured)', () => {
    expect(buildRodePlan({ ...baseInput, windageAreaM2: 0 })).toBeNull()
  })
})

// ---------------------------------------------------------------------------
// buildRodePlan — MIN_SCOPE_RATIO floor. A calm-night, 3kt-wind input fed the
// raw catenary model down to a little over 1:1: mathematically correct (the chain just
// reaches the anchor with a flat tangent), nautically absurd — below ~3:1 the
// anchor pulls upward regardless of what the catenary math says is holding. The floor is composition policy, applied
// here rather than in catenary.ts, so the physics stays pinned and
// policy-free.
// ---------------------------------------------------------------------------
describe('buildRodePlan — MIN_SCOPE_RATIO floor', () => {
  it('floors recommendedRodeM/recommendedScope to MIN_SCOPE_RATIO in light wind and flags scopeFloorApplied', () => {
    const plan = buildRodePlan({ ...baseInput, windKts: 3 })
    expect(plan).not.toBeNull()
    // depthFromHawseM is 5 (live) + 1 (bow roller) = 6, per baseInput.
    expect(plan!.depthFromHawseM).toBe(6)
    expect(plan!.recommendedRodeM).toBe(18) // MIN_SCOPE_RATIO * 6
    expect(plan!.recommendedScope).toBe(3)
    expect(plan!.scopeFloorApplied).toBe(true)
  })

  it('leaves the 20kt characterization result untouched — the floor never binds above ~5:1', () => {
    const plan = buildRodePlan({ ...baseInput, windKts: 20 })
    expect(plan).not.toBeNull()
    expect(plan!.scopeFloorApplied).toBe(false)
    expect(plan!.recommendedRodeM).toBeCloseTo(30.655137917018845, 6)
    expect(plan!.recommendedScope).toBeCloseTo(5.109189652836474, 6)
  })

  it('leaves a raw ratio just above the floor alone (boundary sanity)', () => {
    // minScopeRatio scales linearly with windKts here (all other catenary
    // terms held constant), so 12kt sits just above 3:1 (~3.006:1) without
    // hand-picking a hardcoded expected number — compare against the same
    // physics function rather than a magic constant.
    const raw = calculateCatenary({
      depthM: baseInput.depth!.depthM,
      bowRollerHeightM: baseInput.bowRollerHeightM,
      windKts: 12,
      seaState: baseInput.seaState,
      seabedType: baseInput.seabedType,
      chainSizeMm: baseInput.chainSizeMm,
      windageAreaM2: baseInput.windageAreaM2,
      hullType: baseInput.hullType,
    })
    expect(raw).not.toBeNull()
    expect(raw!.minScopeRatio).toBeGreaterThan(MIN_SCOPE_RATIO)

    const plan = buildRodePlan({ ...baseInput, windKts: 12 })
    expect(plan).not.toBeNull()
    expect(plan!.scopeFloorApplied).toBe(false)
    expect(plan!.recommendedRodeM).toBeCloseTo(raw!.recommendedRodeM, 6)
    expect(plan!.recommendedScope).toBeCloseTo(raw!.minScopeRatio, 6)
  })

  it('derives exceedsChainOnboard/chainRemainingM from the floored rode, not the raw catenary figure', () => {
    // Raw catenary rode at 3kt is well under 10m, so an unfloored comparison
    // would (wrongly) report plenty of chain in hand; the floored 18m rode
    // exceeds the 10m onboard.
    const plan = buildRodePlan({ ...baseInput, windKts: 3, chainOnboardM: 10 })
    expect(plan).not.toBeNull()
    expect(plan!.recommendedRodeM).toBe(18)
    expect(plan!.exceedsChainOnboard).toBe(true)
    expect(plan!.chainRemainingM).toBe(10 - 18)
  })
})

// ---------------------------------------------------------------------------
// catenaryMethod — the RodeMethod seam for §2a
// ---------------------------------------------------------------------------
describe('catenaryMethod', () => {
  it('returns a result with no unavailableReason when inputs are complete', () => {
    const result = catenaryMethod(baseInput)
    expect(result).not.toBeNull()
    expect(result!.id).toBe('catenary')
    expect(result!.unavailableReason).toBeUndefined()
    expect(result!.recommendedRodeM).toBeCloseTo(30.655137917018845, 6)
  })

  it('labels the method plainly, with no echo of its inputs', () => {
    expect(catenaryMethod(baseInput)!.label).toBe('Catenary')
    expect(ratioMethod(baseInput)!.label).toBe('Ratio')
    expect(catenaryMethod({ ...baseInput, depth: null })!.label).toBe('Catenary')
    expect(ratioMethod({ ...baseInput, depth: null })!.label).toBe('Ratio')
    expect(catenaryMethod(baseInput)).not.toHaveProperty('note')
    expect(ratioMethod(baseInput)).not.toHaveProperty('note')
  })

  it('names the missing input in unavailableReason rather than rendering a dash', () => {
    const result = catenaryMethod({ ...baseInput, depth: null })
    expect(result).not.toBeNull()
    expect(result!.unavailableReason).toBeTruthy()
    expect(typeof result!.unavailableReason).toBe('string')
  })

  it('names a different reason when wind is missing than when depth is missing', () => {
    const noDepth = catenaryMethod({ ...baseInput, depth: null })
    const noWind = catenaryMethod({ ...baseInput, windKts: null })
    expect(noDepth!.unavailableReason).not.toBe(noWind!.unavailableReason)
  })

  it('names "no depth reading" when not anchored, and "no depth entered" when anchored — same missing depth, different reason', () => {
    const notAnchored = catenaryMethod({ ...baseInput, depth: null, isAnchored: false })
    const anchored = catenaryMethod({ ...baseInput, depth: null, isAnchored: true })
    expect(notAnchored!.unavailableReason).toBe('no depth reading')
    expect(anchored!.unavailableReason).toBe('no depth entered')
  })
})

// ---------------------------------------------------------------------------
// ratioMethod — the second RodeMethod (ADR 0047 §2a extension seam): pay out
// (planning depth + bow roller height) x 7 in a blow, x 5 otherwise. Reads
// only depth, tide, wind, and bow roller height — never chain size, windage
// area, sea state, seabed type, or hull type.
// ---------------------------------------------------------------------------
describe('ratioMethod', () => {
  it('uses the 5:1 ratio below the wind threshold', () => {
    const result = ratioMethod({ ...baseInput, windKts: 15 })
    expect(result).not.toBeNull()
    expect(result!.id).toBe('ratio')
    expect(result!.recommendedRodeM).toBeCloseTo(30, 6) // (5 + 1) * 5
    expect(result!.scopeRatio).toBe(5)
    expect(result!.unavailableReason).toBeUndefined()
  })

  it('switches to the 7:1 ratio at the wind threshold (>= is inclusive)', () => {
    const result = ratioMethod({ ...baseInput, windKts: 20 })
    expect(result!.recommendedRodeM).toBeCloseTo(42, 6) // (5 + 1) * 7
    expect(result!.scopeRatio).toBe(7)
  })

  it('stays at 7:1 above the wind threshold', () => {
    const result = ratioMethod({ ...baseInput, windKts: 35 })
    expect(result!.recommendedRodeM).toBeCloseTo(42, 6)
    expect(result!.scopeRatio).toBe(7)
  })

  it('stays at 5:1 just below the wind threshold', () => {
    const result = ratioMethod({ ...baseInput, windKts: 19.9 })
    expect(result!.recommendedRodeM).toBeCloseTo(30, 6)
    expect(result!.scopeRatio).toBe(5)
  })

  it('plans against tide-corrected depth, same as buildRodePlan, when the datum carries a tide stamp', () => {
    const result = ratioMethod({
      ...baseInput,
      windKts: 15,
      depth: { depthM: 5, tideHeightFt: 2 },
      tide: makeTide({ current_tide_height_ft: 2, high_tide_height_ft: 5 }),
    })
    expect(result).not.toBeNull()
    const expectedDepthM = 5 + 3 / 3.28084
    expect(result!.recommendedRodeM).toBeCloseTo((expectedDepthM + 1) * 5, 6)
    expect(result!.tideCorrected).toBe(true)
  })

  it('falls back to sounder-only depth when the datum carries no tide stamp', () => {
    const result = ratioMethod({
      ...baseInput,
      windKts: 15,
      tide: makeTide({ current_tide_height_ft: -1 }),
    })
    expect(result).not.toBeNull()
    expect(result!.recommendedRodeM).toBeCloseTo((5 + 1) * 5, 6)
    expect(result!.tideCorrected).toBe(false)
  })

  it('names "no depth reading" first in the unavailable ladder when not anchored', () => {
    const result = ratioMethod({ ...baseInput, depth: null })
    expect(result!.unavailableReason).toBe('no depth reading')
  })

  it('names "no depth entered" when anchored with nothing recorded', () => {
    const result = ratioMethod({ ...baseInput, depth: null, isAnchored: true })
    expect(result!.unavailableReason).toBe('no depth entered')
  })

  it('names "no wind data" when depth is present but wind is missing', () => {
    const result = ratioMethod({ ...baseInput, windKts: null })
    expect(result!.unavailableReason).toBe('no wind data')
  })

  it('names "bow roller height not configured" when depth and wind are present but bow roller is unset', () => {
    const result = ratioMethod({ ...baseInput, bowRollerHeightM: 0 })
    expect(result!.unavailableReason).toBe('bow roller height not configured')
  })

  it('computes fine with the catenary-only inputs left unconfigured', () => {
    const result = ratioMethod({ ...baseInput, chainSizeMm: 0, windageAreaM2: 0 })
    expect(result).not.toBeNull()
    expect(result!.unavailableReason).toBeUndefined()
    expect(result!.recommendedRodeM).toBeCloseTo(42, 6) // baseInput wind is 20 -> 7:1
  })

  // MIN_SCOPE_RATIO (3) holds here by construction — RATIO_NORMAL (5) is
  // already above it — so there is no runtime clamp to test breaking; this
  // locks in that the smallest ratio the method ever returns stays clear of
  // the floor, including at zero wind (a valid number, not "no wind data").
  it('never returns below MIN_SCOPE_RATIO, even at zero wind', () => {
    const result = ratioMethod({ ...baseInput, windKts: 0 })
    expect(result).not.toBeNull()
    expect(result!.unavailableReason).toBeUndefined()
    expect(result!.scopeRatio).toBe(5)
    expect(result!.scopeRatio).toBeGreaterThanOrEqual(MIN_SCOPE_RATIO)
  })

  // The extracted planningDepthWithTide helper (rode-plan.ts) is what both
  // buildRodePlan and ratioMethod call, rather than ratioMethod re-deriving
  // the same three tide lines separately — this guards that the two stay in
  // lockstep by construction, not by two call sites happening to agree today.
  it('derives its planning depth from the same shared datum-plus-tide arithmetic buildRodePlan uses', () => {
    const input: RodePlanInput = {
      ...baseInput,
      windKts: 15,
      isAnchored: true,
      depth: { depthM: 6, tideHeightFt: 1 },
      tide: makeTide({ current_tide_height_ft: 4, high_tide_height_ft: 5 }),
    }
    const plan = buildRodePlan(input)
    const result = ratioMethod(input)
    expect(plan).not.toBeNull()
    expect(result).not.toBeNull()
    expect(plan!.tideCorrected).toBe(true)
    expect(result!.recommendedRodeM).toBeCloseTo((plan!.planningDepthM + input.bowRollerHeightM) * 5, 6)
  })
})

// ---------------------------------------------------------------------------
// rodeMethods — the array the planner component maps over (ADR 0047 §2a)
// ---------------------------------------------------------------------------
describe('rodeMethods', () => {
  it('lists catenaryMethod then ratioMethod', () => {
    expect(rodeMethods).toEqual([catenaryMethod, ratioMethod])
  })
})

// ---------------------------------------------------------------------------
// seedPlanningWindKts — the planner's wind-seed rule, extracted so it exists
// in one place (ADR 0047: plan for the forecast gust, not the breeze at the
// moment you drop the hook).
// ---------------------------------------------------------------------------
describe('seedPlanningWindKts', () => {
  it('prefers the 1h max gust when available', () => {
    const result = seedPlanningWindKts({ '10m': null, '30m': null, '1h': 20, '24h': null }, 12)
    expect(result).toEqual({ windKts: 20, source: 'gust' })
  })

  it('falls back to apparent wind when no 1h gust is recorded', () => {
    const result = seedPlanningWindKts({ '10m': null, '30m': null, '1h': null, '24h': null }, 12)
    expect(result).toEqual({ windKts: 12, source: 'apparent' })
  })

  it('yields no seed and no source when both are unavailable', () => {
    const result = seedPlanningWindKts({ '10m': null, '30m': null, '1h': null, '24h': null }, null)
    expect(result).toEqual({ windKts: null, source: null })
  })
})

// ─────────────────────────────────────────────────────────────
// Wind bands — the planner picks a forecast band rather than typing a figure
// ─────────────────────────────────────────────────────────────
describe('windBandForKts', () => {
  it('picks the band containing the reading', () => {
    expect(windBandForKts(12.3)!.id).toBe('10-15')
    expect(windBandForKts(3)!.id).toBe('0-10')
    expect(windBandForKts(27)!.id).toBe('25-30')
    expect(windBandForKts(35)!.id).toBe('30-40')
    expect(windBandForKts(44.9)!.id).toBe('40-50')
  })

  it('treats each band as lower-inclusive so a reading on the boundary steps up', () => {
    // Matches ratioMethod's `>= 20` convention: the boundary belongs to the
    // stronger band, never the calmer one.
    expect(windBandForKts(10)!.id).toBe('10-15')
    expect(windBandForKts(15)!.id).toBe('15-20')
    expect(windBandForKts(20)!.id).toBe('20-25')
    expect(windBandForKts(50)!.id).toBe('50+')
  })

  it('puts anything above the top band in the open-ended band', () => {
    expect(windBandForKts(65)!.id).toBe('50+')
    expect(windBandForKts(120)!.id).toBe('50+')
  })

  it('returns null with no reading, rather than defaulting to the calmest band', () => {
    // A guessed band would silently drive a real recommendation; the caller
    // renders "no wind data" instead (fallback policy).
    expect(windBandForKts(null)).toBeNull()
  })

  it('clamps a nonsensical negative reading into the calmest band', () => {
    expect(windBandForKts(-2)!.id).toBe('0-10')
  })
})

describe('WIND_BANDS', () => {
  it('plans at the top of each closed band — you rig for the worst of the range', () => {
    const byId = Object.fromEntries(WIND_BANDS.map((b) => [b.id, b]))
    expect(byId['0-10'].planKts).toBe(10)
    expect(byId['10-15'].planKts).toBe(15)
    expect(byId['20-25'].planKts).toBe(25)
    expect(byId['40-50'].planKts).toBe(50)
  })

  it('plans the open band at its floor, the only honest figure available', () => {
    const open = WIND_BANDS[WIND_BANDS.length - 1]
    expect(open.id).toBe('50+')
    expect(open.maxKts).toBeNull()
    expect(open.planKts).toBe(50)
  })

  it('covers the range with no gaps between one band and the next', () => {
    for (let i = 1; i < WIND_BANDS.length; i++) {
      expect(WIND_BANDS[i].minKts).toBe(WIND_BANDS[i - 1].maxKts)
    }
  })
})

// ─────────────────────────────────────────────────────────────
// resolvePlanningWindBand — the single band-selection rule behind the Rode
// Planner's dropdown.
// ─────────────────────────────────────────────────────────────
describe('resolvePlanningWindBand', () => {
  const noGust: Record<GustWindow, number | null> = { '10m': null, '30m': null, '1h': null, '24h': null }

  it('an explicit id wins over the live seed', () => {
    const band = resolvePlanningWindBand({ ...noGust, '1h': 3 }, null, '25-30')
    expect(band!.id).toBe('25-30')
  })

  it('falls back to the seeded band when the id is unrecognised — never throws, never means "calmest"', () => {
    const band = resolvePlanningWindBand({ ...noGust, '1h': 27 }, null, 'not-a-real-band')
    expect(band!.id).toBe('25-30')
  })

  it('uses the seeded band (1h gust, else apparent) when no id is selected', () => {
    const band = resolvePlanningWindBand(noGust, 22, null)
    expect(band!.id).toBe('20-25')
  })

  it('returns null when there is no selection and no live seed', () => {
    expect(resolvePlanningWindBand(noGust, null, null)).toBeNull()
  })
})

// ─────────────────────────────────────────────────────────────
// resolveLoaM — LOA precedence (ADR 0047), extracted from
// anchor-rode-planner.tsx so the Adjust mode's radius ceiling
// (lib/anchor-adjust.ts) resolves the operator's boat length the same way.
// ─────────────────────────────────────────────────────────────
describe('resolveLoaM', () => {
  it('prefers an explicit settings.anchor.loa_m over SignalK', () => {
    expect(resolveLoaM(15, 12)).toEqual({ loaM: 15, source: 'settings' })
  })

  it('falls back to SignalK design.length.overall when settings has none', () => {
    expect(resolveLoaM(0, 12)).toEqual({ loaM: 12, source: 'signalk' })
  })

  it('a zero or negative SignalK length does not count as configured', () => {
    expect(resolveLoaM(0, 0)).toEqual({ loaM: null, source: null })
    expect(resolveLoaM(0, -1)).toEqual({ loaM: null, source: null })
  })

  it('returns null with neither source present, not a silent zero', () => {
    expect(resolveLoaM(0, null)).toEqual({ loaM: null, source: null })
  })
})

// ─────────────────────────────────────────────────────────────
// computeSwingRadiusM — rode + bow offset + LOA, the swing circle a method's
// recommended rode implies. Extracted from anchor-rode-planner.tsx; the
// Adjust mode's "Planner swing" chip needs the identical figure.
// ─────────────────────────────────────────────────────────────
describe('computeSwingRadiusM', () => {
  it('sums recommended rode, bow offset and LOA', () => {
    expect(computeSwingRadiusM(30, 2, 12)).toBe(44)
  })

  it('returns null when LOA is unresolved, rather than a circle smaller than the boat', () => {
    expect(computeSwingRadiusM(30, 2, null)).toBeNull()
  })
})

describe('rodeShortfallDisplay', () => {
  const target = scopeTargetOf(catenaryMethod(baseInput))!
  const payOut = payOutDisplayValue(target.recommendedRodeM, false)

  it('is the Pay Out figure less the rode deployed, in whole display units', () => {
    expect(rodeShortfallDisplay(payOut - 7, target, false)).toBe(7)
  })

  it('rounds up, so paying out that many clears the badge', () => {
    const t = { ...target, recommendedRodeM: 55 }
    expect(rodeShortfallDisplay(50.6, t, false)).toBe(5)
  })

  it('never reads less than 1, so a rode a hair short is still short', () => {
    expect(rodeShortfallDisplay(payOut - 0.2, target, false)).toBe(1)
  })

  it('counts in feet when imperial', () => {
    const payOutFt = payOutDisplayValue(target.recommendedRodeM, true)
    expect(rodeShortfallDisplay(feetToMeters(payOutFt - 12), target, true)).toBe(12)
  })
})

describe('scopeTargetOf', () => {
  it('takes a method\'s own rode, height and scope', () => {
    const ratio = ratioMethod(baseInput)!
    expect(scopeTargetOf(ratio)).toEqual({
      recommendedRodeM: ratio.recommendedRodeM,
      depthFromHawseM: ratio.depthFromHawseM,
      scopeRatio: ratio.scopeRatio,
    })
  })

  it('is null when the method is unavailable', () => {
    expect(scopeTargetOf(catenaryMethod({ ...baseInput, depth: null }))).toBeNull()
    expect(scopeTargetOf(null)).toBeNull()
  })
})

describe('scopeFloorApplied on the method result', () => {
  it('is true on the catenary result when the minimum ratio set the figure', () => {
    expect(catenaryMethod({ ...baseInput, windKts: 3 })!.scopeFloorApplied).toBe(true)
  })

  it('is false when the model cleared the floor, and absent when unavailable', () => {
    expect(catenaryMethod(baseInput)!.scopeFloorApplied).toBe(false)
    expect(catenaryMethod({ ...baseInput, depth: null })!.scopeFloorApplied).toBeUndefined()
  })
})

// ---------------------------------------------------------------------------
// Both methods hand on the height and tide status they planned against, so a
// drawing of the rode uses exactly that figure.
// ---------------------------------------------------------------------------
describe('rode methods report the height they planned against', () => {
  const tided = {
    ...baseInput,
    depth: { depthM: 5, tideHeightFt: 2 },
    tide: makeTide({ current_tide_height_ft: 2, high_tide_height_ft: 5 }),
  }

  it('the catenary method reports the plan depth from the hawse and tideCorrected', () => {
    const plan = buildRodePlan(tided)!
    const result = catenaryMethod(tided)!
    expect(result.depthFromHawseM).toBeCloseTo(plan.depthFromHawseM, 9)
    expect(result.tideCorrected).toBe(true)
    expect(catenaryMethod(baseInput)!.tideCorrected).toBe(false)
    expect(catenaryMethod(baseInput)!.depthFromHawseM).toBe(6)
  })

  it('the ratio method reports the same height and status as the catenary method', () => {
    const ratio = ratioMethod(tided)!
    expect(ratio.depthFromHawseM).toBeCloseTo(catenaryMethod(tided)!.depthFromHawseM!, 9)
    expect(ratio.tideCorrected).toBe(true)
  })

  it('a ratio method with no catenary plan still reports its height', () => {
    const noChain = { ...baseInput, chainSizeMm: 0 }
    expect(buildRodePlan(noChain)).toBeNull()
    expect(ratioMethod(noChain)!.depthFromHawseM).toBe(6)
  })

  it('an unavailable method reports no height', () => {
    expect(catenaryMethod({ ...baseInput, depth: null })!.depthFromHawseM).toBeUndefined()
    expect(ratioMethod({ ...baseInput, depth: null })!.depthFromHawseM).toBeUndefined()
  })
})

describe('the method results carry their load and lift warning', () => {
  it('both methods carry the catenary parameter of the plan', () => {
    const plan = buildRodePlan(baseInput)!
    expect(catenaryMethod(baseInput)!.catenaryParameterM).toBeCloseTo(plan.catenaryParameterM, 9)
    expect(ratioMethod(baseInput)!.catenaryParameterM).toBeCloseTo(plan.catenaryParameterM, 9)
  })

  it('the Ratio method has no parameter when there is no catenary plan, and unavailable results have none', () => {
    expect(ratioMethod({ ...baseInput, chainSizeMm: 0 })!.catenaryParameterM).toBeNull()
    expect(catenaryMethod({ ...baseInput, depth: null })!.catenaryParameterM).toBeNull()
  })

  it('the Catenary method has no lift warning, since its rode is never short of the suspended length', () => {
    expect(catenaryMethod(baseInput)!.liftWarning).toBeNull()
  })

  it('words the Ratio lift warning, and an unavailable result has none', () => {
    expect(ratioMethod(baseInput)!.liftWarning).toBe('The ratio rule is too short for the planned conditions: the anchor would be lifted off the bottom.')
    expect(ratioMethod({ ...baseInput, depth: null })!.liftWarning).toBeNull()
  })

  it('takes the plan it is given rather than building its own', () => {
    const plan = buildRodePlan(baseInput)!
    expect(ratioMethod(baseInput, { ...plan, catenaryParameterM: 123 })!.catenaryParameterM).toBe(123)
    expect(ratioMethod(baseInput, null)!.catenaryParameterM).toBeNull()
  })
})

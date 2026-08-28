import { describe, expect, it } from 'vitest'
import { calculateCatenary } from '@/lib/catenary'
import {
  buildRodePlan,
  catenaryMethod,
  computeScopeRecommendation,
  maxExpectedDepthM,
  MIN_SCOPE_RATIO,
  ratioMethod,
  resolvePlanningDepth,
  resolvePlanningWindBand,
  rodeMethods,
  scopeRatio,
  scopeStatus,
  seedPlanningWindKts,
  tideHeightFtOrNull,
  WIND_BANDS,
  windBandForKts,
  type RodePlanInput,
} from '@/lib/rode-plan'
import type { TideToday } from '@/hooks/use-tide-today'
import type { AnchorConfig } from '@/config/app-config'
import type { GustWindow } from '@/lib/gust-windows'

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
    expect(result!.recommendedRodeM).toBeCloseTo(30.062226808928283, 6)
    expect(result!.minScopeRatio).toBeCloseTo(5.010371134821381, 6)
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
    expect(result!.recommendedRodeM).toBeCloseTo(134.21760925594984, 6)
    expect(result!.minScopeRatio).toBeCloseTo(11.671096457039116, 6)
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
    expect(result!.recommendedRodeM).toBeCloseTo(19.334932895570244, 6)
    expect(result!.minScopeRatio).toBeCloseTo(5.524266541591499, 6)
    expect(result!.horizontalLoadN).toBeCloseTo(638.1218181622763, 6)
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
// buildRodePlan
// ---------------------------------------------------------------------------
describe('buildRodePlan', () => {
  it('uses the raw live depth with no tide correction', () => {
    const plan = buildRodePlan(baseInput)
    expect(plan).not.toBeNull()
    expect(plan!.tideCorrected).toBe(false)
    expect(plan!.planningDepthM).toBe(5)
    expect(plan!.depthFromHawseM).toBe(6)
    expect(plan!.recommendedRodeM).toBeCloseTo(30.062226808928283, 6)
    expect(plan!.recommendedScope).toBeCloseTo(5.010371134821381, 6)
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
    expect(plan!.chainRemainingM).toBeCloseTo(10 - 30.062226808928283, 6)
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
// raw catenary model down to ~0.6:1: mathematically correct, nautically
// absurd — below ~3:1 the anchor pulls upward regardless of what the
// catenary math says is holding. The floor is composition policy, applied
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
    expect(plan!.recommendedRodeM).toBeCloseTo(30.062226808928283, 6)
    expect(plan!.recommendedScope).toBeCloseTo(5.010371134821381, 6)
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
    expect(result!.recommendedRodeM).toBeCloseTo(30.062226808928283, 6)
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

  it('appends the floor marker to the note when the MIN_SCOPE_RATIO floor binds', () => {
    const result = catenaryMethod({ ...baseInput, windKts: 3 })
    expect(result).not.toBeNull()
    expect(result!.note).toContain(` · ${MIN_SCOPE_RATIO}:1 minimum`)
  })

  // Scope is computed against depth-from-hawse (depth + bow roller height),
  // not depth alone, so the caption has to name both terms of that sum
  // rather than just the depth half of it — sitting between the depth clause
  // and the wind clause, ahead of chain/hull/floor.
  it('names the tide-correction status between the depth and wind clauses', () => {
    const result = catenaryMethod(baseInput) // live depth 5, no tide, bowRollerHeightM 1
    expect(result).not.toBeNull()
    expect(result!.note).toContain('Depth 5.0 m (sounder only) + 1.0 m bow height · Wind 20 kts')
  })

  it('omits the floor marker when the raw catenary recommendation already clears the floor', () => {
    const result = catenaryMethod(baseInput) // windKts: 20, ~5:1 — well clear of the floor
    expect(result).not.toBeNull()
    expect(result!.note).not.toContain('minimum')
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
    expect(result!.note).toContain('tide-corrected')
  })

  it('falls back to sounder-only depth when the datum carries no tide stamp', () => {
    const result = ratioMethod({
      ...baseInput,
      windKts: 15,
      tide: makeTide({ current_tide_height_ft: -1 }),
    })
    expect(result).not.toBeNull()
    expect(result!.recommendedRodeM).toBeCloseTo((5 + 1) * 5, 6)
    expect(result!.note).toContain('sounder only')
  })

  // Same bow-height naming rule as catenaryMethod, and via the same helper:
  // the ratio scope is also computed against depth-from-hawse, not depth alone.
  it('names the tide-correction status between the depth and wind clauses', () => {
    const result = ratioMethod({ ...baseInput, windKts: 15 })
    expect(result).not.toBeNull()
    expect(result!.note).toContain('Depth 5.0 m (sounder only) + 1.0 m bow height · Wind 15 kts')
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

// ---------------------------------------------------------------------------
// computeScopeRecommendation — the single recommendation the map overlay's
// Scope row renders (ADR 0059 §3), shared by the tile and drawer hosts.
// ---------------------------------------------------------------------------
describe('computeScopeRecommendation', () => {
  const baseAnchorConfig: AnchorConfig = {
    bowRollerHeightM: 1,
    chainSizeMm: 10,
    chainOnboardM: 50,
    hullType: 'power_mono',
    scopeMethod: 'ratio',
    windageAreaM2: 20,
    gpsFromBowM: 0,
    loaM: 0,
  }

  const baseArgs = {
    isAnchored: false,
    liveDepthM: 5,
    planningDepthM: null as number | null,
    planningTideHeightFt: null as number | null,
    tide: null,
    maxGustKts: { '10m': null, '30m': null, '1h': null, '24h': null } as Record<GustWindow, number | null>,
    windSpeedApparentKts: 15,
    seaState: 'calm' as const,
    seabedType: 'sand' as const,
    anchorConfig: baseAnchorConfig,
    // No operator override in the base case — every case below sets this
    // explicitly (the field is required, not defaulted; see
    // resolvePlanningWindBand below for why a silent default is the trap).
    selectedWindBandId: null as string | null,
  }

  it('picks ratioMethod when anchorConfig.scopeMethod is "ratio"', () => {
    const result = computeScopeRecommendation(baseArgs)
    expect(result.id).toBe('ratio')
    // The 15kt apparent seed lands in the 15-20 band, which plans at its top
    // (20kt) — right at the ratio method's >=20kt threshold, so 7:1, not the
    // 5:1 a raw 15kt reading would have given pre-banding.
    expect(result.recommendedRodeM).toBeCloseTo(42, 6) // (5 + 1) * 7
    expect(result.scopeRatio).toBe(7)
  })

  it('picks catenaryMethod when anchorConfig.scopeMethod is "catenary"', () => {
    const result = computeScopeRecommendation({
      ...baseArgs,
      anchorConfig: { ...baseAnchorConfig, scopeMethod: 'catenary' },
    })
    expect(result.id).toBe('catenary')
    // Matches the case-1 characterization above (5m/20kt would give a
    // different figure) — this uses the 15-20 band's planKts (20), the band
    // the seeded 15kt apparent wind falls in, not the raw 15kt reading.
    const expected = catenaryMethod({
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
    })
    expect(result.recommendedRodeM).toBeCloseTo(expected!.recommendedRodeM, 6)
  })

  it('seeds wind from the 1h max gust ahead of apparent wind, same as the Rode Planner', () => {
    const result = computeScopeRecommendation({
      ...baseArgs,
      maxGustKts: { '10m': null, '30m': null, '1h': 25, '24h': null },
      windSpeedApparentKts: 10,
    })
    // The seeded 25kt gust clears the ratio method's 20kt threshold even
    // though the apparent wind alone (10kt) would not have.
    expect(result.scopeRatio).toBe(7)
  })

  it('falls back to apparent wind when no 1h gust is recorded', () => {
    const result = computeScopeRecommendation({
      ...baseArgs,
      maxGustKts: { '10m': null, '30m': null, '1h': null, '24h': null },
      windSpeedApparentKts: 25,
    })
    expect(result.scopeRatio).toBe(7)
  })

  it('passes through the unavailableReason when depth is missing, rather than throwing or masking it', () => {
    const result = computeScopeRecommendation({ ...baseArgs, liveDepthM: null })
    expect(result.unavailableReason).toBe('no depth reading')
  })

  it('names "no depth entered" when anchored with nothing recorded', () => {
    const result = computeScopeRecommendation({ ...baseArgs, isAnchored: true, liveDepthM: 99 })
    expect(result.unavailableReason).toBe('no depth entered')
  })

  it('plans off the recorded planning depth, not the live sounder, once anchored', () => {
    const result = computeScopeRecommendation({
      ...baseArgs,
      isAnchored: true,
      liveDepthM: 99,
      planningDepthM: 5,
      planningTideHeightFt: null,
    })
    // Same 5m depth as the not-anchored baseline case above, proving the
    // 99m live reading was ignored once a watch is active.
    expect(result.recommendedRodeM).toBeCloseTo(42, 6)
  })

  it('passes through the unavailableReason when neither gust nor apparent wind is available', () => {
    const result = computeScopeRecommendation({
      ...baseArgs,
      maxGustKts: { '10m': null, '30m': null, '1h': null, '24h': null },
      windSpeedApparentKts: null,
    })
    expect(result.unavailableReason).toBe('no wind data')
  })

  // The Rode Planner plans in bands, not raw continuous readings — this is
  // the guarantee that the map's Scope row now agrees with it (previously
  // this function seeded the raw gust straight through, so the two views of
  // the same night could recommend different rode).
  it('plans at the band\'s planKts rather than the raw seed, with no explicit selection', () => {
    const result = computeScopeRecommendation({
      ...baseArgs,
      anchorConfig: { ...baseAnchorConfig, scopeMethod: 'catenary' },
      maxGustKts: { '10m': null, '30m': null, '1h': 12.3, '24h': null },
    })
    // 12.3kt falls in the 10-15 band, which plans at its top (15kt) — not
    // the raw 12.3kt reading. Catenary's load term is v², so the two would
    // produce visibly different figures if the raw seed leaked through.
    const inputAtBandTop: RodePlanInput = {
      depth: { depthM: 5, tideHeightFt: null },
      isAnchored: false,
      bowRollerHeightM: 1,
      tide: null,
      windKts: 15,
      seaState: 'calm',
      seabedType: 'sand',
      chainSizeMm: 10,
      chainOnboardM: 50,
      windageAreaM2: 20,
      hullType: 'power_mono',
    }
    const atBandTop = catenaryMethod(inputAtBandTop)
    const atRawSeed = catenaryMethod({ ...inputAtBandTop, windKts: 12.3 })
    expect(result.recommendedRodeM).toBeCloseTo(atBandTop!.recommendedRodeM, 6)
    expect(result.recommendedRodeM).not.toBeCloseTo(atRawSeed!.recommendedRodeM, 1)
  })

  it('lets an explicit band id override a calm seed', () => {
    const result = computeScopeRecommendation({
      ...baseArgs,
      maxGustKts: { '10m': null, '30m': null, '1h': 3, '24h': null }, // calm — seeds the 0-10 band alone
      selectedWindBandId: '20-25',
    })
    // The explicit 20-25 selection plans at 25kt regardless of the calm 3kt
    // seed: (5 + 1) * 7 = 42, ratio 7:1, at/over the ratio method's threshold.
    expect(result.recommendedRodeM).toBeCloseTo(42, 6)
    expect(result.scopeRatio).toBe(7)
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
// resolvePlanningWindBand — the single band-selection rule the Rode Planner's
// dropdown and computeScopeRecommendation both defer to, so an operator's
// pick in one view reaches the other instead of each deriving its own band.
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

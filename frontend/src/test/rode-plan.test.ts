import { describe, expect, it } from 'vitest'
import { calculateCatenary } from '@/lib/catenary'
import {
  buildRodePlan,
  catenaryMethod,
  maxExpectedDepthM,
  scopeRatio,
  scopeStatus,
  type RodePlanInput,
} from '@/lib/rode-plan'
import type { TideToday } from '@/hooks/use-tide-today'

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

const baseInput: RodePlanInput = {
  sounderDepthM: 5,
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
// maxExpectedDepthM
// ---------------------------------------------------------------------------
describe('maxExpectedDepthM', () => {
  it('adds the rise to the next high on a rising tide', () => {
    // current 2ft, high 5ft -> rise 3ft -> 3/3.28084 = 0.914...m
    const result = maxExpectedDepthM(5, makeTide({ current_tide_height_ft: 2, high_tide_height_ft: 5 }))
    expect(result).toBeCloseTo(5 + 3 / 3.28084, 6)
  })

  it('clamps a falling tide so planning depth never drops below the sounder reading', () => {
    // current 4ft is already above high 3ft (falling past the high) -> rise would
    // be negative; must clamp to 0, not subtract.
    const result = maxExpectedDepthM(5, makeTide({ current_tide_height_ft: 4, high_tide_height_ft: 3 }))
    expect(result).toBe(5)
  })

  it('returns null when depth is unavailable', () => {
    expect(maxExpectedDepthM(null, makeTide())).toBeNull()
  })

  it('returns null when tide is unavailable', () => {
    expect(maxExpectedDepthM(5, null)).toBeNull()
  })

  it('returns null when the tide station has no reading (sentinel -1)', () => {
    expect(maxExpectedDepthM(5, makeTide({ current_tide_height_ft: -1 }))).toBeNull()
    expect(maxExpectedDepthM(5, makeTide({ high_tide_height_ft: -1 }))).toBeNull()
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
})

// ---------------------------------------------------------------------------
// buildRodePlan
// ---------------------------------------------------------------------------
describe('buildRodePlan', () => {
  it('uses the raw sounder depth and reports depthSource "sounder" with no tide', () => {
    const plan = buildRodePlan(baseInput)
    expect(plan).not.toBeNull()
    expect(plan!.depthSource).toBe('sounder')
    expect(plan!.planningDepthM).toBe(5)
    expect(plan!.depthFromHawseM).toBe(6)
    expect(plan!.recommendedRodeM).toBeCloseTo(30.062226808928283, 6)
    expect(plan!.recommendedScope).toBeCloseTo(5.010371134821381, 6)
  })

  it('uses the tide-corrected depth and reports depthSource "tide" when tide data is available', () => {
    const plan = buildRodePlan({
      ...baseInput,
      tide: makeTide({ current_tide_height_ft: 2, high_tide_height_ft: 5 }),
    })
    expect(plan).not.toBeNull()
    expect(plan!.depthSource).toBe('tide')
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
    expect(buildRodePlan({ ...baseInput, sounderDepthM: null })).toBeNull()
  })

  it('returns null when wind is unavailable', () => {
    expect(buildRodePlan({ ...baseInput, windKts: null })).toBeNull()
  })

  it('returns null when the catenary calc itself is unavailable (e.g. no windage configured)', () => {
    expect(buildRodePlan({ ...baseInput, windageAreaM2: 0 })).toBeNull()
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
    const result = catenaryMethod({ ...baseInput, sounderDepthM: null })
    expect(result).not.toBeNull()
    expect(result!.unavailableReason).toBeTruthy()
    expect(typeof result!.unavailableReason).toBe('string')
  })

  it('names a different reason when wind is missing than when depth is missing', () => {
    const noDepth = catenaryMethod({ ...baseInput, sounderDepthM: null })
    const noWind = catenaryMethod({ ...baseInput, windKts: null })
    expect(noDepth!.unavailableReason).not.toBe(noWind!.unavailableReason)
  })
})

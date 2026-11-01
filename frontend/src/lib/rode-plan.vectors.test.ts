import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, it } from 'vitest'
import { computeSwingRadiusM, ratioMethod, resolveLoaM, windBandForKts, type RodePlanInput } from '@/lib/rode-plan'

/**
 * The ratio method and the swing maths are also written in Go
 * (backend/anchorage_rode.go), so that Mate can plan the rode for an anchorage
 * the boat has not reached. Both suites run these vectors; this side is the
 * reference, so a failure here means the reference changed and the file is
 * stale, and a failure in the Go suite means the port drifted. Expected values
 * are the TypeScript's own output. The catenary method is not ported: it needs
 * the sea state and the seabed, which a stay that has not begun does not have.
 */
interface MethodOut { recommendedRodeM: number; scopeRatio: number; unavailableReason: string | null }
interface VectorFile {
  tolerance: number
  cases: Array<{
    name: string
    input: {
      planningDepthM: number
      bowRollerHeightM: number
      windKts: number | null
      loaM: number | null
      bowOffsetM: number
    }
    expected: {
      ratio: MethodOut
      ratioSwingRadiusM: number | null
    }
  }>
  windBands: Array<{ kts: number | null; planKts: number | null }>
  loa: Array<{ settingsLoaM: number; vesselLengthOverallM: number | null; loaM: number | null }>
}

const vectors: VectorFile = JSON.parse(readFileSync(join(__dirname, '../../../backend/testdata/rode_plan_vectors.json'), 'utf8'))

/** The ratio method reads only depth, wind and bow roller height; the rest is filler the type asks for. */
function planInput(c: VectorFile['cases'][number]['input']): RodePlanInput {
  return {
    depth: { depthM: c.planningDepthM, tideHeightFt: null },
    isAnchored: false,
    bowRollerHeightM: c.bowRollerHeightM,
    tide: null,
    windKts: c.windKts,
    seaState: 'calm',
    seabedType: 'sand',
    chainSizeMm: 10,
    chainOnboardM: 60,
    windageAreaM2: 30,
    hullType: 'power_cat',
  }
}

describe('rode plan vectors (shared with the Go port)', () => {
  it('has the cases', () => {
    expect(vectors.cases.length).toBeGreaterThanOrEqual(8)
  })

  it.each(vectors.cases.map((c) => [c.name, c] as const))('%s', (_name, c) => {
    const r = ratioMethod(planInput(c.input))!
    const ratio: MethodOut = { recommendedRodeM: r.recommendedRodeM, scopeRatio: r.scopeRatio, unavailableReason: r.unavailableReason ?? null }
    expect(ratio).toEqual(c.expected.ratio)
    expect(ratio.unavailableReason ? null : computeSwingRadiusM(ratio.recommendedRodeM, c.input.bowOffsetM, c.input.loaM)).toEqual(c.expected.ratioSwingRadiusM)
  })

  it('plans the wind at the top of its band', () => {
    for (const b of vectors.windBands) {
      expect(windBandForKts(b.kts)?.planKts ?? null).toBe(b.planKts)
    }
  })

  it('resolves the boat length', () => {
    for (const l of vectors.loa) {
      expect(resolveLoaM(l.settingsLoaM, l.vesselLengthOverallM).loaM).toBe(l.loaM)
    }
  })
})

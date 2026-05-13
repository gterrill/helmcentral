import type { HullType } from '@/config/app-config'

export type SeaState = 'calm' | 'choppy' | 'rough' | 'storm'
export type SeabedType = 'sand' | 'mud' | 'rock' | 'grass'

export interface CatenaryInput {
  depthM: number
  bowRollerHeightM: number
  windKts: number
  seaState: SeaState
  seabedType: SeabedType
  chainSizeMm: number
  windageAreaM2: number
  hullType: HullType
}

export interface CatenaryResult {
  recommendedRodeM: number
  minScopeRatio: number
  horizontalLoadN: number
}

const AIR_DENSITY_KG_M3 = 1.225
const YAW_BUFFER = 1.2
const SUBMERGED_CHAIN_FACTOR = 0.87
const GRAVITY = 9.81

const CHAIN_DRY_WEIGHT_KG_PER_M: Record<number, number> = {
  6: 0.8,
  8: 1.4,
  10: 2.3,
  12: 3.3,
  14: 4.4,
}

const SEA_STATE_MULTIPLIER: Record<SeaState, number> = {
  calm: 1.0,
  choppy: 1.2,
  rough: 1.5,
  storm: 2.0,
}

const SEABED_MULTIPLIER: Record<SeabedType, number> = {
  sand: 1.0,
  mud: 1.15,
  rock: 0.9,
  grass: 1.1,
}

const HULL_CD: Record<HullType, number> = {
  power_cat: 1.15,
  sail_mono: 0.9,
  power_mono: 0.95,
  sail_cat: 1.05,
}

function interpolateChainDryWeightKgPerM(chainSizeMm: number): number {
  const sortedSizes = Object.keys(CHAIN_DRY_WEIGHT_KG_PER_M)
    .map(Number)
    .sort((a, b) => a - b)

  if (chainSizeMm <= sortedSizes[0]) {
    return CHAIN_DRY_WEIGHT_KG_PER_M[sortedSizes[0]]
  }
  if (chainSizeMm >= sortedSizes[sortedSizes.length - 1]) {
    return CHAIN_DRY_WEIGHT_KG_PER_M[sortedSizes[sortedSizes.length - 1]]
  }

  for (let i = 0; i < sortedSizes.length - 1; i += 1) {
    const lowSize = sortedSizes[i]
    const highSize = sortedSizes[i + 1]
    if (chainSizeMm >= lowSize && chainSizeMm <= highSize) {
      const lowWeight = CHAIN_DRY_WEIGHT_KG_PER_M[lowSize]
      const highWeight = CHAIN_DRY_WEIGHT_KG_PER_M[highSize]
      const ratio = (chainSizeMm - lowSize) / (highSize - lowSize)
      return lowWeight + ratio * (highWeight - lowWeight)
    }
  }

  return CHAIN_DRY_WEIGHT_KG_PER_M[12]
}

export function calculateCatenary(input: CatenaryInput): CatenaryResult | null {
  if (
    input.depthM <= 0
    || input.bowRollerHeightM <= 0
    || input.windKts < 0
    || input.chainSizeMm <= 0
    || input.windageAreaM2 <= 0
  ) {
    return null
  }

  const windMs = input.windKts * 0.514444
  const dragCoefficient = HULL_CD[input.hullType]

  const windForceN = 0.5 * AIR_DENSITY_KG_M3 * windMs * windMs * input.windageAreaM2 * dragCoefficient
  const horizontalLoadN = windForceN
    * SEA_STATE_MULTIPLIER[input.seaState]
    * SEABED_MULTIPLIER[input.seabedType]
    * YAW_BUFFER

  const dryWeightKgPerM = interpolateChainDryWeightKgPerM(input.chainSizeMm)
  const submergedWeightNPerM = dryWeightKgPerM * SUBMERGED_CHAIN_FACTOR * GRAVITY

  if (submergedWeightNPerM <= 0 || horizontalLoadN <= 0) {
    return null
  }

  const depthFromHawseM = input.depthM + input.bowRollerHeightM
  if (depthFromHawseM <= 0) {
    return null
  }

  const recommendedRodeM = Math.sqrt((2 * horizontalLoadN * depthFromHawseM) / submergedWeightNPerM)
  const minScopeRatio = recommendedRodeM / depthFromHawseM

  if (!Number.isFinite(recommendedRodeM) || !Number.isFinite(minScopeRatio)) {
    return null
  }

  return {
    recommendedRodeM,
    minScopeRatio,
    horizontalLoadN,
  }
}

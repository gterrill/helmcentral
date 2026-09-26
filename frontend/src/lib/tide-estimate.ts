import type { TideToday } from '@/hooks/use-tide-today'

const METERS_PER_FOOT = 3.28084

export interface TideExtreme {
  isHigh: boolean
  time: string
  heightFt: number
}

/**
 * Today's two tide extremes (high and low), sorted chronologically.
 * useTideToday's high/low fields are already each the next high and the
 * next low from the provider, so whichever sorts first is simply the next
 * turn — depth-tide-tile.tsx and the Anchor Watch header both need this
 * same ordering, extracted here so it lives in exactly one place.
 */
export function tideExtremesByTime(tide: TideToday): [TideExtreme, TideExtreme] {
  const extremes: [TideExtreme, TideExtreme] = [
    { isHigh: true, time: tide.high_tide_time, heightFt: tide.high_tide_height_ft },
    { isHigh: false, time: tide.low_tide_time, heightFt: tide.low_tide_height_ft },
  ]
  return extremes.sort((a, b) => new Date(a.time).getTime() - new Date(b.time).getTime()) as [TideExtreme, TideExtreme]
}

/** The sooner of the two extremes — the next high or low, whichever comes first. */
export function nextTideExtreme(tide: TideToday): TideExtreme {
  return tideExtremesByTime(tide)[0]
}

/**
 * Depth projected forward to the next turn: the next high if the tide is
 * rising, the next low if falling. Extracted from depth-tide-tile.tsx, which
 * used this arithmetic inline before the Anchor Watch header (ADR 0133's
 * amendment) needed the same estimate.
 *
 * Requires a live depth reading and both the current and target tide
 * heights to be real (not the -1 sentinel useTideToday substitutes for "not
 * published"); returns null rather than a silent zero-based estimate
 * otherwise.
 */
export function estimateDepthAtNextTurn(depthM: number | null, tide: TideToday): number | null {
  const isRising = tide.tide_direction === 'Rising'
  if (isRising) {
    return depthM !== null && tide.current_tide_height_ft >= 0 && tide.high_tide_height_ft >= 0
      ? depthM + (tide.high_tide_height_ft - tide.current_tide_height_ft) / METERS_PER_FOOT
      : null
  }
  return depthM !== null && tide.current_tide_height_ft >= 0 && tide.low_tide_height_ft >= 0
    ? depthM - (tide.current_tide_height_ft - tide.low_tide_height_ft) / METERS_PER_FOOT
    : null
}

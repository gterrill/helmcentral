import type { TideToday } from '@/hooks/use-tide-today'
import { feetToMeters } from '@/lib/units'

export interface TideExtreme {
  isHigh: boolean
  time: string
  heightFt: number
}

/**
 * Whether a tide height in feet is real data rather than the -1 sentinel
 * useTideToday substitutes for "not published" - the sentinel is exactly
 * -1, not "-1 or below" (matches low-water-clearance.ts's own
 * isUsableTideHeightFt; a real spring low can read below -1 ft on some
 * datums and must not be mistaken for missing data just for being very
 * negative).
 */
function isUsableTideHeightFt(value: number): boolean {
  return Number.isFinite(value) && value !== -1
}

/** Whether a tide extreme's own time string is a real, parseable instant - tideToday sends '' for one it has no data for (backend/weather_tide.go). */
function hasUsableTideTime(time: string): boolean {
  return time !== '' && !Number.isNaN(new Date(time).getTime())
}

/**
 * Today's tide extremes (high and low, when each is real), sorted
 * chronologically. useTideToday's high/low fields are already each the next
 * high and the next low from the provider, so whichever sorts first is
 * simply the next turn — depth-tide-tile.tsx and the Anchor Watch header
 * both need this same ordering, extracted here so it lives in exactly one
 * place.
 *
 * An extreme the provider has none of (tideToday sends the -1 height
 * sentinel alongside an empty time) is dropped rather than passed through -
 * code-review finding: the old fabricated time ("now" for a missing high,
 * "now+24h" for a missing low) used to reach this list and render as if it
 * were a real extreme. The result can be length 0, 1 or 2 depending on how
 * many of the two are real; callers render however many they get, not a
 * fixed pair.
 */
export function tideExtremesByTime(tide: TideToday): TideExtreme[] {
  const extremes: TideExtreme[] = [
    { isHigh: true, time: tide.high_tide_time, heightFt: tide.high_tide_height_ft },
    { isHigh: false, time: tide.low_tide_time, heightFt: tide.low_tide_height_ft },
  ].filter((extreme) => isUsableTideHeightFt(extreme.heightFt) && hasUsableTideTime(extreme.time))
  return extremes.sort((a, b) => new Date(a.time).getTime() - new Date(b.time).getTime())
}

/**
 * The sooner of the two extremes — the next high or low, whichever comes
 * first — or null when neither is real (tide station has nothing upcoming
 * of either kind).
 */
export function nextTideExtreme(tide: TideToday): TideExtreme | null {
  return tideExtremesByTime(tide)[0] ?? null
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
    return depthM !== null && isUsableTideHeightFt(tide.current_tide_height_ft) && isUsableTideHeightFt(tide.high_tide_height_ft)
      ? depthM + feetToMeters(tide.high_tide_height_ft - tide.current_tide_height_ft)
      : null
  }
  return depthM !== null && isUsableTideHeightFt(tide.current_tide_height_ft) && isUsableTideHeightFt(tide.low_tide_height_ft)
    ? depthM - feetToMeters(tide.current_tide_height_ft - tide.low_tide_height_ft)
    : null
}

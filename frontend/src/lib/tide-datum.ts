import { isUsableTideHeightFt } from '@/lib/tide-estimate'

// What a tide height is measured from. A station-based provider (BOM, NOAA)
// predicts on its chart's datum. The default provider models sea level from
// mean sea level, so its heights run negative all the time and are not a depth
// correction. The server says which on every tide payload (height_datum).

export const CHART_DATUM = 'chart_datum'
export const MEAN_SEA_LEVEL = 'mean_sea_level'
export type TideDatum = typeof CHART_DATUM | typeof MEAN_SEA_LEVEL

/** The tide provider a blank setting means (backend defaultTideProviderID). */
export const DEFAULT_TIDE_PROVIDER = 'open-meteo'

/** The provider in force: the operator's choice, or the default when none is saved. */
export function effectiveTideProvider(configured: string): string {
  return configured.trim() === '' ? DEFAULT_TIDE_PROVIDER : configured.trim()
}

/** An absent or unknown datum is chart datum, as for every payload sent before the field existed. */
export function normaliseTideDatum(raw: unknown): TideDatum {
  return raw === MEAN_SEA_LEVEL ? MEAN_SEA_LEVEL : CHART_DATUM
}

export function isMeanSeaLevel(raw: unknown): boolean {
  return raw === MEAN_SEA_LEVEL
}

/** Said wherever a height from mean sea level is shown. */
export const MEAN_SEA_LEVEL_LABEL = 'from mean sea level'

/** Why no depth at chart datum can come from such a height. */
export const MEAN_SEA_LEVEL_NOTE =
  'Heights are from mean sea level, not chart datum, so they are often negative and are not a depth.'

/**
 * Whether a stored or forecast tide height in feet is a reading. -1 is "none"
 * in every payload; on chart datum anything below zero is none too, from mean
 * sea level a negative height is real.
 */
export function tideHeightFtKnown(ft: number, datum: unknown): boolean {
  return isUsableTideHeightFt(ft) && (isMeanSeaLevel(datum) || ft >= 0)
}

/** A tide height in feet and the datum it is measured from: one value, so one is never passed without the other. */
export interface TideCapture {
  ft: number
  datum: TideDatum
}

/**
 * Whether the current height in a tide payload is a reading. The payload's
 * -1 ft means "none yet": on chart datum a height below zero is also none (the
 * tile has always drawn a dash for it); from mean sea level a negative height
 * is real, and the server never sends a real one as exactly -1.
 */
export function currentTideHeightKnown(tide: { current_tide_height_ft: number; height_datum?: string }): boolean {
  return tideHeightFtKnown(tide.current_tide_height_ft, tide.height_datum)
}

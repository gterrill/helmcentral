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

/** A tide height in feet and the datum it is measured from: one value, so one is never passed without the other. */
export interface TideCapture {
  ft: number
  datum: TideDatum
}

/**
 * Overnight state-of-charge projection: the "will the bank hold until
 * morning" figure on the Battery & Power tile's STATE card footer.
 *
 * The backend (GET /api/electrical/overnight) reports the next sunset and
 * sunrise plus, when it has enough recent history, a median overnight
 * discharge rate. When history is unavailable it says so with a reason and
 * leaves the tile to fall back to a live-rate extrapolation, which is
 * accurate at night but overstates the afternoon since it does not know the
 * bank will keep charging until the sun goes down. See "Decisions taken in
 * this plan", item 5, in the battery-power-tile-pfd-fixes plan.
 */

export type DawnBasis = 'history' | 'linear' | 'none'

export interface OvernightProjection {
  socPath: string
  sunset: Date
  sunrise: Date
  basis: DawnBasis
  nightRatePercentPerHour: number | null
  nightsUsed: number
  nightsConsidered: number
  /** Size of the backend's rolling lookback window, in nights (e.g. 30). */
  lookbackNights: number
  /** Nights within the lookback window dropped because shore power was on. */
  nightsExcludedShore: number
  /** Nights within the lookback window dropped because the generator ran. */
  nightsExcludedGenerator: number
  reason: string | null
}

function clamp(value: number, min: number, max: number): number {
  return Math.min(max, Math.max(min, value))
}

function isValidDate(date: Date): boolean {
  return !Number.isNaN(date.getTime())
}

/**
 * Strict parse of the /api/electrical/overnight response body. Returns null
 * rather than a guessed object the moment any field doesn't hold the shape
 * the tile depends on, per the fallback policy: an unparsable payload is a
 * missing projection, never an invented one.
 */
export function parseOvernightProjection(payload: unknown): OvernightProjection | null {
  if (typeof payload !== 'object' || payload === null || Array.isArray(payload)) {
    return null
  }

  const body = payload as Record<string, unknown>

  if (typeof body.soc_path !== 'string') {
    return null
  }

  if (typeof body.sunset !== 'string' || typeof body.sunrise !== 'string') {
    return null
  }
  const sunset = new Date(body.sunset)
  const sunrise = new Date(body.sunrise)
  if (!isValidDate(sunset) || !isValidDate(sunrise)) {
    return null
  }

  if (body.basis !== 'history' && body.basis !== 'linear' && body.basis !== 'none') {
    return null
  }

  const nightRate = body.night_rate_percent_per_hour
  if (typeof nightRate !== 'number' && nightRate !== null) {
    return null
  }

  if (typeof body.nights_used !== 'number' || typeof body.nights_considered !== 'number') {
    return null
  }

  if (typeof body.reason !== 'string' && body.reason !== null) {
    return null
  }

  // These three are counts an older backend simply doesn't report yet, not
  // readings it failed to take, so an absent or malformed value means zero
  // rather than an unparsable payload.
  const lookbackNights = typeof body.lookback_nights === 'number' ? body.lookback_nights : 0
  const nightsExcludedShore = typeof body.nights_excluded_shore === 'number' ? body.nights_excluded_shore : 0
  const nightsExcludedGenerator = typeof body.nights_excluded_generator === 'number' ? body.nights_excluded_generator : 0

  return {
    socPath: body.soc_path,
    sunset,
    sunrise,
    basis: body.basis,
    nightRatePercentPerHour: nightRate,
    nightsUsed: body.nights_used,
    nightsConsidered: body.nights_considered,
    lookbackNights,
    nightsExcludedShore,
    nightsExcludedGenerator,
    reason: body.reason,
  }
}

export interface DawnEstimate {
  socPercent: number
  basis: 'history' | 'linear'
}

/**
 * Projects the state of charge at the next sunrise from the current reading
 * and rate. Pure and timezone-free: every time input is an instant, and
 * "before sunset" is read off which of the two next events (sunset,
 * sunrise) comes first rather than off the wall clock.
 *
 * - No projection, a `none` basis, or an unknown current SoC: no estimate.
 * - Daytime with a history basis and a known night rate: the live rate
 *   carries the bank to sunset, then the night rate carries it to sunrise.
 *   Both legs are clamped independently so a bank already full at sunset
 *   doesn't get pushed over 100 a second time by the night leg.
 * - Daytime otherwise (a linear basis, or a history basis with no night
 *   rate yet, which should not happen but must degrade rather than throw):
 *   the live rate is held all the way to sunrise.
 * - Night (sunrise is the next event, before the next sunset): the live
 *   rate is tonight's rate, so it's held to sunrise regardless of what
 *   basis the backend reported, and the result is always labelled `linear`
 *   because that is what it is.
 * - A null live rate means the bank isn't currently charging or
 *   discharging, so it contributes 0 rather than being treated as unknown.
 */
export function projectSocAtDawn(input: {
  socPercent: number | null
  liveRatePercentPerHour: number | null
  projection: OvernightProjection | null
  now: Date
}): DawnEstimate | null {
  const { socPercent, projection, now } = input
  if (projection === null || projection.basis === 'none' || socPercent === null) {
    return null
  }

  const liveRate = input.liveRatePercentPerHour ?? 0
  const { sunset, sunrise } = projection
  const beforeSunset = sunset.getTime() < sunrise.getTime()

  if (!beforeSunset) {
    const hoursToSunrise = (sunrise.getTime() - now.getTime()) / 3_600_000
    return { socPercent: clamp(socPercent + liveRate * hoursToSunrise, 0, 100), basis: 'linear' }
  }

  if (projection.basis === 'history' && projection.nightRatePercentPerHour !== null) {
    const hoursToSunset = (sunset.getTime() - now.getTime()) / 3_600_000
    const hoursSunsetToSunrise = (sunrise.getTime() - sunset.getTime()) / 3_600_000
    const socAtSunset = clamp(socPercent + liveRate * hoursToSunset, 0, 100)
    const atDawn = clamp(socAtSunset + projection.nightRatePercentPerHour * hoursSunsetToSunrise, 0, 100)
    return { socPercent: atDawn, basis: 'history' }
  }

  const hoursToSunrise = (sunrise.getTime() - now.getTime()) / 3_600_000
  return { socPercent: clamp(socPercent + liveRate * hoursToSunrise, 0, 100), basis: 'linear' }
}

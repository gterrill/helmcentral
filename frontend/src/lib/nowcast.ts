import type { WeatherForecastDay } from '@/hooks/use-weather-forecast'
import { nextRain } from '@/lib/forecast-bands'

/**
 * The next-60-minute rain nowcast (ADR 0126). Mirrors HelmCast's
 * precipitation-chart.js behaviour: a small strip of intensity bars over the
 * next hour, plus a status line that falls back through hourly then daily
 * data when the provider has no nowcast for this position, or the nowcast
 * itself is dry. See backend/weather_providers.go's top doc comment for the
 * `next_hour` wire contract this consumes.
 */

/** One point of the provider's nowcast, already parsed out of the API response. */
export interface NowcastPoint {
  time: Date
  /** null when the provider didn't supply a chance for this point - distinct from a real 0%. */
  chancePct: number | null
  mmPerH: number
}

/**
 * A drawable bar: a point clipped into the [0, windowMinutes) display
 * window relative to "now". offsetMinutes/widthMinutes are both in minutes,
 * already clamped so a bar never starts before 0 or extends past the window
 * end - see buildNowcastBars.
 */
export interface NowcastBar {
  offsetMinutes: number
  widthMinutes: number
  chancePct: number | null
  mmPerH: number
}

export const NOWCAST_WINDOW_MINUTES = 60

/**
 * Builds the drawable bar list for the next `windowMinutes` from `now`.
 *
 * Each point covers [point.time, point.time + stepMinutes). A point whose
 * coverage ends before `now` is dropped entirely (stale data - see this
 * file's top doc comment and AGENTS.md's fallback policy: a cached nowcast
 * must never be trusted against its own fetch time, only against the
 * viewer's current clock). A point straddling `now` - a step interval that
 * started before now but hasn't ended yet - is clipped to start at offset 0
 * rather than dropped, so a bucket that is 5 minutes into a 15-minute
 * interval still shows as a 10-minute bar starting "now", not as either a
 * dropped bar or one that starts before the "Now" tick. The final bar is
 * clipped to end at `windowMinutes` for the same reason.
 */
export function buildNowcastBars(
  points: NowcastPoint[],
  now: Date,
  stepMinutes: number,
  windowMinutes: number = NOWCAST_WINDOW_MINUTES,
): NowcastBar[] {
  const nowMs = now.getTime()
  const bars: NowcastBar[] = []

  for (const p of points) {
    const startOffset = (p.time.getTime() - nowMs) / 60000
    const endOffset = startOffset + stepMinutes
    if (endOffset <= 0) continue // fully in the past relative to "now"
    if (startOffset >= windowMinutes) continue // fully beyond the display window

    const clippedStart = Math.max(0, startOffset)
    const clippedEnd = Math.min(windowMinutes, endOffset)
    if (clippedEnd <= clippedStart) continue

    bars.push({
      offsetMinutes: clippedStart,
      widthMinutes: clippedEnd - clippedStart,
      chancePct: p.chancePct,
      mmPerH: p.mmPerH,
    })
  }

  return bars
}

/**
 * Whether any bar carries a rain signal: a positive chance, OR positive
 * intensity when chance wasn't supplied at all. A null chance is neither
 * "dry" nor "raining" on its own - it only counts alongside real intensity,
 * mirroring HelmCast's separate chance/intensity arrays
 * (`hasNextHourChance`/`hasVisibleMinutelyRain` in precipitation-chart.js).
 */
export function hasNowcastRain(bars: NowcastBar[]): boolean {
  return bars.some((b) => (b.chancePct !== null && b.chancePct > 0) || b.mmPerH > 0)
}

export type NowcastIntensity = 'light' | 'moderate' | 'heavy'

/**
 * Standard meteorological mm/h bands (UK Met Office / WMO convention, the
 * same one HelmCast's getRainIntensityLabel used): light rain < 2.5mm/h,
 * moderate 2.5-7.6mm/h, heavy above 7.6mm/h.
 */
export function nowcastIntensityLabel(mmPerH: number): NowcastIntensity {
  if (mmPerH < 2.5) return 'light'
  if (mmPerH < 7.6) return 'moderate'
  return 'heavy'
}

/** Chance-only intensity bands, for the hourly fallback tier where no mm/h figure exists - mirrors HelmCast's getRainIntensityLabelFromSignals fallback branch. */
function hourlyChanceIntensityLabel(chancePct: number): NowcastIntensity {
  if (chancePct < 30) return 'light'
  if (chancePct < 70) return 'moderate'
  return 'heavy'
}

function capitalize(s: string): string {
  return s.length > 0 ? s[0].toUpperCase() + s.slice(1) : s
}

/**
 * The daily-tier fallback: the first upcoming day (tomorrow onward - today
 * is already covered by the hourly tier) with a genuine, non-null
 * precipitation chance above 0. 'none' only when every day checked had real
 * (non-null) data and all of it read 0 - never inferred from a day with no
 * data at all. Helmcentral's forecast contract has no per-day mm amount
 * (unlike HelmCast's daily.precipitation_amount), so this reports the
 * day's chance-of-precipitation percentage instead of inventing an amount
 * the provider never gave - see this file's ADR for why.
 */
function checkDailyRain(forecastDays: WeatherForecastDay[]): { kind: 'found' | 'none' | 'unknown'; dayLabel?: string; chancePct?: number } {
  const upcoming = forecastDays.slice(1, 7) // tomorrow through day 6
  if (upcoming.length === 0) return { kind: 'unknown' }

  const withData = upcoming.filter((d) => d.precipitation !== null)
  if (withData.length === 0) return { kind: 'unknown' }

  const rainy = withData.find((d) => (d.precipitation as number) > 0)
  if (rainy) {
    return { kind: 'found', dayLabel: rainy.dayName, chancePct: rainy.precipitation as number }
  }

  // Only a genuine "no rain" claim when every one of the checked days had
  // real data - a day with no data at all must never read as "dry".
  return withData.length === upcoming.length ? { kind: 'none' } : { kind: 'unknown' }
}

/**
 * The hourly/daily fallback cascade used once the nowcast itself has
 * nothing to say (absent or fully dry/stale) - HelmCast's
 * getNextRainExpectationFromHourly / getNextDailyRainExpectation /
 * hasNoDailyRainWithinDays(6), reusing `nextRain` (forecast-bands.ts) for
 * the hourly tier rather than a second hourly rain-finder. threshold 1
 * (not nextRain's 40% default) matches HelmCast's fallback firing on any
 * positive chance, not a "likely" threshold.
 */
function fallbackRainLine(forecastDays: WeatherForecastDay[], nowHour: number): string {
  const hourly = nextRain(forecastDays, nowHour, 1)
  if (hourly && hourly !== 'none') {
    const intensity = capitalize(hourlyChanceIntensityLabel(hourly.chancePct))
    return hourly.isNow ? `${intensity} rain expected now` : `${intensity} rain expected after ${hourly.label}`
  }

  const daily = checkDailyRain(forecastDays)
  if (daily.kind === 'found') {
    return `${Math.round(daily.chancePct!)}% chance of rain ${daily.dayLabel}`
  }
  if (daily.kind === 'none' && hourly === 'none') {
    return 'No rain expected in the next 6 days'
  }

  // Nothing usable anywhere in the cascade - a structural dash, never a
  // fabricated "no rain" (AGENTS.md fallback policy).
  return '—'
}

export interface NowcastStatus {
  /** The clipped, drawable bars for the strip - empty when there is nothing to draw. */
  bars: NowcastBar[]
  /** Whether the strip should be drawn at all (design point 7: only when the window has real rain signal). */
  showChart: boolean
  line: string
  /**
   * True when the strip/line above are drawn from next_hour data that is
   * itself interpolated from the hourly model, not a genuine short-range
   * nowcast (backend/weather_providers.go's next_hour_source contract,
   * e.g. Open-Meteo's minutely_15 outside its native-resolution regions).
   * Always false when showChart is false - the hourly/daily fallback tiers
   * are already labelled by their own phrasing ("expected after 4PM") and
   * don't need a second "(hourly forecast)" caption on top of that.
   */
  isHourlySourced: boolean
}

/**
 * The single entry point the tile calls: builds the display bars and picks
 * the status line, in HelmCast's priority order - nowcast "now"/"in N
 * minutes" first, then the hourly fallback, then the daily fallback, then
 * "no rain in 6 days", then a dash when nothing is known at all.
 */
export function computeNowcastStatus(params: {
  now: Date
  /** null means the provider supplied no next_hour data at all for this position. */
  nextHour: {
    stepMinutes: number
    points: NowcastPoint[]
    /**
     * "nowcast" (genuine short-range data) or "hourly" (interpolated from
     * the hourly model - see NowcastStatus.isHourlySourced). Defaults to
     * "nowcast" when omitted, so existing callers/tests that predate this
     * field keep their current (uncaptioned) behaviour; the real caller
     * (current-conditions-tile.tsx, via useWeatherForecast) always supplies
     * the provider's actual value.
     */
    source?: 'nowcast' | 'hourly'
  } | null
  forecastDays: WeatherForecastDay[]
  nowHour: number
}): NowcastStatus {
  const { now, nextHour, forecastDays, nowHour } = params

  const bars = nextHour ? buildNowcastBars(nextHour.points, now, nextHour.stepMinutes) : []
  const showChart = hasNowcastRain(bars)
  const isHourlySourced = showChart && nextHour?.source === 'hourly'
  const captionSuffix = isHourlySourced ? ' (hourly forecast)' : ''

  if (showChart) {
    // Rain "starts" where the provider forecasts rainfall, not where the
    // chance first rises above zero: a 5% chance with nothing falling is
    // not rain arriving, and HelmCast never called it that either.
    const first = bars.find((b) => b.mmPerH > 0)
    if (!first) {
      const peakChance = Math.max(...bars.map((b) => b.chancePct ?? 0))
      return { bars, showChart, isHourlySourced, line: `${Math.round(peakChance)}% chance of rain in the next hour${captionSuffix}` }
    }
    const peakMmPerH = Math.max(...bars.map((b) => b.mmPerH))
    const intensity = capitalize(nowcastIntensityLabel(peakMmPerH))
    const startMinutes = Math.round(first.offsetMinutes)
    const when = startMinutes <= 0 ? 'now' : `in ${startMinutes} minute${startMinutes === 1 ? '' : 's'}`
    return { bars, showChart, isHourlySourced, line: `${intensity} rain expected ${when}${captionSuffix}` }
  }

  return { bars, showChart, isHourlySourced: false, line: fallbackRainLine(forecastDays, nowHour) }
}

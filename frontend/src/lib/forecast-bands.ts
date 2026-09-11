import type {
  WeatherForecastDay,
  WeatherHourlyPrecipPoint,
  WeatherHourlyWindPoint,
} from '@/hooks/use-weather-forecast'

/**
 * Both windows below cover the next 24 hours starting at `nowHour` on
 * `forecastDays[0]`: hours [nowHour, 23] of day 0, then hours [0, nowHour-1]
 * of day 1 (if present) to make up the remaining hours. `forecastDays[1]`
 * missing just means the window is whatever day 0 has.
 */
function todayRemainingHours<T extends { hourOfDay: number }>(points: T[], nowHour: number): T[] {
  return points.filter((p) => p.hourOfDay >= nowHour).sort((a, b) => a.hourOfDay - b.hourOfDay)
}

function tomorrowLeadingHours<T extends { hourOfDay: number }>(points: T[], nowHour: number): T[] {
  return points.filter((p) => p.hourOfDay < nowHour).sort((a, b) => a.hourOfDay - b.hourOfDay)
}

function next24hWindow<T extends { hourOfDay: number }>(forecastDays: WeatherForecastDay[], nowHour: number, pick: (day: WeatherForecastDay) => T[]): T[] {
  const day0 = forecastDays[0]
  const day1 = forecastDays[1]
  const fromDay0 = day0 ? todayRemainingHours(pick(day0), nowHour) : []
  const fromDay1 = day1 ? tomorrowLeadingHours(pick(day1), nowHour) : []
  return [...fromDay0, ...fromDay1]
}

export function next24hWindBand(
  forecastDays: WeatherForecastDay[],
  nowHour: number,
): { min: number; max: number; gustMax: number } | null {
  const points = next24hWindow<WeatherHourlyWindPoint>(forecastDays, nowHour, (d) => d.hourlyWind)

  const speeds = points.filter((p) => p.windSpeed >= 0).map((p) => p.windSpeed)
  const gusts = points.filter((p) => p.windGust >= 0).map((p) => p.windGust)

  // A dead feed must not read as calm: no usable points at all means null,
  // never a fabricated zero band.
  if (speeds.length === 0) return null

  const max = Math.max(...speeds)
  return {
    min: Math.min(...speeds),
    max,
    // If every hour in the window is missing gust data specifically (speed
    // present, gust absent), fall back to the sustained-speed max rather
    // than 0 - gust can never read below sustained speed, so 0 here would
    // read as a fabricated dead calm rather than "gust unknown".
    gustMax: gusts.length > 0 ? Math.max(...gusts) : max,
  }
}

export function todayTempBand(day: WeatherForecastDay | undefined): { low: number; high: number } | null {
  if (!day) return null
  if (day.low === -1 || day.high === -1) return null
  return { low: day.low, high: day.high }
}

export function nextRain(
  forecastDays: WeatherForecastDay[],
  nowHour: number,
  thresholdPct = 40,
): { label: string; chancePct: number; isNow: boolean } | 'none' | null {
  const points = next24hWindow<WeatherHourlyPrecipPoint>(forecastDays, nowHour, (d) => d.hourlyPrecip)

  const withData = points.filter((p): p is WeatherHourlyPrecipPoint & { precipChancePct: number } => p.precipChancePct !== null)

  // The provider reported nothing at all in this window - we cannot say "no
  // rain", only "unknown". Never coerce this into a fabricated 'none'.
  if (withData.length === 0) return null

  const firstOverThreshold = withData.find((p) => p.precipChancePct >= thresholdPct)
  if (!firstOverThreshold) return 'none'

  // next24hWindow orders today's remaining hours (hourOfDay >= nowHour)
  // before tomorrow's leading hours (hourOfDay < nowHour), so a match on
  // nowHour itself can only be today's current hour, never a point carried
  // over from tomorrow's window.
  return {
    label: firstOverThreshold.label,
    chancePct: firstOverThreshold.precipChancePct,
    isNow: firstOverThreshold.hourOfDay === nowHour,
  }
}

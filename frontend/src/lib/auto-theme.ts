import { formatHourMinute } from '@/lib/vessel-time'

export interface AutoThemeInput {
  now: Date
  /** The vessel's IANA zone. Undefined means the browser's zone. */
  timeZone: string | undefined
  /** RFC3339 instants from the forecast (forecast[0].sunriseTime); the time of day is read in `timeZone`. */
  sunriseTime: string | null
  sunsetTime: string | null
}

export interface AutoThemeResult {
  isDark: boolean
  /** When the current theme ends, e.g. "6.12 AM". Sunrise at night, sunset by day. */
  until: string
}

function minutesOfDay(instant: Date | string | null, timeZone: string | undefined): number | null {
  if (instant === null) return null
  const now = instant instanceof Date ? instant : new Date(instant)
  if (Number.isNaN(now.getTime())) return null
  try {
    const parts = new Intl.DateTimeFormat('en-US', {
      hour: 'numeric',
      minute: 'numeric',
      hourCycle: 'h23',
      timeZone,
    }).formatToParts(now)
    const hour = Number(parts.find((p) => p.type === 'hour')?.value)
    const minute = Number(parts.find((p) => p.type === 'minute')?.value)
    if (Number.isNaN(hour) || Number.isNaN(minute)) return null
    return (hour % 24) * 60 + minute
  } catch {
    return null
  }
}

/**
 * Day or night from the sunrise and sunset the Clock tile shows. Night runs
 * from sunset to the next sunrise. Returns null, never a guess, when the times
 * are missing, unreadable, or sunset is not after sunrise.
 */
export function resolveAutoTheme({ now, timeZone, sunriseTime, sunsetTime }: AutoThemeInput): AutoThemeResult | null {
  const sunrise = minutesOfDay(sunriseTime, timeZone)
  const sunset = minutesOfDay(sunsetTime, timeZone)
  if (sunrise === null || sunset === null || sunset <= sunrise) return null
  const current = minutesOfDay(now, timeZone)
  if (current === null) return null
  const isDark = current < sunrise || current >= sunset
  const until = isDark ? sunrise : sunset
  return { isDark, until: formatHourMinute(Math.floor(until / 60), until % 60) }
}

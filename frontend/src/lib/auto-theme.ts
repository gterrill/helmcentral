export interface AutoThemeInput {
  now: Date
  /** The vessel's IANA zone. Undefined means the browser's zone. */
  timeZone: string | undefined
  /** Backend-formatted, e.g. "6:12AM", in the vessel's zone (forecast[0].sunriseTime). */
  sunriseTime: string | null
  sunsetTime: string | null
}

export interface AutoThemeResult {
  isDark: boolean
  /** When the current theme ends, e.g. "6:12 AM". Sunrise at night, sunset by day. */
  until: string
}

const CLOCK = /^(\d{1,2}):(\d{2})\s*([AP]M)$/i

/** Minutes after midnight for "6:12AM" / "5:48 PM", or null when the string is not that format. */
export function parseClockMinutes(value: string | null): number | null {
  if (value === null) return null
  const match = CLOCK.exec(value.trim())
  if (!match) return null
  const hour12 = Number(match[1])
  const minute = Number(match[2])
  if (hour12 < 1 || hour12 > 12 || minute > 59) return null
  const pm = match[3]!.toUpperCase() === 'PM'
  return ((hour12 % 12) + (pm ? 12 : 0)) * 60 + minute
}

function formatMinutes(minutes: number): string {
  const hour24 = Math.floor(minutes / 60)
  const minute = String(minutes % 60).padStart(2, '0')
  return `${hour24 % 12 === 0 ? 12 : hour24 % 12}:${minute} ${hour24 >= 12 ? 'PM' : 'AM'}`
}

function minutesOfDay(now: Date, timeZone: string | undefined): number | null {
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
  const sunrise = parseClockMinutes(sunriseTime)
  const sunset = parseClockMinutes(sunsetTime)
  if (sunrise === null || sunset === null || sunset <= sunrise) return null
  const current = minutesOfDay(now, timeZone)
  if (current === null) return null
  const isDark = current < sunrise || current >= sunset
  return { isDark, until: formatMinutes(isDark ? sunrise : sunset) }
}

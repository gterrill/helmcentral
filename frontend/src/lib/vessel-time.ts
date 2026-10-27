import type { VesselStateValue } from '@/hooks/use-vessel-state'

/** The vessel's IANA zone as the formatters take it: undefined until the boat has reported one. */
export const selectTimeZone = (state: VesselStateValue): string | undefined => state.timeZone ?? undefined

// The vessel's local zone (backend/weather_tide.go's vesselLocalTimezoneName,
// ADR 0035) can be an IANA name Intl has never heard of only if the backend
// itself starts sending something malformed — that upstream contract is
// worth failing loudly for, but not by crashing the wall display's clock.
// Warn once per bad zone name rather than throwing, and rather than warning
// on every per-second tick.
const warnedInvalidTimeZones = new Set<string>()

// Formatters cached per (timeZone, options) at module level rather than
// rebuilt on every call: the clock readers format `now` once per second, and
// clock-tile.tsx calls both directly on top of that — constructing a fresh
// Intl.DateTimeFormat that often is pure waste.
const formatterCache = new Map<string, Intl.DateTimeFormat>()

function dateTimeFormat(options: Intl.DateTimeFormatOptions, timeZone?: string): Intl.DateTimeFormat {
  const key = `${timeZone ?? ''}|${JSON.stringify(options)}`
  const cached = formatterCache.get(key)
  if (cached) return cached

  let formatter: Intl.DateTimeFormat
  if (timeZone) {
    try {
      formatter = new Intl.DateTimeFormat('en-US', { ...options, timeZone })
    } catch (error) {
      if (!warnedInvalidTimeZones.has(timeZone)) {
        warnedInvalidTimeZones.add(timeZone)
        console.warn(`vessel-time: unknown timezone "${timeZone}", falling back to the browser zone`, error)
      }
      formatter = new Intl.DateTimeFormat('en-US', options)
    }
  } else {
    formatter = new Intl.DateTimeFormat('en-US', options)
  }
  formatterCache.set(key, formatter)
  return formatter
}

export function formatClock(date: Date, timeZone?: string) {
  const value = dateTimeFormat(
    {
      hour: '2-digit',
      minute: '2-digit',
      second: '2-digit',
      hour12: true,
    },
    timeZone,
  ).format(date)

  const [timePart = '--:--:--', meridiem = ''] = value.toUpperCase().split(/\s+/)
  return { timePart, meridiem }
}

export function formatDate(date: Date, options?: { compact?: boolean; timeZone?: string }) {
  return dateTimeFormat(
    {
      weekday: options?.compact ? 'short' : 'long',
      month: 'short',
      day: 'numeric',
      year: 'numeric',
    },
    options?.timeZone,
  ).format(date)
}

/** 3-letter weekday in the given zone ("Thu"), for clock-tile.tsx's ETA line: HelmCast prefixed a cross-day ETA with the weekday rather than leaving it looking like today. */
export function formatWeekday(date: Date, timeZone?: string): string {
  return dateTimeFormat({ weekday: 'short' }, timeZone).format(date)
}

/** Whether `a` and `b` fall on the same calendar date in the given zone - the test clock-tile.tsx runs to decide whether an ETA needs formatWeekday's prefix. */
export function isSameLocalDate(a: Date, b: Date, timeZone?: string): boolean {
  const key = dateTimeFormat({ year: 'numeric', month: '2-digit', day: '2-digit' }, timeZone)
  return key.format(a) === key.format(b)
}

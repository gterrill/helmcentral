import type { WindChange } from '@/hooks/use-weather-forecast'
import { calendarDayLabel } from '@/lib/vessel-time'

/** A day as the wind-change wording needs it. */
export interface WindChangeDay {
  dayKey: string
  windChanges: WindChange[]
}

/** "Tue 17": a calendar day written out. */
function absoluteDay(dayKey: string): string {
  return `${calendarDayLabel(dayKey, { weekday: 'short' })} ${calendarDayLabel(dayKey, { day: 'numeric' })}`
}

/**
 * Names a change's day: "today", "tomorrow" or "Tue 17". The response is held
 * for up to an hour, so the word is worked out when the page draws, from the
 * boat's date then, never from when the server answered. With no date for the
 * boat yet the day is written out.
 */
export function windChangeDayName(dayKey: string, todayKey: string | null): string {
  if (todayKey) {
    const dayMs = Date.parse(`${dayKey}T00:00:00Z`)
    const todayMs = Date.parse(`${todayKey}T00:00:00Z`)
    if (dayMs === todayMs) return 'today'
    if (dayMs === todayMs + 86_400_000) return 'tomorrow'
  }
  return absoluteDay(dayKey)
}

/**
 * "Wind changes checked to Thu 19 3 AM: the forecast skips an hour here": how far
 * the changes were read when the hourly forecast stops early. The backend words
 * the time on the boat's clock and says why; both are shown as given.
 */
export function windChangesCheckedToText(to: string, reason: string | null): string {
  return reason ? `Wind changes checked to ${to}: ${reason}` : `Wind changes checked to ${to}`
}

const ADJECTIVE: Record<string, string> = {
  N: 'Northerly',
  NE: 'North-easterly',
  E: 'Easterly',
  SE: 'South-easterly',
  S: 'Southerly',
  SW: 'South-westerly',
  W: 'Westerly',
  NW: 'North-westerly',
}

/** "N→SE 2 PM": the chip on a day card. */
export function windChangeChipText(change: WindChange): string {
  return `${change.from}→${change.to} ${change.label}`
}

export const UNSURE = 'less certain this far out'

/** The change a day leads with: the first prominent one, else the first. */
export function leadWindChange<T extends { prominent: boolean }>(changes: T[]): T | undefined {
  return changes.find((c) => c.prominent) ?? changes[0]
}

/**
 * The wind sentence that leads the extended summary, for example
 * "A South-easterly change expected Tue 17 around 2 PM." It names only the
 * first change; the day cards carry the rest. The day is named from the day the
 * change sits under and the boat's date now (`todayKey`): "today", "tomorrow" or
 * "Tue 17". A principal point reads as its adjective ("A Southerly change"), any
 * other as the bare point ("A SSE change", "An ENE change"). It never says a
 * change is less certain: the operator knows a forecast gets less sure further
 * out. The day-card chip and plan rows still mark it. Null when the
 * forecast holds no change: nothing is said rather than a guess.
 */
export function buildWindChangeSentence(days: WindChangeDay[], todayKey: string | null): string | null {
  const day = days.find((d) => d.windChanges.length > 0)
  if (!day) return null
  const first = day.windChanges[0]
  const name = ADJECTIVE[first.to] ?? first.to
  // Read aloud: "An Easterly", "An ENE".
  const article = name.startsWith('E') ? 'An' : 'A'
  return `${article} ${name} change expected ${windChangeDayName(day.dayKey, todayKey)} around ${first.label}.`
}

import type { WindChange } from '@/hooks/use-weather-forecast'
import { formatWindRange } from '@/lib/plans'
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
  N: 'Northerlies',
  NE: 'North-easterlies',
  E: 'Easterlies',
  SE: 'South-easterlies',
  S: 'Southerlies',
  SW: 'South-westerlies',
  W: 'Westerlies',
  NW: 'North-westerlies',
}

/** "Northerlies" for a principal point, "NNE winds" for the rest. */
function windsFrom(point: string): string {
  return ADJECTIVE[point] ?? `${point} winds`
}

/** "N→SE 2 PM": the chip on a day card. */
export function windChangeChipText(change: WindChange): string {
  return `${change.from}→${change.to} ${change.label}`
}

const UNSURE = 'less certain this far out'

/**
 * The wind sentence that leads the extended summary, for example
 * "Northerlies until a SE change Tue 17 around 2 PM, 15–20 kn behind it." Days
 * are named from the day each change sits under and the boat's date now
 * (`todayKey`): "today", "tomorrow" or "Tue 17". Null when the forecast holds no change: nothing is
 * said rather than a guess. Later changes follow as "then a W change Thu 19
 * around 1 AM".
 */
export function buildWindChangeSentence(days: WindChangeDay[], todayKey: string | null): string | null {
  const changes = days.flatMap((day) => day.windChanges.map((change) => ({ ...change, dayName: windChangeDayName(day.dayKey, todayKey) })))
  const first = changes[0]
  if (!first) return null
  // Points are read as words, so "an ESE" (an east-south-east) but "a SE".
  const phrase = (c: (typeof changes)[number]) =>
    `${c.to.startsWith('E') ? 'an' : 'a'} ${c.to} change ${c.dayName} around ${c.label}`
  let out = `${windsFrom(first.from)} until ${phrase(first)}, ${formatWindRange(first.speedMinKn, first.speedMaxKn)} behind it`
  if (first.lowerConfidence) out += ` (${UNSURE})`
  for (const c of changes.slice(1)) {
    out += `, then ${phrase(c)}`
    if (c.lowerConfidence) out += ` (${UNSURE})`
  }
  return `${out}.`
}

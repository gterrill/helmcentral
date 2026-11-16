import type { WindChange } from '@/hooks/use-weather-forecast'
import { calendarDayLabel, formatDayKey, zonedWallInstant, zoneOffsetMinutes } from '@/lib/vessel-time'

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

/** The change a day leads with: the first prominent one, else the first. */
export function leadWindChange<T extends { prominent: boolean }>(changes: T[]): T | undefined {
  return changes.find((c) => c.prominent) ?? changes[0]
}

/** The boat's time: an instant and the zone its clock is read in. */
export interface BoatNow {
  at: Date
  timeZone: string
}

const LABEL_CLOCK = /^(\d{1,2})(?:\.(\d{2}))? (AM|PM)$/

/** Whether a change's label reads as a boat-clock time ("2 PM", "1.30 PM"). */
export function windChangeLabelReadable(label: string): boolean {
  const m = LABEL_CLOCK.exec(label)
  return m !== null && Number(m[1]) >= 1 && Number(m[1]) <= 12 && Number(m[2] ?? 0) <= 59
}

/**
 * The instant a change happens: its boat-clock label ("2 PM", "1.30 PM") on the
 * day it sits under. Null when the label does not read as a time.
 */
function windChangeInstant(dayKey: string, label: string, timeZone: string): Date | null {
  const m = LABEL_CLOCK.exec(label)
  const [year, month, day] = dayKey.split('-').map(Number)
  if (!m || !year || !month || !day) return null
  const hour12 = Number(m[1])
  const minute = m[2] ? Number(m[2]) : 0
  if (hour12 < 1 || hour12 > 12 || minute > 59) return null
  const hour = (hour12 % 12) + (m[3] === 'PM' ? 12 : 0)
  return zonedWallInstant(Date.UTC(year, month - 1, day, hour, minute), timeZone)
}

/** The start of the boat's current hour: the backend keeps a change from the hour that has begun. */
function startOfBoatHour(now: BoatNow): number {
  const offsetMs = zoneOffsetMinutes(now.timeZone, now.at) * 60_000
  const wall = now.at.getTime() + offsetMs
  return wall - (((wall % 3_600_000) + 3_600_000) % 3_600_000) - offsetMs
}

/**
 * The wind sentence that leads the extended summary, for example
 * "A South-easterly change expected Tue 17 around 2 PM." It names one change,
 * chosen across all the days as the day-card chip chooses within a day: the
 * first that reaches the marginal wind, else the first. The day is named from
 * the day the change sits under and the boat's time now: "today", "tomorrow" or
 * "Tue 17". A change whose time has already passed is left out before choosing,
 * because the response is held for up to an hour and a page can be shown stale.
 * With no time for the boat yet nothing is dropped and the day is written out.
 * A principal point reads as its adjective ("A Southerly change"), any other as
 * the bare point ("A SSE change", "An ENE change"). It never says a change is
 * less certain: the operator knows a forecast gets less sure further out. The
 * day-card chip and plan rows still mark it. Null when no change is to come:
 * nothing is said rather than a guess.
 */
export function buildWindChangeSentence(days: WindChangeDay[], now: BoatNow | null): string | null {
  const cutoff = now ? startOfBoatHour(now) : 0
  const upcoming: { dayKey: string; change: WindChange }[] = []
  for (const d of days) {
    for (const change of d.windChanges) {
      if (now) {
        const instant = windChangeInstant(d.dayKey, change.label, now.timeZone)
        if (!instant) {
          console.error('wind change: unreadable time', d.dayKey, change.label)
          continue
        }
        if (instant.getTime() < cutoff) continue
      }
      upcoming.push({ dayKey: d.dayKey, change })
    }
  }
  const lead = leadWindChange(upcoming.map((u) => ({ ...u, prominent: u.change.prominent })))
  if (!lead) return null
  const todayKey = now ? formatDayKey(now.at, now.timeZone) : null
  const name = ADJECTIVE[lead.change.to] ?? lead.change.to
  // Read aloud: "An Easterly", "An ENE".
  const article = name.startsWith('E') ? 'An' : 'A'
  return `${article} ${name} change expected ${windChangeDayName(lead.dayKey, todayKey)} around ${lead.change.label}.`
}

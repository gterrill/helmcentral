import type { VesselStateValue } from '@/hooks/use-vessel-state'

/** The vessel's IANA zone as the formatters take it: undefined until the boat has reported one. */
export const selectTimeZone = (state: VesselStateValue): string | undefined => state.timeZone ?? undefined

/** Where the boat's zone came from; null until the server has said. */
export const selectTimeZoneSource = (state: VesselStateValue): VesselStateValue['timeZoneSource'] => state.timeZoneSource

// A zone name Intl does not know throws a RangeError, and nothing is cached for
// it: the server contract is a real IANA name, and a clock in another zone
// would be a wrong time shown as right. Only an absent zone (the boat has not
// reported one yet) reads the device's clock, and that says so once.
let warnedNoZone = false

// Whether this browser knows a zone name, asked once per name. A name it does
// not know (an older kiosk with a stale ICU) is reported once, in the console,
// and the surfaces that cannot sit behind an error boundary show their own
// error state or "--" instead of throwing. A name the server sends is never
// read as another zone.
const zoneKnownCache = new Map<string, boolean>()

export function zoneKnown(timeZone: string): boolean {
  const cached = zoneKnownCache.get(timeZone)
  if (cached !== undefined) return cached
  let known = true
  try {
    new Intl.DateTimeFormat('en-US', { timeZone })
  } catch (error) {
    known = false
    console.error(`vessel-time: zone ${timeZone} is unknown to this browser`, error)
  }
  zoneKnownCache.set(timeZone, known)
  return known
}

/** The words for a zone this browser does not know, or null when it knows it. */
export function unknownZoneMessage(timeZone: string | undefined): string | null {
  return timeZone && !zoneKnown(timeZone) ? `Zone ${timeZone} unknown to this browser` : null
}

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
    formatter = new Intl.DateTimeFormat('en-US', { ...options, timeZone })
  } else {
    if (!warnedNoZone) {
      warnedNoZone = true
      console.warn('vessel-time: the boat has sent no zone yet, so times use this device\'s clock until it does')
    }
    // helm-browser-zone: the clock surfaces draw before the first vessel frame carries a zone; they read the device's clock, with the warning above, until the boat's zone arrives.
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

// ── helpers for times at the boat or at a named place ───────────────────

// Where the zone is not the vessel's own (a passage's first point, a planned
// stay) the server names it in an IANA field and these read it. A zone name
// Intl does not know throws: the server contract is IANA, and a place whose
// zone cannot be read must say so rather than show another zone's clock.

const OFFSET_NAME = /GMT([+-])(\d{1,2})(?::(\d{2}))?/

/** Minutes east of UTC for `timeZone` at the instant `at`. */
export function zoneOffsetMinutes(timeZone: string, at: Date | number): number {
  const name = dateTimeFormat({ timeZoneName: 'longOffset' }, timeZone)
    .formatToParts(at)
    .find((p) => p.type === 'timeZoneName')?.value
  if (name === 'GMT') return 0
  const m = name ? OFFSET_NAME.exec(name) : null
  if (!m) throw new Error(`vessel-time: no UTC offset for zone "${timeZone}"`)
  const minutes = Number(m[2]) * 60 + Number(m[3] ?? 0)
  return m[1] === '-' ? -minutes : minutes
}

/** "UTC", "UTC+10", "UTC+9:30", "UTC-5" for `timeZone` at the instant `at`. */
export function zoneOffsetLabel(timeZone: string, at: Date | number): string {
  const minutes = zoneOffsetMinutes(timeZone, at)
  if (minutes === 0) return 'UTC'
  const abs = Math.abs(minutes)
  const h = Math.floor(abs / 60)
  const m = abs % 60
  return `UTC${minutes < 0 ? '-' : '+'}${h}${m ? `:${String(m).padStart(2, '0')}` : ''}`
}

/** The instant the calendar day `dayKey` (YYYY-MM-DD) begins in `timeZone`. */
export function zonedDayStart(dayKey: string, timeZone: string): Date {
  const [year, month, day] = dayKey.split('-').map(Number)
  const wall = Date.UTC(year, month - 1, day)
  // Offset at the first guess, then at the corrected instant: the second pass
  // settles a day whose midnight sits across a daylight-saving change.
  const first = wall - zoneOffsetMinutes(timeZone, wall) * 60_000
  return new Date(wall - zoneOffsetMinutes(timeZone, first) * 60_000)
}

/** Whether two zones show a different clock at the instant `at`. */
export function zoneClocksDiffer(a: string, b: string, at: Date | number): boolean {
  return zoneOffsetMinutes(a, at) !== zoneOffsetMinutes(b, at)
}

/** "14 Mar", "09:32" and "UTC+10" for an instant in `timeZone`. */
export function zonedDateTimeParts(unixSeconds: number, timeZone: string): { date: string; time: string; zone: string } {
  if (!zoneKnown(timeZone)) return { date: NO_TIME, time: NO_TIME, zone: '' }
  const at = new Date(unixSeconds * 1000)
  const parts = dateTimeFormat({ day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit', hourCycle: 'h23' }, timeZone).formatToParts(at)
  const pick = (type: string) => parts.find((p) => p.type === type)?.value ?? ''
  return { date: `${pick('day')} ${pick('month')}`, time: `${pick('hour')}:${pick('minute')}`, zone: zoneOffsetLabel(timeZone, at) }
}

/** The calendar date (YYYY-MM-DD) of an instant in `timeZone`. */
export function formatDayKey(date: Date, timeZone: string): string {
  const parts = dateTimeFormat({ year: 'numeric', month: '2-digit', day: '2-digit' }, timeZone).formatToParts(date)
  const pick = (type: string) => parts.find((p) => p.type === type)?.value ?? ''
  return `${pick('year')}-${pick('month')}-${pick('day')}`
}

// ── operator-facing stamps in the boat's zone ───────────────────────────
//
// Every helper here takes the zone as an argument and answers "--" until the
// boat has reported one, rather than reading the device's clock in the
// meantime. Callers get the zone from useVesselStateSelector(selectTimeZone).

export const NO_TIME = '--'

type Instant = Date | number | string

function toDate(at: Instant): Date | null {
  const d = at instanceof Date ? at : new Date(at)
  return Number.isNaN(d.getTime()) ? null : d
}

function partsIn(date: Date, timeZone: string, options: Intl.DateTimeFormatOptions): Record<string, string> {
  const out: Record<string, string> = {}
  for (const p of dateTimeFormat(options, timeZone).formatToParts(date)) out[p.type] = p.value
  return out
}

/** "14:05", or "2:05 PM" with hour12. */
export function formatTimeOfDay(at: Instant, timeZone: string | undefined, options?: { hour12?: boolean }): string {
  const date = toDate(at)
  if (!date || !timeZone || !zoneKnown(timeZone)) return NO_TIME
  if (options?.hour12) {
    const p = partsIn(date, timeZone, { hour: 'numeric', minute: '2-digit', hour12: true })
    return `${p.hour}:${p.minute} ${p.dayPeriod}`
  }
  const p = partsIn(date, timeZone, { hour: '2-digit', minute: '2-digit', hourCycle: 'h23' })
  return `${p.hour}:${p.minute}`
}

/** "30 Aug", or "30 Aug 2026" with year. */
export function formatDayMonthIn(at: Instant, timeZone: string | undefined, options?: { year?: boolean }): string {
  const date = toDate(at)
  if (!date || !timeZone || !zoneKnown(timeZone)) return NO_TIME
  const p = partsIn(date, timeZone, { day: 'numeric', month: 'short', year: 'numeric' })
  return options?.year ? `${p.day} ${p.month} ${p.year}` : `${p.day} ${p.month}`
}

/** "14:05" for an instant on the boat's date today, "30 Aug 14:05" otherwise. */
export function formatTimeOrDayTime(at: Instant, timeZone: string | undefined, now: Date = new Date()): string {
  const date = toDate(at)
  if (!date || !timeZone || !zoneKnown(timeZone)) return NO_TIME
  const time = formatTimeOfDay(date, timeZone)
  return isSameLocalDate(date, now, timeZone) ? time : `${formatDayMonthIn(date, timeZone)} ${time}`
}

/** "30 Aug 2026 14:05". */
export function formatDayTimeIn(at: Instant, timeZone: string | undefined): string {
  const date = toDate(at)
  if (!date || !timeZone || !zoneKnown(timeZone)) return NO_TIME
  return `${formatDayMonthIn(date, timeZone, { year: true })} ${formatTimeOfDay(date, timeZone)}`
}

/** The hour of day (0-23) of an instant in `timeZone`; null until the boat has reported a zone. */
export function hourOfDayIn(at: Instant, timeZone: string | undefined): number | null {
  const date = toDate(at)
  if (!date || !timeZone || !zoneKnown(timeZone)) return null
  return Number(partsIn(date, timeZone, { hour: 'numeric', hourCycle: 'h23' }).hour) % 24
}

/** A forecast day's calendar key (YYYY-MM-DD, at the forecast's place). The server always sends one; a day without it is a broken payload, not a day to guess. */
export function requireDayKey(value: unknown): string {
  if (typeof value !== 'string' || !/^\d{4}-\d{2}-\d{2}$/.test(value)) {
    throw new Error(`forecast day has no usable day_key (${JSON.stringify(value)})`)
  }
  return value
}

/** A calendar day (YYYY-MM-DD) written out. It is a date, not an instant, so it is read in UTC and no zone moves it. */
export function calendarDayLabel(dayKey: string, options: Intl.DateTimeFormatOptions): string {
  const [year, month, day] = dayKey.split('-').map(Number)
  return dateTimeFormat(options, 'UTC').format(new Date(Date.UTC(year, month - 1, day)))
}

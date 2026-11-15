/**
 * Today's date, YYYY-MM-DD, in the OPERATOR'S OWN LOCAL TIMEZONE - never
 * UTC (ADR 0138's 2026-09-27 amendment).
 *
 * Maintenance rules and their due dates are calendar dates read off a wall
 * calendar, not instants. A boat well east of UTC (this one runs at
 * UTC+10) computing `new Date().toISOString().slice(0, 10)` - the bug this
 * replaces - would get YESTERDAY's date for roughly the first ten hours of
 * every single day, because `toISOString()` always renders in UTC
 * regardless of the device's own timezone. `getFullYear()`/`getMonth()`/
 * `getDate()` read the browser's own local clock instead - the one on the
 * wall at the helm.
 *
 * This is the device's date, for what a form starts on and for ages shown
 * beside a date. It is never sent to the server as the day: the boat's day
 * comes from the server (ADR 0230), which works it out from the boat's zone.
 */
export function todayISO(date: Date = new Date()): string {
  // helm-browser-zone: a form's starting date and ages beside a date are the operator's own device date; due dates use the boat's day (ADR 0230).
  const local = { year: date.getFullYear(), month: date.getMonth() + 1, day: date.getDate() }
  return `${local.year}-${String(local.month).padStart(2, '0')}-${String(local.day).padStart(2, '0')}`
}

/**
 * dateISO plus months, as YYYY-MM-DD - the frontend's own read-only
 * preview of what the backend will compute for a fixed-due-date rule that
 * also carries interval_months (completeMaintenanceRuleHandler advances
 * fixed_due_date to performed_at + interval_months itself; this is only
 * ever a display of that same arithmetic, never sent to the server).
 * Calendar-safe the same way Go's time.AddDate is: adding a month to
 * January 31 lands on whatever March actually has (JS's own Date
 * normalises an out-of-range day by rolling into the following month,
 * which is the same "spills over" behaviour, not a mismatch worth
 * guarding against here).
 */
export function addMonthsISO(dateISO: string, months: number): string {
  const [year, month, day] = dateISO.split('-').map(Number)
  const d = new Date(year, month - 1 + months, day)
  return todayISO(d)
}

/** What a repair form says when the fault is dated after the repair. */
export const STARTED_AFTER_REPAIR = "Started showing can't be after the repair date."

/**
 * Whether a repair's "Started showing" date is after the repair's own date, both
 * YYYY-MM-DD. Only a repair has one, and a blank date is no problem. The server
 * refuses it, so the forms say so first (ADR 0276).
 */
export function startedAfterRepair(kind: string, startedAt: string, performedAt: string): boolean {
  return kind === 'repair' && startedAt !== '' && performedAt !== '' && startedAt > performedAt
}

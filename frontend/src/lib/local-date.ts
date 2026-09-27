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
 * Every maintenance API call that needs "today" (use-maintenance.ts) calls
 * this exactly once and sends the result as the backend's required
 * `?today=` query param (maintenance_handlers.go's requireTodayParam) -
 * the server never computes its own "today" and refuses to guess when it
 * isn't told one.
 */
export function todayISO(date: Date = new Date()): string {
  const year = date.getFullYear()
  const month = String(date.getMonth() + 1).padStart(2, '0')
  const day = String(date.getDate()).padStart(2, '0')
  return `${year}-${month}-${day}`
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

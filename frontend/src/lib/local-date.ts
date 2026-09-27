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

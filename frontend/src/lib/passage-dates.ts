/** "Thu 23 Oct" from a local YYYY-MM-DD; read as UTC so no zone shifts the day. */
export function formatRowDate(ymd: string): string {
  const parts = new Intl.DateTimeFormat('en-GB', { timeZone: 'UTC', weekday: 'short', day: 'numeric', month: 'short' }).formatToParts(
    new Date(`${ymd}T00:00:00Z`),
  )
  const get = (t: string) => parts.find((p) => p.type === t)?.value ?? ''
  return `${get('weekday')} ${get('day')} ${get('month')}`
}

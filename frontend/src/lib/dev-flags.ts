// Switches for checking by eye in a development build. They are read only when
// `import.meta.env.DEV` is true, so a production build has none of them: the
// bundler drops the branch.

const MAX_OFFSET_H = 24

// The query string as the page was opened. The app rewrites the URL as it routes,
// which drops these switches before the replay mounts; main.tsx imports this
// module first so the page's own query is what gets kept.
const OPENED_SEARCH = typeof window === 'undefined' ? '' : window.location.search

/** Hours to add to the time the replay's sun is worked out for (`?replayClockOffsetH=9`),
 * so dusk and night can be looked at on a passage sailed by day. It moves the
 * sun only, not the replay clock. */
export function replayClockOffsetHours(search: string = OPENED_SEARCH, dev: boolean = import.meta.env.DEV): number {
  if (!dev) return 0
  const raw = new URLSearchParams(search).get('replayClockOffsetH')
  if (raw === null || raw.trim() === '') return 0
  const hours = Number(raw)
  return Number.isFinite(hours) ? Math.max(-MAX_OFFSET_H, Math.min(MAX_OFFSET_H, hours)) : 0
}

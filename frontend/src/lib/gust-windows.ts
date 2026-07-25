// Ladder of gust-averaging windows offered on the wind tile's "MAX GUST" cards.
// Each card cycles forward through this list (wrapping from the last back to
// the first) independently of the other card.
//
// MUST stay in sync with the backend's ladder (backend/main.go — the
// package-level slice of the same 4 window strings used to key the
// `max_gust_kts` response object). If the backend's windows ever change,
// update this list to match.
export const GUST_WINDOWS = ['10m', '30m', '1h', '24h'] as const

export type GustWindow = (typeof GUST_WINDOWS)[number]

export const GUST_WINDOW_LABELS: Record<GustWindow, string> = {
  '10m': '10M',
  '30m': '30M',
  '1h': '1HR',
  '24h': '24HR',
}

// Spelled-out form for accessible labels, e.g. "Max gust over 10 minutes — click to change window".
export const GUST_WINDOW_SPOKEN: Record<GustWindow, string> = {
  '10m': '10 minutes',
  '30m': '30 minutes',
  '1h': '1 hour',
  '24h': '24 hours',
}

/** Cycles forward through GUST_WINDOWS, wrapping from the last window back to the first. */
export function nextGustWindow(window: GustWindow): GustWindow {
  const index = GUST_WINDOWS.indexOf(window)
  return GUST_WINDOWS[(index + 1) % GUST_WINDOWS.length]
}

/** Validates an arbitrary/corrupt string (e.g. from localStorage) as a GustWindow, or null if invalid. */
export function parseGustWindow(value: string | null): GustWindow | null {
  return (GUST_WINDOWS as readonly string[]).includes(value ?? '') ? (value as GustWindow) : null
}

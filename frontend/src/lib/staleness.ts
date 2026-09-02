/**
 * Freshness checks for telemetry values.
 *
 * A tile that renders the last number it was given cannot distinguish "the
 * panels are making nothing" from "the Victron feed died an hour ago and this
 * is the reading from before dawn". Both look like 0 W. Sources carry the age
 * of their last update so the UI can tell those apart and say so.
 *
 * Ages arrive already computed by the backend against the vessel clock, so
 * nothing here depends on the browser's clock being right.
 */

/**
 * SignalK sources on a boat publish every few seconds. Two minutes of silence
 * is a stopped feed, not a quiet one. Deliberately well clear of the dashboard
 * refresh interval so a slow poll cannot trip it.
 */
export const STALE_AFTER_SECONDS = 120

/**
 * Whether a value is too old to show as current.
 *
 * A `null` age means the source carries no timestamp at all. That is an
 * absence of evidence, not evidence of staleness, so it reads as not stale;
 * treating it otherwise would flag such sources forever and teach the operator
 * to ignore the indicator.
 */
export function isStale(ageSeconds: number | null, thresholdSeconds = STALE_AFTER_SECONDS): boolean {
  if (ageSeconds === null || !Number.isFinite(ageSeconds)) {
    return false
  }
  return ageSeconds > thresholdSeconds
}

/** Compact age for a badge: `45s`, `2m`, `1h 39m`, `2d`. */
export function formatDataAge(ageSeconds: number | null): string {
  if (ageSeconds === null || !Number.isFinite(ageSeconds) || ageSeconds < 0) {
    return '—'
  }

  const seconds = Math.floor(ageSeconds)
  if (seconds < 60) {
    return `${seconds}s`
  }
  if (seconds < 3600) {
    return `${Math.floor(seconds / 60)}m`
  }
  if (seconds < 86400) {
    const hours = Math.floor(seconds / 3600)
    const minutes = Math.floor((seconds % 3600) / 60)
    return minutes > 0 ? `${hours}h ${minutes}m` : `${hours}h`
  }
  return `${Math.floor(seconds / 86400)}d`
}

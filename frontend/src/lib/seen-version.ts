/** localStorage key holding the last Helmcentral version this browser showed. */
export const SEEN_VERSION_KEY = 'helmcentral.lastSeenVersion'

/**
 * Compares the running version with the one this browser last showed and
 * remembers the running one. Returns the version to announce, or null when
 * there is nothing to say: the first load ever (stored silently), the same
 * version again, a dev build, or storage that cannot be read or written (that
 * is logged, and the announcement skipped rather than guessed at).
 *
 * A revision-only change keeps the same version string and so never announces.
 */
export function recordSeenVersion(version: string): string | null {
  let previous: string | null
  try {
    previous = localStorage.getItem(SEEN_VERSION_KEY)
    if (previous !== version) localStorage.setItem(SEEN_VERSION_KEY, version)
  } catch (err) {
    console.warn('Could not read or record the last version shown; skipping the update message', err)
    return null
  }
  if (previous === null || previous === version || version === 'dev') return null
  return version
}

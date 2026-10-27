import type { PassagesFilter } from '@/lib/app-location'
import type { PassageSummary } from '@/lib/passages'

// Kept apart from lib/passages.ts: the export player bundles that file and
// must not pull the app location (and React with it) in behind it.

/** The passages the index lists for a filter: all, those in no Cruise, or one Cruise's. */
export function passagesInFilter(passages: PassageSummary[], filter: PassagesFilter): PassageSummary[] {
  if (filter.kind === 'all') return passages
  if (filter.kind === 'unfiled') return passages.filter((p) => p.cruise_id === null)
  return passages.filter((p) => p.cruise_id === filter.id)
}

/** The passages either side of `id` in start time, within the filter the page
 * was opened from. Null at each end, and for both when `id` is not in the filter. */
export function passageNeighbours(
  passages: PassageSummary[],
  id: string,
  filter: PassagesFilter,
): { previous: PassageSummary | null; next: PassageSummary | null } {
  const ordered = [...passagesInFilter(passages, filter)].sort((a, b) => Date.parse(a.start) - Date.parse(b.start))
  const at = ordered.findIndex((p) => p.id === id)
  if (at < 0) return { previous: null, next: null }
  return { previous: ordered[at - 1] ?? null, next: ordered[at + 1] ?? null }
}

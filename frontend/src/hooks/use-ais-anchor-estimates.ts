import type { AisAnchorEstimate } from '@/lib/ais-anchor-estimates'
import { apiRequest } from '@/lib/api-query'
import { shallowEqualArray } from '@/lib/shallow-equal'
import { createSharedPoller, type PollStore } from '@/lib/shared-poller'

const EMPTY: AisAnchorEstimate[] = []

async function pollEstimates(signal: AbortSignal, store: PollStore<AisAnchorEstimate[]>): Promise<void> {
  try {
    const data = await apiRequest<{ estimates?: AisAnchorEstimate[] }>('/api/anchor-watch/ais-anchors', { signal })
    if (signal.aborted) return
    const next = data.estimates ?? []
    store.publish(next.length === 0 ? EMPTY : next)
  } catch (err) {
    if (signal.aborted) return
    // Stale anchors must not stay drawn as current. Unlike the trails, this
    // snapshot has no cursor to keep.
    console.error('AIS anchor estimates unavailable:', err)
    store.publish(EMPTY)
  }
}

// A quiet watch brings the same estimates each poll, and the map is not
// re-rendered for them. The snapshot empties when the last reader leaves.
const poller = createSharedPoller<AisAnchorEstimate[]>(EMPTY, pollEstimates, () => {}, {
  resetOnIdle: true,
  isEqual: shallowEqualArray,
})

/**
 * Polls /api/anchor-watch/ais-anchors for where nearby boats have anchored, as
 * the server estimates it from their recorded tracks. Empty with no watch, until
 * a boat has swung enough to fit, and when the poll fails. The anchor watch tile
 * and page share one poll (lib/shared-poller.ts). Estimates move slowly, so the
 * default interval is long.
 */
export function useAisAnchorEstimates(pollIntervalMs = 30_000, enabled = true): AisAnchorEstimate[] {
  return poller.useSnapshot(pollIntervalMs, enabled)
}

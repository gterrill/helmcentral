import { useApiQuery } from '@/lib/api-query'
import { REPLAY_SIGNS_KEY, replaySignsPath } from '@/lib/replay-signs-api'
import type { ReplaySign } from '@/lib/replay-signs'

/** The cache key of the signs read over a span. */
export const replaySignsKey = (fromMs: number | null, toMs: number | null) => [...REPLAY_SIGNS_KEY, fromMs, toMs]

const NO_SIGNS: ReplaySign[] = []

/** The signs whose time falls between two Unix-millisecond instants: the span
 * the replay covers. Disabled while there is nothing to replay. */
export function useReplaySigns(fromMs: number | null, toMs: number | null) {
  const enabled = fromMs !== null && toMs !== null
  const query = useApiQuery({
    queryKey: replaySignsKey(fromMs, toMs),
    path: enabled ? replaySignsPath(fromMs, toMs) : '',
    enabled,
    select: (raw: { signs?: ReplaySign[] }) => raw.signs ?? NO_SIGNS,
  })
  return { signs: query.data ?? NO_SIGNS, loading: query.isLoading, error: query.error?.message ?? null }
}

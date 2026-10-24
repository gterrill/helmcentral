import { useApiQuery } from '@/lib/api-query'
import { INSET_VIEW_KEY, insetViewPath, toInsetView, type WireInsetView } from '@/lib/inset-view-api'

/** The saved overview corner and zoom step for the replay covering the range,
 * or null until the server has answered (or when it could not). Disabled with
 * no replay. `refetch` is the retry after a failed read. */
export function useInsetView(range: { from: number; to: number } | null) {
  const enabled = range !== null
  const from = range ? Math.round(range.from) : null
  const to = range ? Math.round(range.to) : null
  const query = useApiQuery({
    queryKey: [...INSET_VIEW_KEY, from, to],
    path: enabled ? insetViewPath(from as number, to as number) : '',
    enabled,
    select: (raw: { inset_view: WireInsetView }) => toInsetView(raw),
  })
  return {
    view: query.data ?? null,
    loading: enabled && query.isLoading,
    error: query.error?.message ?? null,
    refetch: query.refetch,
  }
}

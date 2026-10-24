import { useApiQuery } from '@/lib/api-query'
import { INSET_VIEW_KEY, insetViewPath, toInsetView } from '@/lib/inset-view-api'
import type { InsetView } from '@/lib/passage-inset-view'

/** The saved overview corner and zoom step for the replay that starts at the
 * instant, or null until the server has answered. Disabled with no replay. */
export function useInsetView(atMs: number | null) {
  const enabled = atMs !== null
  const at = atMs === null ? null : Math.round(atMs)
  const query = useApiQuery({
    queryKey: [...INSET_VIEW_KEY, at],
    path: enabled ? insetViewPath(at as number) : '',
    enabled,
    select: (raw: { inset_view: { at_ms: number; corner: InsetView['corner']; zoom_offset: number } }) => toInsetView(raw),
  })
  return { view: query.data ?? null, loading: enabled && query.isLoading, error: query.error?.message ?? null }
}

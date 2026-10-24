import { apiRequest, setQueryData } from '@/lib/api-query'
import type { InsetCorner, InsetView } from '@/lib/passage-inset-view'

// The overview map's corner and zoom step for a replay (ADR 0209). Keyed by the
// replay's start instant, like camera shots are keyed by time. A refused write
// throws with the server's own words; nothing retries or substitutes.

export const INSET_VIEW_KEY = ['passage-inset-view'] as const

const PATH = '/api/passages/inset-view'

export const insetViewPath = (atMs: number) => `${PATH}?at=${Math.round(atMs)}`

interface WireInsetView { at_ms: number; corner: InsetCorner; zoom_offset: number }

export const toInsetView = (raw: { inset_view: WireInsetView }): InsetView => ({
  corner: raw.inset_view.corner,
  zoomOffset: raw.inset_view.zoom_offset,
})

export const insetViewApi = {
  save: async (atMs: number, view: InsetView): Promise<InsetView> => {
    const raw = await apiRequest<{ inset_view: WireInsetView }>(PATH, {
      method: 'PUT',
      json: { at_ms: Math.round(atMs), corner: view.corner, zoom_offset: view.zoomOffset },
    })
    // The answer is what is stored, so the list shows it without another read.
    setQueryData([...INSET_VIEW_KEY, Math.round(atMs)], raw)
    return toInsetView(raw)
  },
}

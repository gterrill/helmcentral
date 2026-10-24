import { apiRequest, setQueryData } from '@/lib/api-query'
import type { InsetCorner, InsetView } from '@/lib/passage-inset-view'

// The overview map's corner and zoom step for a replay (ADR 0209). Keyed by the
// replay's range, from the first departure to the last arrival. A refused write
// throws with the server's own words; nothing retries or substitutes.

export const INSET_VIEW_KEY = ['passage-inset-view'] as const

const PATH = '/api/passages/inset-view'

export const insetViewPath = (fromMs: number, toMs: number) => `${PATH}?from=${Math.round(fromMs)}&to=${Math.round(toMs)}`

export interface WireInsetView { from_ms: number; to_ms: number; corner: InsetCorner; zoom_offset: number }

export const toInsetView = (raw: { inset_view: WireInsetView }): InsetView => ({
  corner: raw.inset_view.corner,
  zoomOffset: raw.inset_view.zoom_offset,
})

export const insetViewApi = {
  save: async (fromMs: number, toMs: number, view: InsetView): Promise<InsetView> => {
    const from = Math.round(fromMs)
    const to = Math.round(toMs)
    const raw = await apiRequest<{ inset_view: WireInsetView }>(PATH, {
      method: 'PUT',
      json: { from_ms: from, to_ms: to, corner: view.corner, zoom_offset: view.zoomOffset },
    })
    // The answer is what is stored, so readers show it without another read.
    setQueryData([...INSET_VIEW_KEY, from, to], raw)
    return toInsetView(raw)
  },
}

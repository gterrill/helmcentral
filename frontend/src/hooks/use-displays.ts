import { errorMessage } from '@/lib/errors'
import { useCallback } from 'react'
import { toast } from 'sonner'
import type { Display } from '@/lib/displays'
import { apiRequest, declareQuery, invalidateQueries, setQueryData, useApiQuery } from '@/lib/api-query'

// Modelled directly on use-dashboard-pages.ts (ADR 0110 §6): same
// toast/error-state shape, for the same reasons.

interface DisplaysListResponse {
  displays?: Display[]
}

interface DeleteDisplayResponse {
  status?: string
  released_page_ids?: string[]
}

/** Every field the create/patch endpoints accept, all optional: a create
 * needs at least `name`, a patch can touch just one field. The backend does
 * the real validation (slug format/uniqueness, width/height/scale bounds,
 * rotation) - this hook only shapes and sends the request. */
export type DisplayFields = Partial<
  Pick<Display, 'name' | 'slug' | 'width' | 'height' | 'scale' | 'rotate' | 'pixel_shift' | 'wake_lock'>
>

const DISPLAYS_KEY = ['/api/displays'] as const
const EMPTY_DISPLAYS: Display[] = []

// The cache holds the raw list response, so patches keep its shape.
function patchDisplays(fn: (displays: Display[]) => Display[]) {
  setQueryData<DisplaysListResponse>(DISPLAYS_KEY, (prev) => ({
    ...prev,
    displays: fn(Array.isArray(prev?.displays) ? prev.displays : []),
  }))
}

declareQuery(DISPLAYS_KEY, { structuralSharing: true })

export function useDisplays() {
  const { data, isLoading, error, refetch } = useApiQuery<Display[]>({
    queryKey: DISPLAYS_KEY,
    path: '/api/displays',
    select: (raw: DisplaysListResponse) => (Array.isArray(raw.displays) ? raw.displays : []),
  })
  const displays = data ?? EMPTY_DISPLAYS

  const createDisplay = useCallback(async (fields: DisplayFields & { name: string }): Promise<Display | null> => {
    try {
      const display = await apiRequest<Display>('/api/displays', { method: 'POST', json: fields })
      patchDisplays((prev) => [...prev, display])
      return display
    } catch (err) {
      toast.error('Could not create display', { description: errorMessage(err) })
      return null
    }
  }, [])

  const updateDisplay = useCallback(async (id: string, patch: DisplayFields): Promise<Display | null> => {
    // On failure we deliberately do NOT surface `error` above: that's
    // reserved for the initial display-list load failure. Whatever gates a
    // screen on `!displaysError` would otherwise blank itself on something
    // as routine as a failed field edit; a toast surfaces the problem
    // without doing that (use-dashboard-pages.ts does the same for `updatePage`).
    try {
      const display = await apiRequest<Display>(`/api/displays/${id}`, { method: 'PATCH', json: patch })
      patchDisplays((prev) => prev.map((d) => (d.id === id ? display : d)))
      return display
    } catch (err) {
      toast.error('Could not save display', { description: errorMessage(err) })
      return null
    }
  }, [])

  /**
   * Deleting a display releases its pages rather than deleting them (ADR
   * 0110 §3): the server clears `display_id` on every page that referenced
   * it in the same locked write and reports their ids back. The page list
   * is invalidated here so the released pages return to the Dashboard list
   * without the caller wiring a refetch. Returns null on failure, matching
   * createDisplay/updateDisplay above.
   */
  const deleteDisplay = useCallback(async (id: string): Promise<string[] | null> => {
    try {
      const data = await apiRequest<DeleteDisplayResponse>(`/api/displays/${id}`, { method: 'DELETE' })
      patchDisplays((prev) => prev.filter((d) => d.id !== id))
      invalidateQueries(['/api/dashboard-pages'])
      return Array.isArray(data.released_page_ids) ? data.released_page_ids : []
    } catch (err) {
      toast.error('Could not delete display', { description: errorMessage(err) })
      return null
    }
  }, [])

  return {
    displays,
    loading: isLoading,
    error: error?.message ?? null,
    refetch,
    createDisplay,
    updateDisplay,
    deleteDisplay,
  }
}

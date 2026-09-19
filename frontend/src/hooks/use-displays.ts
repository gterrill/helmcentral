import { useCallback, useEffect, useState } from 'react'
import { toast } from 'sonner'
import type { Display } from '@/lib/displays'
import { readErrorMessage } from '@/lib/api-error'

// Modelled directly on use-dashboard-pages.ts (ADR 0110 §6): same fetch/
// toast/error-state shape, for the same reasons - see that file for the
// longer version of the comments below.

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

export function useDisplays() {
  const [displays, setDisplays] = useState<Display[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const fetchDisplays = useCallback(async () => {
    try {
      setLoading(true)
      const res = await fetch('/api/displays')
      if (!res.ok) {
        throw new Error(`HTTP error! status: ${res.status}`)
      }
      const data = (await res.json()) as DisplaysListResponse
      setDisplays(Array.isArray(data.displays) ? data.displays : [])
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load displays')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void fetchDisplays()
  }, [fetchDisplays])

  const createDisplay = useCallback(async (fields: DisplayFields & { name: string }): Promise<Display | null> => {
    const res = await fetch('/api/displays', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(fields),
    })
    if (!res.ok) {
      const message = await readErrorMessage(res)
      toast.error('Could not create display', { description: message })
      return null
    }
    const display = (await res.json()) as Display
    setDisplays((prev) => [...prev, display])
    return display
  }, [])

  const updateDisplay = useCallback(async (id: string, patch: DisplayFields): Promise<Display | null> => {
    const res = await fetch(`/api/displays/${id}`, {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(patch),
    })
    // Note: on failure we deliberately do NOT set the `error` state above —
    // that's reserved for the initial display-list load failure
    // (fetchDisplays). Whatever gates a screen on `!displaysError` would
    // otherwise blank itself on something as routine as a failed field edit;
    // a toast surfaces the problem without doing that (use-dashboard-pages.ts
    // makes the identical call for `updatePage`, for the identical reason).
    if (!res.ok) {
      const message = await readErrorMessage(res)
      toast.error('Could not save display', { description: message })
      return null
    }
    const display = (await res.json()) as Display
    setDisplays((prev) => prev.map((d) => (d.id === id ? display : d)))
    return display
  }, [])

  /**
   * Deleting a display releases its pages rather than deleting them (ADR
   * 0110 §3): the server clears `display_id` on every page that referenced
   * it in the same locked write and reports their ids back. Returning those
   * ids lets the caller (the displays dialog / App.tsx) drop the released
   * pages back into the Dashboard list without a full page-list refetch.
   * Returns null on failure, matching createDisplay/updateDisplay above.
   */
  const deleteDisplay = useCallback(async (id: string): Promise<string[] | null> => {
    const res = await fetch(`/api/displays/${id}`, { method: 'DELETE' })
    if (!res.ok) {
      const message = await readErrorMessage(res)
      toast.error('Could not delete display', { description: message })
      return null
    }
    const data = (await res.json()) as DeleteDisplayResponse
    setDisplays((prev) => prev.filter((d) => d.id !== id))
    return Array.isArray(data.released_page_ids) ? data.released_page_ids : []
  }, [])

  return { displays, loading, error, refetch: fetchDisplays, createDisplay, updateDisplay, deleteDisplay }
}

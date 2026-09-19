import { useCallback, useEffect, useState } from 'react'

import { apiBaseUrl } from '@/config/api'

// ADR 0095: one page of the embedded help, as
// backend/help_handlers.go's GET /api/help/<id> returns it.
export interface HelpPage {
  id: string
  title: string
  body: string
}

export interface HelpPageError {
  status: number
  message: string
}

export interface UseHelpPageResult {
  page: HelpPage | null
  loading: boolean
  error: HelpPageError | null
  reload: () => void
}

// The help is embedded in the binary at build time and cannot change
// without a new one - the same reasoning use-app-version.ts gives for
// probing /api/health once per page load. Caching per id, module-level
// rather than per-hook-instance, is what makes the Help sheet's Back
// button (re-selecting a page already visited this session) instant rather
// than re-fetching a page that cannot have changed underneath it.
const pageCache = new Map<string, HelpPage>()

/**
 * Fetches one help page by id (e.g. "features/forecast", or "index" for
 * the contents page) from `GET /api/help/<id>`. `id === null` means
 * "nothing to load" - the Help sheet passes this while closed, so it
 * fetches nothing before the operator has ever opened it.
 *
 * A non-2xx response's body is read as `{error: string}` so the backend's
 * own sentence (the 503 "help is not embedded in this build" message,
 * or the 404 "unknown help page" message) reaches the sheet verbatim,
 * the same pattern use-assistant-status.ts uses for its own status probe.
 * A response with no readable JSON body, or a network failure that never
 * got a response at all, falls back to `HTTP <status>` or the raw fetch
 * exception message respectively - never a synthesized explanation.
 */
export function useHelpPage(id: string | null): UseHelpPageResult {
  const [page, setPage] = useState<HelpPage | null>(id !== null ? pageCache.get(id) ?? null : null)
  const [loading, setLoading] = useState(id !== null && !pageCache.has(id))
  const [error, setError] = useState<HelpPageError | null>(null)
  const [reloadToken, setReloadToken] = useState(0)

  useEffect(() => {
    if (id === null) {
      setPage(null)
      setLoading(false)
      setError(null)
      return
    }

    const cached = pageCache.get(id)
    if (cached && reloadToken === 0) {
      setPage(cached)
      setLoading(false)
      setError(null)
      return
    }

    let cancelled = false
    setLoading(true)
    setError(null)
    setPage(null)

    void (async () => {
      try {
        const response = await fetch(`${apiBaseUrl}/api/help/${id}`)
        if (!response.ok) {
          const body = (await response.json().catch(() => null)) as { error?: unknown } | null
          const message = body && typeof body.error === 'string' ? body.error : `HTTP ${response.status}`
          if (!cancelled) setError({ status: response.status, message })
          return
        }
        const data = (await response.json()) as HelpPage
        pageCache.set(id, data)
        if (!cancelled) setPage(data)
      } catch (err) {
        if (!cancelled) setError({ status: 0, message: err instanceof Error ? err.message : String(err) })
      } finally {
        if (!cancelled) setLoading(false)
      }
    })()

    return () => {
      cancelled = true
    }
  }, [id, reloadToken])

  const reload = useCallback(() => setReloadToken((token) => token + 1), [])

  return { page, loading, error, reload }
}

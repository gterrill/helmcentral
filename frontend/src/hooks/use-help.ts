import { useCallback } from 'react'

import { ApiError, useApiQuery } from '@/lib/api-query'

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

/**
 * Fetches one help page by id (e.g. "features/forecast", or "index" for
 * the contents page) from `GET /api/help/<id>`. `id === null` means
 * "nothing to load" - the Help sheet passes this while closed, so it
 * fetches nothing before the operator has ever opened it.
 *
 * A non-2xx response's `{error: string}` body reaches the sheet verbatim
 * as `message` (the 503 "help is not embedded in this build" sentence, or
 * the 404 "unknown help page" one). A response with no readable JSON body
 * falls back to `HTTP <status>`, and a network failure that never got a
 * response at all to the raw fetch exception message with status 0 - never
 * a synthesized explanation.
 */
export function useHelpPage(id: string | null): UseHelpPageResult {
  // The help is embedded in the binary and cannot change without a new one,
  // so the shared copy is what makes the Help sheet's Back button
  // (re-selecting a page already visited) show at once; the background
  // revalidation behind it costs nothing visible.
  const path = `/api/help/${id ?? ''}`
  const query = useApiQuery<HelpPage>({ queryKey: [path], path, enabled: id !== null })
  const { refetch } = query
  const reload = useCallback(() => { void refetch() }, [refetch])

  const error: HelpPageError | null = query.error
    ? { status: query.error instanceof ApiError ? query.error.status : 0, message: query.error.message }
    : null
  return { page: error ? null : (query.data ?? null), loading: query.isLoading, error, reload }
}

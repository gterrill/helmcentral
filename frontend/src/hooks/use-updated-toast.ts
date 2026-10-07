import { useEffect } from 'react'
import { toast } from 'sonner'

import { UPDATED_TO_KEY } from '@/hooks/use-version-reload'
import { releaseNotesUrl } from '@/lib/help-links'

const TOAST_DURATION_MS = 10_000

/**
 * Tells the operator which version Helmcentral is now running after
 * useVersionReload reloaded the page on its own, so an update that happens
 * while nobody is watching is not silent. The marker is removed as it is read,
 * so the message shows once: a manual refresh, or React's second StrictMode
 * effect pass, finds nothing. A malformed marker is discarded without a toast.
 */
export function useUpdatedToast(): void {
  useEffect(() => {
    let raw: string | null = null
    try {
      raw = sessionStorage.getItem(UPDATED_TO_KEY)
      if (raw !== null) sessionStorage.removeItem(UPDATED_TO_KEY)
    } catch (err) {
      console.warn('Could not read the update marker', err)
      return
    }
    if (raw === null) return

    let to: unknown
    try {
      to = (JSON.parse(raw) as { to?: unknown }).to
    } catch {
      return
    }
    if (typeof to !== 'string' || to === '') return

    const url = releaseNotesUrl(to)
    toast(`Helmcentral updated to ${to}`, {
      duration: TOAST_DURATION_MS,
      action: url ? { label: 'Release notes', onClick: () => window.open(url, '_blank', 'noopener') } : undefined,
    })
  }, [])
}

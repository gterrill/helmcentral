import { useEffect } from 'react'
import { toast } from 'sonner'

import type { AppVersion } from '@/hooks/use-app-version'
import { releaseNotesUrl } from '@/lib/help-links'
import { recordSeenVersion } from '@/lib/seen-version'

const TOAST_DURATION_MS = 10_000

/**
 * Tells the operator which version Helmcentral is now running when this
 * browser last showed a different one, however the new page came about: the
 * Reload button, a manual refresh, a new tab or a Home Screen relaunch. The
 * comparison is against the version this browser remembers (see
 * recordSeenVersion), and waits for the first successful health read.
 */
export function useUpdatedToast(current: AppVersion | null): void {
  const version = current?.version ?? null
  useEffect(() => {
    if (version === null) return
    const to = recordSeenVersion(version)
    if (to === null) return

    const url = releaseNotesUrl(to)
    toast(`Helmcentral updated to ${to}`, {
      duration: TOAST_DURATION_MS,
      action: url ? { label: 'Release notes', onClick: () => window.open(url, '_blank', 'noopener') } : undefined,
    })
  }, [version])
}

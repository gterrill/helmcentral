import { toast } from 'sonner'

import { useVersionAnnouncement } from '@/hooks/use-version-announcement'
import type { AppVersion } from '@/lib/build-stamp'
import { releaseNotesUrl } from '@/lib/help-links'

const TOAST_DURATION_MS = 10_000

/**
 * Tells the operator which version Helmcentral is now running when this
 * browser last showed a different one, however the new page came about: the
 * Reload button, a manual refresh, a new tab or a Home Screen relaunch.
 */
export function useUpdatedToast(current: AppVersion | null): void {
  useVersionAnnouncement(current, (to) => {
    const url = releaseNotesUrl(to)
    toast(`Helmcentral updated to ${to}`, {
      duration: TOAST_DURATION_MS,
      action: url ? { label: 'Release notes', onClick: () => window.open(url, '_blank', 'noopener') } : undefined,
    })
  })
}

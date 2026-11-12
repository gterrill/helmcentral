import { lazy, memo, Suspense, useState } from 'react'

import { runningFromHomeScreen } from '@/hooks/use-screen-wake-lock'

const InboxWaitingBanner = lazy(() => import('@/components/inbox-waiting-banner').then((mod) => ({ default: mod.InboxWaitingBanner })))

export interface InboxBannerSlotProps {
  canWrite: boolean
  /** Opens the upload page. Keep it stable, or the slot draws on every parent draw. */
  onUploadNow: () => void
}

/**
 * Where the waiting banner goes. Shares arrive only at the installed app, so a
 * kiosk or a browser tab shows nothing here and loads neither the banner nor
 * the waiting list's database (ADR 0280). The parent redraws on every
 * telemetry tick, so the slot draws only when its props change.
 */
export const InboxBannerSlot = memo(function InboxBannerSlot({ canWrite, onUploadNow }: InboxBannerSlotProps) {
  // The display mode does not change while the app is open.
  const [installed] = useState(runningFromHomeScreen)
  if (!installed) return null
  return (
    <Suspense fallback={null}>
      <InboxWaitingBanner canWrite={canWrite} onUploadNow={onUploadNow} />
    </Suspense>
  )
})

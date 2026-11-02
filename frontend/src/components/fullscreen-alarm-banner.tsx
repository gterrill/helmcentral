import { createContext, useContext } from 'react'

import { LiveAlarmBanner } from '@/components/live-alarm-banner'
import { useAppRouter, useNavigate } from '@/hooks/use-app-location'

/**
 * Closes the overlay a full-screen view sits in, when that overlay would
 * otherwise cover the page Alarms opens (the Mate sheet). A no-op elsewhere.
 */
export const OverlayCloseContext = createContext<() => void>(() => {})

// A full-screen view covers the app's own alarm banner, so it shows one at the
// top while it is up. Render it as a child, so the router and alarm hooks only
// run in full screen. Opening Alarms closes the full-screen view and any
// overlay beneath it, but only once the move has been made: a page with
// unsaved edits holds it behind the navigation guard's question, and choosing
// to stay must leave everything open. If the operator chooses to leave, the
// move lands later, and the views close then.
export function FullscreenAlarmBanner({ onClose }: { onClose: () => void }) {
  const router = useAppRouter()
  const navigate = useNavigate()
  const closeOverlay = useContext(OverlayCloseContext)
  const open = () => {
    const close = () => {
      onClose()
      closeOverlay()
    }
    if (navigate.toPanel('alarms')) {
      close()
      return
    }
    // Held by a guard. Wait for its answer: the move lands (close) or the
    // question is dropped (leave everything as it was).
    const stop = router.subscribe(() => {
      const { location, pending } = router.getSnapshot()
      if (location.panel === 'alarms') {
        stop()
        close()
      } else if (pending === null) {
        stop()
      }
    })
  }
  return <LiveAlarmBanner onOpen={open} />
}

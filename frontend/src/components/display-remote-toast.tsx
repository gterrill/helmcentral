import { useEffect, useRef, useState } from 'react'
import { cn } from '@/lib/utils'

export interface DisplayRemoteToastProps {
  /** Bump this (e.g. a counter) each time a remote key should flash a new
   * message - a step, a pause, or a resume. The toast reacts only to this
   * changing, not to `position`/`paused`/`pageName` changing on their own,
   * so an ordinary dwell-driven page change (which also changes `position`)
   * never triggers it - only an actual remote action does, at the caller's
   * discretion (App.tsx wraps useDisplayRotation's next/previous/pause/
   * resume to bump this alongside calling them). */
  announcementId: number
  pageName: string
  position: { index: number; total: number }
  paused: boolean
}

const FADE_AFTER_MS = 2000

/**
 * A momentary confirmation that a remote key landed (ADR 0110 §5b) -
 * "Paused · Engines (2 of 5)" while paused, "Engines (3 of 5)" on a step -
 * fading out on its own rather than needing another key to dismiss it, since
 * a wall has no cursor to click a dismiss button with.
 *
 * Rendered inside display-shell.tsx's inner box (as part of the `children`
 * App.tsx passes it) so it rotates, scales and pixel-shifts with the rest of
 * the board rather than sitting fixed in a corner the upside-down strip
 * would show wrong-way-up.
 */
export function DisplayRemoteToast({ announcementId, pageName, position, paused }: DisplayRemoteToastProps) {
  const [visible, setVisible] = useState(false)
  // The effect below runs once on mount with the initial announcementId,
  // which is not a remote action: without this guard every wall boot flashes
  // the toast for two seconds, reading "(1 of 0)" while the feed is still
  // resolving, with nobody having touched a remote.
  const seenFirst = useRef(false)

  useEffect(() => {
    if (!seenFirst.current) {
      seenFirst.current = true
      return
    }
    setVisible(true)
    const id = setTimeout(() => setVisible(false), FADE_AFTER_MS)
    return () => clearTimeout(id)
    // Deliberately only `announcementId` - see this prop's own doc comment.
  }, [announcementId])

  const label = paused
    ? `Paused · ${pageName} (${position.index + 1} of ${position.total})`
    : `${pageName} (${position.index + 1} of ${position.total})`

  return (
    <div
      data-testid="display-remote-toast"
      aria-hidden={!visible}
      className={cn(
        'pointer-events-none fixed left-1/2 top-4 z-20 -translate-x-1/2 rounded-md border border-border bg-background/90 px-3 py-1.5 text-xs font-semibold uppercase tracking-[0.08em] text-foreground shadow-md transition-opacity duration-500',
        visible ? 'opacity-100' : 'opacity-0',
      )}
    >
      {label}
    </div>
  )
}

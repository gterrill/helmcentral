import { useEffect, useState } from 'react'

import { useVersionAnnouncement } from '@/hooks/use-version-announcement'
import type { AppVersion } from '@/lib/build-stamp'
import { cn } from '@/lib/utils'

const SHOW_FOR_MS = 8000

/**
 * A brief, non-interactive "Updated to vX" line for the wall display, which
 * has no Toaster and no cursor. Sits in DisplayShell's overlay box, so it
 * rotates with the board.
 */
export function DisplayUpdatedNotice({ current }: { current: AppVersion | null }) {
  const version = useVersionAnnouncement(current)
  const [visible, setVisible] = useState(false)

  useEffect(() => {
    if (version !== null) setVisible(true)
  }, [version])

  useEffect(() => {
    if (!visible) return
    const id = setTimeout(() => setVisible(false), SHOW_FOR_MS)
    return () => clearTimeout(id)
  }, [visible])

  if (version === null) return null
  return (
    <div
      data-testid="display-updated-notice"
      aria-hidden={!visible}
      className={cn(
        'pointer-events-none fixed bottom-4 left-1/2 z-20 -translate-x-1/2 rounded-md border border-border bg-background/90 px-3 py-1.5 text-xs font-semibold uppercase tracking-[0.08em] text-foreground shadow-md transition-opacity duration-500',
        visible ? 'opacity-100' : 'opacity-0',
      )}
    >
      Updated to {version}
    </div>
  )
}

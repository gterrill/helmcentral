import { useEffect, useState } from 'react'

import type { AppVersion } from '@/hooks/use-app-version'
import { recordSeenVersion } from '@/lib/seen-version'
import { cn } from '@/lib/utils'

const SHOW_FOR_MS = 8000

/**
 * A brief, non-interactive "Updated to vX" line for the wall display, which
 * has no Toaster and no cursor. Sits in DisplayShell's overlay box, so it
 * rotates with the board.
 */
export function DisplayUpdatedNotice({ current }: { current: AppVersion | null }) {
  const [version, setVersion] = useState<string | null>(null)
  const [visible, setVisible] = useState(false)
  const running = current?.version ?? null

  useEffect(() => {
    if (running === null) return
    const to = recordSeenVersion(running)
    if (to === null) return
    setVersion(to)
    setVisible(true)
  }, [running])

  // A separate effect, so a StrictMode remount of the one above (which finds
  // the version already recorded) cannot cancel the timer and leave it showing.
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

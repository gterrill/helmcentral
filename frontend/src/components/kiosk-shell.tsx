import { useEffect, type CSSProperties, type ReactNode } from 'react'
import { KioskStatusBadge } from '@/components/kiosk-status-badge'
import type { ActiveAlarm } from '@/hooks/use-alarms'

interface KioskShellProps {
  rotate: 0 | 180
  alarms: ActiveAlarm[]
  children: ReactNode
}

/**
 * The whole screen at /kiosk (ADR 0089): no sidebar, no header, no
 * SidebarProvider — App.tsx renders this in place of the ordinary shell
 * when on the kiosk route, reusing dashboardGrid as its only content.
 *
 * `fixed inset-0` claims the entire viewport regardless of what document
 * flow looks like above it. The 1920x360 strip is physically mounted upside
 * down, so `?rotate=180` (lib/kiosk.ts's parseKioskOptions) rotates the
 * whole root with a centred transform — everything inside it, content and
 * KioskStatusBadge alike, flips together and reads right way up to someone
 * looking at the inverted screen.
 */
export function KioskShell({ rotate, alarms, children }: KioskShellProps) {
  // A kiosk has no pointer and nothing to click: a stray cursor left over
  // from whatever set up the browser would otherwise sit on screen forever.
  // Restored on unmount so leaving /kiosk (e.g. the authoring preview) does
  // not strand the rest of the app with no cursor.
  useEffect(() => {
    const root = document.documentElement
    const previousCursor = root.style.cursor
    const previousOverflow = document.body.style.overflow
    root.style.cursor = 'none'
    document.body.style.overflow = 'hidden'
    return () => {
      root.style.cursor = previousCursor
      document.body.style.overflow = previousOverflow
    }
  }, [])

  const style: CSSProperties = {
    transformOrigin: 'center center',
    // The instrument skin's board padding/radius exist for a tile sitting
    // inside the app shell's own chrome. The kiosk root IS the screen, so
    // both are zeroed here rather than letting the skin eat into the
    // already-tight 344px fold budget (lib/kiosk.ts's KIOSK_FOLD_PX).
    ['--board-pad' as string]: '0px',
    ['--board-radius' as string]: '0px',
  }
  if (rotate === 180) {
    style.transform = 'rotate(180deg)'
  }

  return (
    <div data-testid="kiosk-root" data-rotate={rotate} className="fixed inset-0 overflow-hidden bg-background p-2" style={style}>
      {children}
      <KioskStatusBadge alarms={alarms} />
    </div>
  )
}

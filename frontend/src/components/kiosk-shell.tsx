import { useEffect, type CSSProperties, type ReactNode } from 'react'
import { KioskStatusBadge } from '@/components/kiosk-status-badge'
import type { ActiveAlarm } from '@/hooks/use-alarms'

interface KioskShellProps {
  rotate: 0 | 180
  /**
   * `?height=<px>` (lib/kiosk.ts's parseKioskOptions), for a kiosk browser
   * whose framebuffer is taller than the physical panel. Null means the
   * full viewport, the behaviour before this option existed. Optional so
   * callers that never pass it (this component's own unit tests) keep
   * working unchanged.
   */
  height?: number | null
  alarms: ActiveAlarm[]
  children: ReactNode
}

/**
 * The whole screen at /kiosk (ADR 0089): no sidebar, no header, no
 * SidebarProvider — App.tsx renders this in place of the ordinary shell
 * when on the kiosk route, reusing dashboardGrid as its only content.
 *
 * With no `height`, `fixed inset-0` claims the entire viewport regardless of
 * what document flow looks like above it. A kiosk browser can report a
 * framebuffer taller than the panel it drives (a WPE-webkit build reporting
 * 1920x1080 on a 1920x360 strip); `height` then constrains this same box to
 * that many pixels at the top of the viewport instead, so the visible band
 * is the only part of the page that ever lays out content. The 1920x360
 * strip is physically mounted upside down, so `?rotate=180` rotates
 * whichever box this is (full viewport or height-constrained) with a
 * centred transform — it rotates in place, about its own centre, rather
 * than about the framebuffer's — so everything inside it, content and
 * KioskStatusBadge alike, flips together and reads right way up to someone
 * looking at the inverted screen.
 */
export function KioskShell({ rotate, height = null, alarms, children }: KioskShellProps) {
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
  if (height !== null) {
    style.top = 0
    style.left = 0
    style.width = '100vw'
    style.height = `${height}px`
  }

  const positionClassName = height !== null ? '' : ' inset-0'

  return (
    <div
      data-testid="kiosk-root"
      data-rotate={rotate}
      className={`fixed${positionClassName} overflow-hidden bg-background p-2`}
      style={style}
    >
      {children}
      <KioskStatusBadge alarms={alarms} />
    </div>
  )
}

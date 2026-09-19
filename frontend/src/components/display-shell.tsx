import { useEffect, type CSSProperties, type ReactNode } from 'react'
import { DisplayStatusBadge } from '@/components/display-status-badge'
import { usePixelShift } from '@/hooks/use-pixel-shift'
import { useScreenWakeLock } from '@/hooks/use-screen-wake-lock'
import { displayScale, type Display } from '@/lib/displays'
import type { ActiveAlarm } from '@/hooks/use-alarms'

interface DisplayShellProps {
  display: Display
  alarms: ActiveAlarm[]
  children: ReactNode
}

// A Magic Remote shows a pointer when waved; a screen that swallows it reads
// as frozen. The flybridge ODROID has no pointer device at all, so hiding
// the cursor only after a few seconds of no movement is correct on both: an
// active remote never has its pointer yanked away, and it disappears on its
// own where there was never anything to move it in the first place.
const CURSOR_IDLE_MS = 3000

/**
 * The whole screen at /display/<slug> (ADR 0110, superseding ADR 0089's
 * /kiosk): no sidebar, no header, no SidebarProvider — App.tsx renders this
 * in place of the ordinary shell when on the wall route, reusing
 * dashboardGrid as its only content.
 *
 * Two nested boxes, so rotation, scale and the pixel shift compose without
 * fighting each other:
 *  - The OUTER box sits at the viewport's top-left, sized to the *physical*
 *    footprint the display occupies on the wall (width*scale x
 *    height*scale), and carries the 180-degree rotation about its own
 *    centre — a 1920x360 strip mounted upside down needs its whole rotated
 *    footprint to still land inside the viewport it was measured against.
 *  - The INNER box is the *logical* canvas a page is authored against
 *    (width x height, in CSS px), transform-origin top-left, magnified and
 *    pixel-shifted by a single `transform`.
 *
 * `transform` rather than CSS `zoom` is load-bearing: react-grid-layout's
 * WidthProvider measures this box's *layout* width (`offsetWidth`), which a
 * `transform` leaves alone and `zoom` would not — that's what keeps the
 * 12-column grid authored against the logical canvas rather than the
 * on-screen, scaled one.
 *
 * A display with both width and height 0 means "whatever this browser
 * reports": the outer box claims the entire viewport (the pre-ADR-0110
 * `fixed inset-0` kiosk root's own behaviour) and no scaling is applied,
 * regardless of what the record's own `scale` field says — the backend
 * rejects a non-1.0 scale on a zero canvas, but this component doesn't lean
 * on that invariant holding for a record it didn't itself validate.
 */
export function DisplayShell({ display, alarms, children }: DisplayShellProps) {
  const { dx, dy } = usePixelShift(display.pixel_shift)
  const { status: wakeLockStatus } = useScreenWakeLock(display.wake_lock)

  const isFullViewport = display.width === 0 && display.height === 0
  const scale = isFullViewport ? 1 : displayScale(display)

  // A wall has no pointer and nothing to click, but a Magic Remote's cursor
  // (webOS's air-mouse pointer) has to keep working while it's actually
  // moving. Restored on unmount so leaving the wall route (e.g. the
  // authoring preview) never strands the rest of the app with no cursor.
  useEffect(() => {
    const root = document.documentElement
    const previousCursor = root.style.cursor
    const previousOverflow = document.body.style.overflow
    document.body.style.overflow = 'hidden'

    let hideTimer: ReturnType<typeof setTimeout> | null = null
    function hideCursor() {
      root.style.cursor = 'none'
    }
    function scheduleHide() {
      if (hideTimer !== null) clearTimeout(hideTimer)
      hideTimer = setTimeout(hideCursor, CURSOR_IDLE_MS)
    }
    function handlePointerMove() {
      root.style.cursor = ''
      scheduleHide()
    }

    scheduleHide()
    window.addEventListener('pointermove', handlePointerMove)
    return () => {
      window.removeEventListener('pointermove', handlePointerMove)
      if (hideTimer !== null) clearTimeout(hideTimer)
      root.style.cursor = previousCursor
      document.body.style.overflow = previousOverflow
    }
  }, [])

  const outerStyle: CSSProperties = {
    top: 0,
    left: 0,
    transformOrigin: 'center center',
  }
  if (display.rotate === 180) {
    outerStyle.transform = 'rotate(180deg)'
  }
  if (isFullViewport) {
    outerStyle.width = '100vw'
    outerStyle.height = '100vh'
  } else {
    outerStyle.width = `${display.width * scale}px`
    outerStyle.height = `${display.height * scale}px`
  }

  const innerStyle: CSSProperties = {
    transformOrigin: 'top left',
    transform: `scale(${scale}) translate(${dx}px, ${dy}px)`,
    // The instrument skin's board padding/radius exist for a tile sitting
    // inside the app shell's own chrome. The display root IS the screen, so
    // both are zeroed here rather than letting the skin eat into the
    // already-tight fold budget (lib/displays.ts's displayFoldPx).
    ['--board-pad' as string]: '0px',
    ['--board-radius' as string]: '0px',
  }
  if (isFullViewport) {
    innerStyle.width = '100%'
    innerStyle.height = '100%'
  } else {
    innerStyle.width = `${display.width}px`
    innerStyle.height = `${display.height}px`
  }

  return (
    <div
      data-testid="display-shell-outer"
      data-rotate={display.rotate}
      className="fixed overflow-hidden bg-background"
      style={outerStyle}
    >
      <div data-testid="display-shell-inner" className="relative p-1" style={innerStyle}>
        {children}
        <DisplayStatusBadge alarms={alarms} wakeLockStatus={wakeLockStatus} />
      </div>
    </div>
  )
}

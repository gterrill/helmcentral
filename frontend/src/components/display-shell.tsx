import { createContext, useEffect, useMemo, useState, type CSSProperties, type ReactNode } from 'react'
import { DisplayStatusBadge } from '@/components/display-status-badge'
import { usePixelShift } from '@/hooks/use-pixel-shift'
import { useScreenWakeLock } from '@/hooks/use-screen-wake-lock'
import { displayScale, type Display } from '@/lib/displays'
import type { ActiveAlarm } from '@/hooks/use-alarms'

interface DisplayShellProps {
  display: Display
  alarms: ActiveAlarm[]
  children: ReactNode
  /** Rendered in the overlay layer (see this component's own doc comment)
   * rather than as an ordinary child - for anything that's itself
   * `position: fixed` and needs to stay painted above a portalled embed
   * iframe rather than trapped behind it. App.tsx passes DisplayRemoteToast
   * this way; DisplayStatusBadge lives there unconditionally, below. */
  overlay?: ReactNode
}

/** This shell's own rotation/scale/pixel-shift, as DisplayTransformContext
 * broadcasts it. `rotate` is narrowed to what the outer box actually applies
 * (see DisplayShell's doc comment) rather than carrying the display record's
 * own `0 | 180` type, so a descendant reading this never has to re-derive
 * that narrowing itself. */
export interface DisplayTransform {
  rotate: 0 | 180
  scale: number
  dx: number
  dy: number
  /** The outer box's own DOM node (`display-shell-outer`), exposed so an
   * embed's portal can crop itself to the display's actual on-screen
   * footprint - `overflow-hidden` on that box clips everything painted
   * inside it, but a portalled iframe lives at `document.body` and escapes
   * that clip entirely. Set via a callback ref once the box actually
   * mounts (DisplayShell's own render), so this is `null` for exactly the
   * first render of a fresh DisplayShell and the real element for the rest
   * of its life. */
  clipElement: HTMLElement | null
}

/**
 * This shell's own transform, broadcast to descendants that need to know
 * they're painting inside a rotated/scaled/pixel-shifted ancestor chain.
 * Null outside a DisplayShell, so nothing reads into it by accident on the
 * ordinary (non-wall) dashboard.
 *
 * The one consumer today is EmbedTile: WPE WebKit 2.44.1 (the flybridge
 * kiosk's browser) can't composite a multipart MJPEG `<img>` inside an
 * iframe whenever that iframe sits under a CSS-transformed ancestor in its
 * own document — verified on-device, and display-shell.tsx's outer
 * (rotation) and inner (scale + pixel shift) boxes are exactly such an
 * ancestor chain. The same iframe, transformed directly with no transformed
 * ancestor between it and the viewport, renders fine, so EmbedTile portals
 * its iframe onto `document.body` and applies this context's rotate/scale
 * to the iframe itself instead of inheriting it here - `clipElement` is
 * what lets it also reproduce the crop that portal otherwise escapes.
 */
export const DisplayTransformContext = createContext<DisplayTransform | null>(null)

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
 * Both boxes' transforms are also broadcast down through
 * DisplayTransformContext, because being *inside* this transformed ancestor
 * chain is itself a problem for one descendant: EmbedTile's iframe (see that
 * context's own doc comment for why).
 *
 * A second, identically-sized-and-rotated "overlay" box is rendered as a
 * sibling of the main one, `z-10` and non-interactive. EmbedTile's portalled
 * iframe (a plain child of `document.body`, appended after this component's
 * own DOM) paints above the main box's own transformed stacking context
 * regardless of any z-index set *inside* it - so DisplayStatusBadge and
 * anything passed as `overlay` (App.tsx's DisplayRemoteToast) live in this
 * second box instead, where an explicit z-index actually gets compared
 * against the portal's and wins.
 *
 * A display with both width and height 0 means "whatever this browser
 * reports": the outer box claims the entire viewport (the pre-ADR-0110
 * `fixed inset-0` kiosk root's own behaviour) and no scaling is applied,
 * regardless of what the record's own `scale` field says — the backend
 * rejects a non-1.0 scale on a zero canvas, but this component doesn't lean
 * on that invariant holding for a record it didn't itself validate.
 */
export function DisplayShell({ display, alarms, children, overlay }: DisplayShellProps) {
  const { dx, dy } = usePixelShift(display.pixel_shift)
  const { status: wakeLockStatus } = useScreenWakeLock(display.wake_lock)

  const isFullViewport = display.width === 0 && display.height === 0
  const scale = isFullViewport ? 1 : displayScale(display)
  const rotate = display.rotate === 180 ? 180 : 0

  // A callback ref via useState rather than useRef: DisplayTransformContext
  // needs to re-broadcast once this actually points at the mounted node
  // (useRef's mutation wouldn't trigger that), but a plain function ref
  // recreated every render would forget it just as fast, calling this with
  // `null` then the real node right back on every single render.
  const [outerEl, setOuterEl] = useState<HTMLElement | null>(null)

  // Memoised so a descendant reading this from context (today, just
  // EmbedTile) only re-runs its own positioning effect when one of these
  // five values actually changes, not on every DisplayShell re-render.
  const displayTransform = useMemo<DisplayTransform>(
    () => ({ rotate, scale, dx, dy, clipElement: outerEl }),
    [rotate, scale, dx, dy, outerEl],
  )

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
    <>
      <div
        ref={setOuterEl}
        data-testid="display-shell-outer"
        data-rotate={display.rotate}
        className="fixed overflow-hidden bg-background"
        style={outerStyle}
      >
        <div data-testid="display-shell-inner" className="relative p-1" style={innerStyle}>
          <DisplayTransformContext.Provider value={displayTransform}>
            {children}
          </DisplayTransformContext.Provider>
        </div>
      </div>
      {/* Same footprint and rotation as the main box above (shared style
          objects), but z-10, pointer-events-none and transparent - see this
          component's own doc comment for why this exists as a second box at
          all rather than just raising these children's own z-index. */}
      <div
        data-testid="display-shell-overlay"
        className="pointer-events-none fixed z-10 overflow-hidden"
        style={outerStyle}
      >
        <div className="relative p-1" style={innerStyle}>
          <DisplayStatusBadge alarms={alarms} wakeLockStatus={wakeLockStatus} />
          {overlay}
        </div>
      </div>
    </>
  )
}

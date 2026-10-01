import { Globe, Settings2 } from 'lucide-react'
import {
  memo,
  useCallback,
  useContext,
  useEffect,
  useLayoutEffect,
  useMemo,
  useState,
  type CSSProperties,
} from 'react'
import { createPortal } from 'react-dom'

import { DisplayTransformContext } from '@/components/display-shell'
import { Button } from '@/components/ui/button'
import { Tile, tilePillButtonClass } from '@/components/ui/tile'
import { useInView } from '@/hooks/use-in-view'
import { isValidEmbedUrl, type EmbedWidgetConfig } from '@/lib/dashboard-widgets'
import { cn } from '@/lib/utils'

// How long to wait, once the tile has scrolled into view, before actually
// mounting the iframe. A tile already in view at first render (the common
// case: dashboard startup) would otherwise have its embed compete with
// Helmcentral's own script evaluation for the same paint — a measured trace
// found one Grafana panel alone costing 1.38s of main-thread time, more than
// the rest of the dashboard's startup script combined. Exported so the test
// file can advance fake timers past it without duplicating the number.
export const MOUNT_IDLE_TIMEOUT_MS = 200

interface EmbedTileProps {
  config: EmbedWidgetConfig | undefined
  editing: boolean
  onConfigure?: () => void
  isDarkTheme?: boolean
}

/** Keeps a `theme` param already present in an operator URL in sync with the
 *  app's light/dark mode. URLs without one are returned untouched: ADR 0031
 *  keeps this widget vendor-neutral, so we do not inject a Grafana convention
 *  into every embed. The operator opts in by writing `&theme=` once. */
function withTheme(rawUrl: string, isDarkTheme: boolean): string {
  try {
    const url = new URL(rawUrl)
    if (!url.searchParams.has('theme')) return rawUrl
    url.searchParams.set('theme', isDarkTheme ? 'dark' : 'light')
    return url.toString()
  } catch {
    return rawUrl
  }
}

interface EmbedFrameProps {
  /** The tile's own frame container element (both branches below pass their
   * `frameContainerEl` state, not a ref object — see the layout effect
   * below for why) — used only to measure and position the iframe when it
   * has to be portalled; ignored otherwise. */
  container: HTMLDivElement | null
  src: string
  title: string
  className: string
}

/** Field-by-field, not a deep-equal library: `portalStyle` only ever has
 * these seven fields (see `reposition` below), and this is what lets a
 * no-op reposition bail out of setState entirely rather than committing a
 * new-but-identical style object on every ResizeObserver/MutationObserver
 * firing on an otherwise-static tile. */
function portalStylesEqual(a: CSSProperties, b: CSSProperties): boolean {
  return (
    a.left === b.left &&
    a.top === b.top &&
    a.width === b.width &&
    a.height === b.height &&
    a.transform === b.transform &&
    a.clipPath === b.clipPath &&
    a.visibility === b.visibility
  )
}

/**
 * The `<iframe>` itself, factored out because the frameless and Tile-wrapped
 * branches below both need one with identical attributes.
 *
 * WPE WebKit 2.44.1 (the flybridge kiosk's browser) fails to composite a
 * multipart MJPEG `<img>` inside this iframe whenever the iframe sits under
 * a CSS-transformed ancestor in its own document — verified on-device. A
 * wall display is exactly that: display-shell.tsx rotates its outer box and
 * scales/pixel-shifts its inner box, and every tile paints inside both. The
 * same iframe, transformed directly with no transformed ancestor between it
 * and the viewport, renders fine on-device. So inside a DisplayShell
 * (DisplayTransformContext non-null) this portals the iframe onto
 * `document.body` — outside the transformed chain entirely — and applies
 * the shell's own rotation/scale to the iframe itself instead of inheriting
 * it from an ancestor. The container div keeps its ref and testid either
 * way; it's what gets measured to position the portalled iframe, and what
 * stays in place as an (empty) placeholder for it.
 *
 * Outside a DisplayShell, DisplayTransformContext is null and this renders
 * the iframe inline exactly as before — no portal, no behaviour change.
 *
 * Once portalled, the iframe is repositioned to track the container's own
 * rect (see `reposition` inside the layout effect below): centred over it,
 * rotated/scaled to match, cropped to the display's outer box (which the
 * portal otherwise escapes, since `overflow-hidden` only clips the outer
 * box's actual DOM descendants), and re-run on every resize/mutation of the
 * container or any ancestor up to the shell's own inner box.
 *
 * `container` arrives as plain state, not a ref object, on purpose: when
 * the container div and this component mount together in the same commit
 * (e.g. toggling the widget's `frameless` setting swaps EmbedTile between
 * the frameless and Tile-wrapped branches while an embed is already
 * mounted), React attaches a host node's ref only after its child
 * subtree's own layout effects have already run in that commit — so a
 * `containerRef.current` read here would still see null on this first
 * pass. Reading `container` as a prop instead means the parent's own
 * callback ref, once it fires and updates that state, drives a second
 * render with the real element, and the dependency array below picks that
 * up and reruns the effect against a container that is now guaranteed to
 * be attached.
 */
function EmbedFrame({ container, src, title, className }: EmbedFrameProps) {
  const displayTransform = useContext(DisplayTransformContext)
  const [portalStyle, setPortalStyle] = useState<CSSProperties | null>(null)

  // Recomputed on the container's own size and every ancestor's up to the
  // shell's inner box (ResizeObserver + MutationObserver, both below), on
  // window resize, and whenever the shell's rotation/scale/pixel-shift/clip
  // element changes — a pixel-shift move alone doesn't resize anything, so
  // ResizeObserver and window resize wouldn't otherwise catch it. Also
  // rerun whenever `container` itself changes identity (see the doc
  // comment above) rather than only on mount, since a null-then-set
  // transition has to re-attach the observers it skipped the first time.
  useLayoutEffect(() => {
    if (!displayTransform) return
    if (!container) return
    // Re-bound to a variable whose own type is already non-null, rather than
    // relying on `displayTransform` staying narrowed inside the nested
    // `reposition` closure below — TS's control-flow narrowing doesn't carry
    // an outer guard into a nested function body.
    const shellTransform = displayTransform

    function reposition() {
      if (!container) return
      const r = container.getBoundingClientRect()
      const w = container.offsetWidth
      const h = container.offsetHeight
      // The on-screen size over the layout size, rather than the shell's
      // scale alone: the hero row enlarges its tile with a transform of its
      // own, and that has to reach the iframe too.
      const s = w > 0 ? r.width / w : shellTransform.scale
      const rotated = shellTransform.rotate === 180

      const style: CSSProperties = {
        position: 'fixed',
        left: r.left + r.width / 2 - w / 2,
        top: r.top + r.height / 2 - h / 2,
        width: w,
        height: h,
        transformOrigin: 'center center',
        transform: `${rotated ? 'rotate(180deg) ' : ''}scale(${s})`,
      }

      // The outer box clips everything painted inside it with
      // overflow-hidden, but this iframe lives at document.body and
      // escapes that clip entirely - re-derive the same crop by hand from
      // how far the container's own rect extends past the outer box's.
      const outer = shellTransform.clipElement
      if (outer) {
        const o = outer.getBoundingClientRect()
        const oTop = Math.max(0, o.top - r.top)
        const oBottom = Math.max(0, r.bottom - o.bottom)
        const oLeft = Math.max(0, o.left - r.left)
        const oRight = Math.max(0, r.right - o.right)

        if (oTop > 0 || oBottom > 0 || oLeft > 0 || oRight > 0) {
          // clip-path insets the element's own (local, pre-transform) box.
          // Unrotated, local top/right/bottom/left line up with screen
          // top/right/bottom/left one-to-one. Rotated 180, the iframe's own
          // `transform` above turns its local top into screen bottom (and
          // local left into screen right), so the inset that belongs on
          // local top is the overflow measured at screen bottom, and so on
          // round the box.
          const [top, right, bottom, left] = rotated
            ? [oBottom, oLeft, oTop, oRight]
            : [oTop, oRight, oBottom, oLeft]
          style.clipPath = `inset(${top / s}px ${right / s}px ${bottom / s}px ${left / s}px)`
        }

        const outsideEntirely =
          r.right <= o.left || r.left >= o.right || r.bottom <= o.top || r.top >= o.bottom
        if (outsideEntirely) style.visibility = 'hidden'
      }

      setPortalStyle((prev) => (prev && portalStylesEqual(prev, style) ? prev : style))
    }

    reposition()

    const resizeObserver = new ResizeObserver(reposition)
    resizeObserver.observe(container)

    // react-grid-layout moves a tile by writing `transform`/`top`/`left` (or
    // toggling a class mid-drag) onto the grid-item element, never by
    // resizing the tile's own container - and a sibling growing (e.g. the
    // hero row) shifts everything below it the same way. Neither trips the
    // ResizeObserver above, so every ancestor between the container and the
    // shell's own inner box (which already re-broadcasts its own changes
    // through displayTransform) gets watched too, for exactly the
    // attributes react-grid-layout and the grid actually touch.
    const shellInner = container.closest('[data-testid="display-shell-inner"]')
    const ancestors: Element[] = []
    for (let node = container.parentElement; node && node !== shellInner; node = node.parentElement) {
      ancestors.push(node)
    }
    ancestors.forEach((ancestor) => resizeObserver.observe(ancestor))

    const mutationObserver = new MutationObserver(reposition)
    ancestors.forEach((ancestor) =>
      mutationObserver.observe(ancestor, { attributes: true, attributeFilter: ['style', 'class'] }),
    )

    // react-grid-layout slides a moved tile over a CSS transition, so the
    // mutation above fires while the tile is still at its old spot. Catch
    // the end of the slide too, but only an ancestor's own transition, not
    // one bubbling up from a tile's content.
    function handleTransitionEnd(event: Event) {
      if (event.target === event.currentTarget) reposition()
    }
    ancestors.forEach((ancestor) => ancestor.addEventListener('transitionend', handleTransitionEnd))

    window.addEventListener('resize', reposition)
    return () => {
      resizeObserver.disconnect()
      mutationObserver.disconnect()
      ancestors.forEach((ancestor) => ancestor.removeEventListener('transitionend', handleTransitionEnd))
      window.removeEventListener('resize', reposition)
    }
  }, [displayTransform, container])

  const frameProps = {
    src,
    title,
    loading: 'lazy' as const,
    // F-3 (security audit): "no-referrer-when-downgrade" sent this page's
    // full URL — including a deep-link path like /dashboard/<page id> — as
    // the Referer header on every request an http-served embed makes. The
    // embed never needs that.
    referrerPolicy: 'no-referrer' as const,
    // allow-same-origin is needed for the embedded app's own session
    // (Grafana will not render without it). isValidEmbedUrl
    // (lib/dashboard-widgets.ts) rejects any embed URL whose origin equals
    // this app's own before the config dialog can save it, so through that
    // dialog allow-same-origin can only ever apply to a genuinely different
    // origin. It is NOT enforced by the backend yet — see
    // validateEmbedWidget's comment in backend/dashboard_pages.go — so a
    // same-origin URL saved by calling the API directly, bypassing this
    // dialog, would still render here with the sandbox defeated against
    // this window.
    sandbox: 'allow-scripts allow-same-origin allow-popups allow-forms',
    className,
  }

  if (!displayTransform) {
    return <iframe {...frameProps} />
  }

  if (!portalStyle) return null

  return createPortal(
    <iframe
      {...frameProps}
      data-testid="embed-frame-portal"
      data-rotate={displayTransform.rotate}
      style={portalStyle}
    />,
    document.body,
  )
}

/**
 * Renders an operator-supplied page (a Grafana panel, a Node-RED dashboard, …)
 * inside the bento grid. See ADR 0031.
 *
 * Unlike the Windy embed in radar-drawer.tsx, the URL here comes from persisted
 * config rather than live GPS, so the day/night toggle is the only thing that
 * should ever change it. Changing an iframe's src remounts the frame, so `src`
 * is memoised on the config URL and isDarkTheme: without that, App's frequent
 * re-renders would rebuild the string each time and reload the embed.
 */
export const EmbedTile = memo(function EmbedTile({
  config,
  editing,
  onConfigure,
  isDarkTheme = false,
}: EmbedTileProps) {
  const title = config?.title.trim() || 'Embed'
  const hasUsableUrl = config !== undefined && isValidEmbedUrl(config.url)
  const src = useMemo(
    () => (config ? withTheme(config.url.trim(), isDarkTheme) : ''),
    [config?.url, isDarkTheme],
  )

  // Loads the iframe only once the tile has actually scrolled near the
  // viewport, so an off-screen embed doesn't cost anything until it might be
  // seen. useInView is sticky (see its own doc comment), so scrolling the
  // tile back out afterwards never tears the iframe down and forces the
  // embedded app to reload.
  const [frameContainerRef, inView] = useInView<HTMLDivElement>({ rootMargin: '200px' })

  // The container element, held as state rather than read straight off
  // frameContainerRef.current, so EmbedFrame can depend on it. Toggling
  // `frameless` swaps which of the two branches below returns the
  // container div and the EmbedFrame together, in the same commit — if
  // EmbedFrame read `frameContainerRef.current` directly in that commit's
  // own layout effect, it would still see null (a host node's ref attaches
  // after its child subtree's layout effects have already run in that same
  // commit). Setting state from this callback ref instead schedules a
  // follow-up render with the real element once React has actually
  // attached it, and EmbedFrame's effect reruns against that.
  const [frameContainerEl, setFrameContainerEl] = useState<HTMLDivElement | null>(null)
  const setFrameContainer = useCallback(
    (node: HTMLDivElement | null) => {
      frameContainerRef.current = node
      setFrameContainerEl(node)
    },
    [frameContainerRef],
  )
  const [shouldMount, setShouldMount] = useState(false)

  useEffect(() => {
    if (!inView) return

    // requestIdleCallback, where it exists, so the mount doesn't steal a
    // frame from the browser's own first paint. The kiosk's WPE WebKit
    // (Safari 16-era) has no requestIdleCallback, so a plain setTimeout
    // fallback is required there — this is a feature check, not a masking
    // fallback: both paths land on the same mount, just scheduled
    // differently, and neither is a substitute for the other's failure.
    if (typeof window.requestIdleCallback === 'function') {
      const id = window.requestIdleCallback(() => setShouldMount(true), {
        timeout: MOUNT_IDLE_TIMEOUT_MS,
      })
      return () => window.cancelIdleCallback(id)
    }
    const id = window.setTimeout(() => setShouldMount(true), MOUNT_IDLE_TIMEOUT_MS)
    return () => window.clearTimeout(id)
    // `src` is included so a pending idle callback scheduled for a since-
    // replaced embed URL is cancelled rather than left to fire later.
  }, [inView, src])

  const mountFrame = hasUsableUrl && shouldMount
  const placeholderClassName =
    'flex h-full min-h-0 flex-col items-center justify-center gap-2 rounded-md border border-dashed bg-background/60 px-3 py-4 text-center'

  // Frameless drops the Tile title bar and padding so the embed fills the
  // widget (a wall-display strip has no use for either). Editing overrides
  // it: the gear icon and title need to stay reachable to reconfigure the
  // widget, and an empty URL has no chrome of its own to drop, so both fall
  // through to the regular Tile-wrapped rendering below.
  if (hasUsableUrl && config?.frameless && !editing) {
    return (
      <div
        ref={setFrameContainer}
        data-testid="embed-frame-container"
        className="h-full w-full overflow-hidden rounded-md border border-border bg-background"
      >
        {mountFrame ? (
          <EmbedFrame
            container={frameContainerEl}
            src={src}
            title={title}
            className="h-full w-full border-0"
          />
        ) : (
          <div className={placeholderClassName}>
            <span className="text-xs uppercase tracking-[0.16em] text-muted-foreground">
              Loading {title}...
            </span>
          </div>
        )}
      </div>
    )
  }

  return (
    <Tile
      title={title}
      icon={<Globe className="h-3.5 w-3.5 text-gauge-secondary" />}
      titleExtra={
        editing && onConfigure ? (
          <button type="button" className={tilePillButtonClass} onClick={onConfigure} aria-label={`Configure embed: ${title}`}>
            <Settings2 className="size-3.5" />
          </button>
        ) : undefined
      }
    >
      {/* `min-h` floor so a newly-added embed is usable before the operator has sized
          it. The iframe is `h-full`, and in the narrow CSS grid the enclosing chain is
          auto-height, so without a floor the frame collapses to its ~150px intrinsic
          default rather than the height the tile was given. */}
      <div ref={setFrameContainer} data-testid="embed-frame-container" className="h-full min-h-[240px]">
        {hasUsableUrl ? (
          mountFrame ? (
            <EmbedFrame
              container={frameContainerEl}
              src={src}
              title={title}
              className={cn(
                'h-full w-full rounded-md border-0 bg-background',
                // Let drag/resize gestures pass through to the grid underneath.
                editing && 'pointer-events-none',
              )}
            />
          ) : (
            <div className={placeholderClassName}>
              <span className="text-xs uppercase tracking-[0.16em] text-muted-foreground">
                Loading {title}...
              </span>
            </div>
          )
        ) : (
          <div className={placeholderClassName}>
            <span className="text-xs uppercase tracking-[0.16em] text-muted-foreground">
              No URL configured
            </span>
            {editing && onConfigure && (
              <Button variant="outline" size="sm" className="h-8 text-xs" onClick={onConfigure}>
                Configure
              </Button>
            )}
          </div>
        )}
      </div>
    </Tile>
  )
})

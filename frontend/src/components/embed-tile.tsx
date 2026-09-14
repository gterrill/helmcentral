import { Globe, Settings2 } from 'lucide-react'
import { memo, useEffect, useMemo, useState } from 'react'

import { Button } from '@/components/ui/button'
import { Tile } from '@/components/ui/tile'
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
        ref={frameContainerRef}
        data-testid="embed-frame-container"
        className="h-full w-full overflow-hidden rounded-md border border-border bg-background"
      >
        {mountFrame ? (
          <iframe
            src={src}
            title={title}
            loading="lazy"
            referrerPolicy="no-referrer-when-downgrade"
            // allow-same-origin is needed for the embedded app's own session
            // (Grafana will not render without it). It only defeats the sandbox
            // for a same-origin frame, which an operator-supplied embed is not.
            sandbox="allow-scripts allow-same-origin allow-popups allow-forms"
            className="h-full w-full border-0"
          />
        ) : (
          <div className={placeholderClassName}>
            <span className="text-[11px] uppercase tracking-[0.16em] text-muted-foreground">
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
          <Button
            variant="ghost"
            size="icon"
            className="h-6 w-6 text-muted-foreground hover:text-foreground"
            onClick={onConfigure}
            aria-label={`Configure embed: ${title}`}
          >
            <Settings2 className="h-3.5 w-3.5" />
          </Button>
        ) : undefined
      }
    >
      {/* `min-h` floor so a newly-added embed is usable before the operator has sized
          it. The iframe is `h-full`, and in the narrow CSS grid the enclosing chain is
          auto-height, so without a floor the frame collapses to its ~150px intrinsic
          default rather than the height the tile was given. */}
      <div ref={frameContainerRef} data-testid="embed-frame-container" className="h-full min-h-[240px]">
        {hasUsableUrl ? (
          mountFrame ? (
            <iframe
              src={src}
              title={title}
              loading="lazy"
              referrerPolicy="no-referrer-when-downgrade"
              // allow-same-origin is needed for the embedded app's own session
              // (Grafana will not render without it). It only defeats the sandbox
              // for a same-origin frame, which an operator-supplied embed is not.
              sandbox="allow-scripts allow-same-origin allow-popups allow-forms"
              className={cn(
                'h-full w-full rounded-md border-0 bg-background',
                // Let drag/resize gestures pass through to the grid underneath.
                editing && 'pointer-events-none',
              )}
            />
          ) : (
            <div className={placeholderClassName}>
              <span className="text-[11px] uppercase tracking-[0.16em] text-muted-foreground">
                Loading {title}...
              </span>
            </div>
          )
        ) : (
          <div className={placeholderClassName}>
            <span className="text-[11px] uppercase tracking-[0.16em] text-muted-foreground">
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

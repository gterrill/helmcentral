import { Globe, Settings2 } from 'lucide-react'
import { memo, useMemo } from 'react'

import { Button } from '@/components/ui/button'
import { Tile } from '@/components/ui/tile'
import { isValidEmbedUrl, type EmbedWidgetConfig } from '@/lib/dashboard-widgets'
import { cn } from '@/lib/utils'

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
      <div className="h-full min-h-[240px]">
        {hasUsableUrl ? (
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
          <div className="flex h-full min-h-0 flex-col items-center justify-center gap-2 rounded-md border border-dashed bg-background/60 px-3 py-4 text-center">
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

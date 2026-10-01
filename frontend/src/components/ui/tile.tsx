import * as React from 'react'

import { Card, CardContent, CardTitle } from '@/components/ui/card'
import { severityBorderClass, severityFill, type ZoneState } from '@/lib/severity'
import { useMeasureTileHeight } from '@/lib/tile-content-height'
import { cn } from '@/lib/utils'

/**
 * The look shared by the tile title and every control on the tile's top edge:
 * a 24px pill whose outline inherits the card's border colour, so title and
 * controls turn amber or red together with the tile edge. One string, so they
 * cannot drift apart.
 */
export const tilePillClass =
  'relative inline-flex h-6 min-w-0 shrink-0 items-center justify-center gap-1 rounded-full border bg-card px-2.5 [border-color:inherit] font-display text-xs font-normal uppercase leading-none tracking-[0.14em] text-muted-foreground sm:tracking-[0.22em]'

/**
 * A pill that is a button. The pill stays 24px tall, but DESIGN.md sets a 40px
 * control floor for a moving boat, so an invisible pseudo-element grows the hit
 * area 8px on every side. Tiles are 16px apart, so 8px is the most a hit area
 * can grow without overlapping the neighbouring tile's controls.
 */
export const tilePillButtonClass = cn(
  tilePillClass,
  "cursor-pointer transition-colors after:absolute after:-inset-2 after:content-[''] hover:text-foreground ring-offset-background focus-visible:outline-hidden focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2",
)

interface TileProps {
  title: string
  icon?: React.ReactNode
  className?: string
  titleClassName?: string
  titleExtra?: React.ReactNode
  /**
   * The tile's source has stopped updating. Desaturates the readings,
   * outlines the tile in amber and shows how long ago the last update
   * arrived, so a value frozen by a dead feed is loudly — not quietly —
   * distinguished from a live measurement.
   */
  stale?: boolean
  /** Age of the last update, already formatted (`1h 39m`). */
  staleLabel?: string
  /**
   * The worst zone state among this tile's readings (ADR 0081), from
   * `worstZoneState` in lib/severity.ts. `null` or `normal` draw nothing —
   * colour is spent only once a tile actually has something to say. Ignored
   * while `stale`: a value the tile can no longer vouch for should not also
   * claim a state, which is the same call ADR 0068 already made for the
   * readings themselves. `outside` is also ignored here (see `showState`
   * below): only a named severity the operator actually configured lights
   * the edge.
   */
  state?: ZoneState | null
  /**
   * Make the content a flex column with `min-h-0`, so a child with `flex-1`
   * receives the tile's remaining height. Opt-in: turning every tile's content
   * into a flex container would change the layout of the ones that rely on
   * block flow.
   */
  fill?: boolean
  /**
   * The tile is a list of variable length. It tells the board the height its
   * content needs, and the board may draw it that short when it is the bottom
   * tile of its column (never while editing, and never saved). Leave it off
   * for anything that is a fixed instrument or fills its space.
   */
  shrinkToContent?: boolean
  children: React.ReactNode
}

export function Tile({
  title,
  icon,
  className,
  titleClassName,
  titleExtra,
  stale = false,
  staleLabel,
  state = null,
  fill = false,
  shrinkToContent = false,
  children,
}: TileProps) {
  const contentRef = React.useRef<HTMLDivElement>(null)
  useMeasureTileHeight(contentRef, shrinkToContent)

  // `outside` stays out of this: a bundled engine profile with no warn/alarm
  // thresholds filled in (ADR 0054 §5a) puts every reading above its normal
  // band on `outside` all day on a healthy engine, and lighting the edge for
  // that reproduces the exact noise floor ADR 0080 removed from the dial.
  // worstZoneState still returns it and severityBorderClass still maps it,
  // for callers that want it; the tile itself only lights for a severity the
  // operator actually configured.
  const showState = !stale && state !== null && state !== 'normal' && state !== 'outside'

  return (
    <Card
      className={cn(
        'relative h-full gap-0 pb-2 pt-6',
        stale && 'border-amber-500 dark:border-amber-400',
        showState && severityBorderClass(state),
        className,
      )}
      data-stale={stale ? 'true' : undefined}
      data-state={showState ? state : undefined}
    >
      {/* The legend sits across the card's top border, fieldset style: the header
          is out of flow, centred on the border line, and the title's bg-card masks
          the line behind it. The title is a pill whose outline inherits the card's
          border colour, so it turns amber or red with the tile edge. Padding and
          letter-spacing tighten before anything else at phone width. */}
      <div
        data-slot="card-header"
        className="absolute inset-x-3 top-0 z-10 flex -translate-y-1/2 items-center gap-2 [border-color:inherit] sm:inset-x-4"
      >
        <CardTitle
          as="h2"
          className={cn(
            tilePillClass,
            'shrink',
            titleClassName,
          )}
        >
          {icon ? <span className="inline-flex h-3.5 w-3.5 shrink-0 items-center justify-center">{icon}</span> : null}
          {/* Truncates rather than stretching the card: an operator-supplied embed
              title is arbitrary length and used to have no way to yield. */}
          <span className="truncate">{title}</span>
          {stale && (
            <span
              data-testid="tile-stale-badge"
              title={staleLabel ? `No update for ${staleLabel}` : 'Source has stopped updating'}
              className="ml-1 shrink-0 rounded-xs border border-amber-500/40 bg-amber-500/10 px-1.5 py-0.5 text-xs leading-none text-amber-600 dark:text-amber-400"
            >
              Stale{staleLabel ? ` ${staleLabel}` : ''}
            </span>
          )}
        </CardTitle>
        {showState && (
          <span className="-mr-1 ml-auto shrink-0 rounded-full bg-card p-1">
            <span
              data-testid="tile-state-dot"
              aria-label={`State: ${state}`}
              className="block h-2 w-2 rounded-full"
              style={{ background: severityFill(state) }}
            />
          </span>
        )}
        {/* Controls sit on the same border at the right end, as pills. */}
        {titleExtra && (
          <div data-slot="card-action" className={cn('flex shrink-0 items-center gap-1.5 [border-color:inherit]', !showState && 'ml-auto')}>
            {titleExtra}
          </div>
        )}
      </div>

      {/* Grayscale drains the colour out of a dead reading so it can't be mistaken
          for a live one; it deliberately does not dim it further with opacity — a
          faded tile reads as "dim screen in the sun," not "this feed is dead," and
          this is the system's loudest state, not its quietest. */}
      <CardContent className={cn('flex-1 px-3 sm:px-4', fill && 'flex min-h-0 flex-col',
          // min-h-0 so the area is card minus chrome whatever the list needs.
          // With the default min-height:auto a list taller than a shrunk card
          // pushes the area out with it, the measurement collapses to the
          // card's current height, and the tile could never grow back.
          shrinkToContent && 'min-h-0',
          stale && 'grayscale')}>
        {shrinkToContent ? (
          // flow-root so the list's own top margin stays inside the measured
          // block; a plain div would let it collapse out and go uncounted.
          <div ref={contentRef} className="flow-root">{children}</div>
        ) : (
          children
        )}
      </CardContent>
    </Card>
  )
}

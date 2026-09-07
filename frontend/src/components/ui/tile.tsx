import * as React from 'react'

import { Card, CardAction, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { severityBorderClass, severityFill, type ZoneState } from '@/lib/severity'
import { cn } from '@/lib/utils'

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
   * readings themselves.
   */
  state?: ZoneState | null
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
  children,
}: TileProps) {
  const showState = !stale && state !== null && state !== 'normal'

  return (
    <Card
      className={cn(
        'h-full gap-0 py-4',
        stale && 'border-amber-500 dark:border-amber-400',
        showState && severityBorderClass(state),
        className,
      )}
      data-stale={stale ? 'true' : undefined}
      data-state={showState ? state : undefined}
    >
      {/* Padding and letter-spacing tighten before anything else at phone width. The
          0.22em tracking costs more width than the horizontal padding does, so it is
          the first thing to give. */}
      <CardHeader className="flex-row items-center gap-2 space-y-0 px-3 pb-3 sm:px-4">
        <CardTitle
          as="h2"
          className={cn(
            'inline-flex min-w-0 items-center gap-1 font-display text-xs font-normal uppercase tracking-[0.14em] text-muted-foreground sm:tracking-[0.22em]',
            titleClassName,
          )}
        >
          {icon ? <span className="inline-flex h-3.5 w-3.5 shrink-0 items-center justify-center">{icon}</span> : null}
          {/* Truncates rather than stretching the card: an operator-supplied embed
              title is arbitrary length and used to have no way to yield. */}
          <span className="truncate">{title}</span>
          {/* Inside the title rather than a sibling of it: CardHeader is a grid,
              and a direct child would stretch into a full-width band. */}
          {stale && (
            <span
              data-testid="tile-stale-badge"
              title={staleLabel ? `No update for ${staleLabel}` : 'Source has stopped updating'}
              className="ml-1 shrink-0 rounded-sm border border-amber-500/40 bg-amber-500/10 px-1.5 py-0.5 text-[10px] leading-none text-amber-600 dark:text-amber-400"
            >
              Stale{staleLabel ? ` ${staleLabel}` : ''}
            </span>
          )}
        </CardTitle>
        <div className="h-px flex-1 bg-border/70" />
        {showState && (
          <span
            data-testid="tile-state-dot"
            aria-label={`State: ${state}`}
            className="h-2 w-2 shrink-0 rounded-full"
            style={{ background: severityFill(state === 'outside' ? 'warn' : state) }}
          />
        )}
        {titleExtra && <CardAction className="static shrink-0">{titleExtra}</CardAction>}
      </CardHeader>

      {/* Grayscale drains the colour out of a dead reading so it can't be mistaken
          for a live one; it deliberately does not dim it further with opacity — a
          faded tile reads as "dim screen in the sun," not "this feed is dead," and
          this is the system's loudest state, not its quietest. */}
      <CardContent className={cn('flex-1 px-3 sm:px-4', stale && 'grayscale')}>{children}</CardContent>
    </Card>
  )
}

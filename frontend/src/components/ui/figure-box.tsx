import type { ReactNode } from 'react'

import { cn } from '@/lib/utils'

interface FigureBoxProps {
  label: string
  value: string
  /** Unit after the figure. Never smaller than text-xs, even narrow. */
  unit?: string
  /** A line under the figure (a current, an AC/DC split). */
  sub?: ReactNode
  /**
   * What the box does below 18rem (@max-2xs) of tile width, when the tile
   * carries `@container`. `row`: label left (it truncates), figure right,
   * `sub` on its own line. Use it where the tile is at least three columns wide.
   * `stack`: label over figure, the figure at text-lg, for the two-column
   * tile (112-155px) where a row leaves the label no room.
   */
  narrow?: 'row' | 'stack'
  className?: string
  labelClassName?: string
  /** Size and colour of the figure. Defaults to text-3xl. */
  valueClassName?: string
  unitClassName?: string
  subClassName?: string
}

/**
 * The tile's figure box: a label over a `font-display` figure, as the power
 * tiles share it. The tile that holds it carries `@container` on its `<Tile>`,
 * and the box answers the tile's width, not the viewport's. Callers' classes
 * win over the defaults, including the narrow ones.
 */
export function FigureBox({
  label,
  value,
  unit,
  sub,
  narrow = 'row',
  className,
  labelClassName,
  valueClassName,
  unitClassName,
  subClassName,
}: FigureBoxProps) {
  const row = narrow === 'row'
  return (
    <div
      className={cn(
        'min-w-0 rounded-md border bg-background/60 px-3 py-2 @max-2xs:px-2 @max-2xs:py-1',
        row && '@max-2xs:flex @max-2xs:flex-wrap @max-2xs:items-baseline @max-2xs:gap-x-2',
        className,
      )}
    >
      <p
        className={cn(
          'truncate text-[10px] uppercase tracking-[0.16em] text-muted-foreground',
          row && '@max-2xs:min-w-0 @max-2xs:flex-1',
          labelClassName,
        )}
      >
        {label}
      </p>
      <p
        className={cn(
          'whitespace-nowrap font-display text-3xl tabular-nums @max-2xs:text-lg',
          row && '@max-2xs:mt-0 @max-2xs:shrink-0',
          valueClassName,
          // After the caller's classes: tailwind-merge drops a line height that
          // comes before a font size, and the figures are set leading-none
          // unless the caller names a line height of its own.
          !/(^|\s)leading-/.test(valueClassName ?? '') && 'leading-none',
          !/@max-2xs:leading-/.test(valueClassName ?? '') && '@max-2xs:leading-none',
        )}
      >
        {value}
        {unit !== undefined && (
          <span className={cn('ml-1 text-base text-muted-foreground @max-2xs:text-xs', unitClassName)}>{unit}</span>
        )}
      </p>
      {sub !== undefined && (
        <p className={cn('mt-1 truncate font-display text-xs tabular-nums text-muted-foreground', row && '@max-2xs:mt-0 @max-2xs:basis-full', subClassName)}>
          {sub}
        </p>
      )}
    </div>
  )
}

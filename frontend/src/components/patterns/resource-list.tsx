import { Children, type ReactNode } from 'react'

import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { cn } from '@/lib/utils'

// ADR 0142: a list of things that are not columns of comparable values - a
// document with a thumbnail, a photo, a linked note. Where IndexTable is
// the right shape for a sortable, filterable table of records, ResourceList
// is for a short list inside a Details section whose entries each read as
// one media-led line with a menu. Loading, error and empty states follow
// IndexTable's so the two look the same when something is wrong.

export interface ResourceListProps {
  /** Accessible name for the list. */
  label: string
  header?: ReactNode
  count?: number
  loading?: boolean
  error?: string | null
  onRetry?: () => void
  /** Shown in place of the list when there are no items. */
  empty?: ReactNode
  children?: ReactNode
  className?: string
}

export function ResourceList({
  label,
  header,
  count,
  loading = false,
  error = null,
  onRetry,
  empty,
  children,
  className,
}: ResourceListProps) {
  const heading = header !== undefined && (
    <div className="flex items-center gap-2 text-xs font-medium tracking-wider text-muted-foreground uppercase">
      <span>{header}</span>
      {count !== undefined && <span className="tabular-nums">{count}</span>}
    </div>
  )
  const frame = 'rounded-md border border-border bg-card'

  if (error) {
    return (
      <div className={cn('flex flex-col gap-2', className)}>
        {heading}
        <div className={cn(frame, 'flex flex-col items-center gap-3 p-6 text-center')}>
          <p role="alert" className="text-sm text-destructive">{error}</p>
          {onRetry && (
            <Button type="button" variant="outline" size="sm" onClick={onRetry}>
              Retry
            </Button>
          )}
        </div>
      </div>
    )
  }

  if (loading) {
    return (
      <div className={cn('flex flex-col gap-2', className)}>
        {heading}
        <div className={cn(frame, 'flex flex-col gap-2 p-3')}>
          {[0, 1, 2].map((i) => (
            <Skeleton key={i} data-testid="resource-list-skeleton-row" className="h-10 w-full" />
          ))}
        </div>
      </div>
    )
  }

  if (Children.count(children) === 0) {
    return (
      <div className={cn('flex flex-col gap-2', className)}>
        {heading}
        {empty ?? null}
      </div>
    )
  }

  return (
    <div className={cn('flex flex-col gap-2', className)}>
      {heading}
      <ul aria-label={label} className={cn(frame, 'divide-y divide-border overflow-hidden')}>
        {children}
      </ul>
    </div>
  )
}

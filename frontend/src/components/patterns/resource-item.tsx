import type { ReactNode } from 'react'

import { cn } from '@/lib/utils'
import { RowActions, type RowAction } from './index-table'

// ADR 0142: one entry in a ResourceList. Polaris' ResourceItem: a media slot
// on the left, a primary title, a secondary metadata line, a trailing badge,
// and a contextual actions menu. Clicking the item runs its primary action.
// The click/Enter rules are IndexTable's: Enter counts only when it started
// on the item itself, and RowActions swallows its own click and keydown, so
// opening the menu or picking from it never also opens the item.

export interface ResourceItemProps<T> {
  /** Handed back to onOpen and to every action. */
  item: T
  media?: ReactNode
  title: ReactNode
  meta?: ReactNode
  badge?: ReactNode
  onOpen?: (item: T) => void
  actions?: RowAction<T>[]
  /** aria-label for the actions trigger, e.g. "Actions for Manual". */
  actionsLabel?: string
  className?: string
}

export function ResourceItem<T>({
  item,
  media,
  title,
  meta,
  badge,
  onOpen,
  actions,
  actionsLabel = 'Actions',
  className,
}: ResourceItemProps<T>) {
  return (
    <li
      tabIndex={onOpen ? 0 : undefined}
      onClick={onOpen ? () => onOpen(item) : undefined}
      onKeyDown={onOpen ? (e) => {
        if (e.key === 'Enter' && e.target === e.currentTarget) onOpen(item)
      } : undefined}
      className={cn(
        'flex items-center gap-3 p-3',
        onOpen && 'cursor-pointer hover:bg-muted/50 focus-visible:bg-muted/50 focus-visible:outline-none',
        className,
      )}
    >
      {media && <div className="flex size-10 shrink-0 items-center justify-center overflow-hidden rounded-md bg-muted text-muted-foreground">{media}</div>}
      <div className="flex min-w-0 flex-1 flex-col gap-0.5">
        <div className="truncate text-sm font-medium text-foreground">{title}</div>
        {meta && <div className="truncate text-xs text-muted-foreground">{meta}</div>}
      </div>
      {badge && <div className="shrink-0">{badge}</div>}
      {actions && actions.length > 0 && <RowActions row={item} actions={actions} label={actionsLabel} />}
    </li>
  )
}

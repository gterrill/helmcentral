import type { ReactNode } from 'react'

import { cn } from '@/lib/utils'

// ADR 0142: the CRUD pattern library's empty-list slot. Every index (an
// empty Equipment list, an empty Locations zone, ...) shows the same shape -
// an icon, a sentence of what's missing, and the one action that fixes it -
// so this is deliberately just a layout, not a fetcher: the caller decides
// when there's nothing to show and supplies the words for it.

export interface EmptyStateProps {
  icon?: ReactNode
  title: string
  description?: string
  action?: ReactNode
  className?: string
}

export function EmptyState({ icon, title, description, action, className }: EmptyStateProps) {
  return (
    <div className={cn('flex h-full flex-col items-center justify-center gap-3 px-6 py-16 text-center', className)}>
      {icon}
      <p className="text-sm font-medium text-foreground">{title}</p>
      {description && <p className="max-w-md text-sm text-muted-foreground">{description}</p>}
      {action}
    </div>
  )
}

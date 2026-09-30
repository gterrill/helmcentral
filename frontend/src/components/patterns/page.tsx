import type { ReactNode } from 'react'
import { ArrowLeft, MoreVertical } from 'lucide-react'

import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { cn } from '@/lib/utils'

// ADR 0142: the CRUD page shell - title, an optional back control or
// breadcrumb above it, one primary action, and any number of secondary
// actions collapsed into a single overflow menu (Polaris' own "resource
// index/details" header shape, built from this project's existing
// DropdownMenu rather than a new menu primitive).

export interface PageAction {
  label: string
  onClick: () => void
  icon?: ReactNode
  destructive?: boolean
  disabled?: boolean
}

export interface PageProps {
  title: ReactNode
  /** A ui/breadcrumb trail rendered above the title row - independent of
   * onBack, which renders a single back arrow inline with the title instead.
   * A page uses one or the other, never both. */
  breadcrumb?: ReactNode
  onBack?: () => void
  backLabel?: string
  primaryAction?: PageAction
  secondaryActions?: PageAction[]
  children?: ReactNode
  className?: string
}

export function Page({
  title,
  breadcrumb,
  onBack,
  backLabel = 'Back',
  primaryAction,
  secondaryActions,
  children,
  className,
}: PageProps) {
  return (
    <div className={cn('flex flex-col gap-4', className)}>
      {breadcrumb}
      <div className="flex items-center gap-2">
        {onBack && (
          <Button type="button" variant="ghost" size="icon" aria-label={backLabel} onClick={onBack}>
            <ArrowLeft className="h-4 w-4" aria-hidden="true" />
          </Button>
        )}
        <h1 className="min-w-0 flex-1 truncate text-lg font-semibold text-foreground">{title}</h1>
        {primaryAction && (
          <Button type="button" onClick={primaryAction.onClick} disabled={primaryAction.disabled}>
            {primaryAction.icon}
            {primaryAction.label}
          </Button>
        )}
        {secondaryActions && secondaryActions.length > 0 && (
          <DropdownMenu>
            <DropdownMenuTrigger
              render={
                <Button type="button" variant="outline" size="icon" aria-label="More actions">
                  <MoreVertical className="h-4 w-4" aria-hidden="true" />
                </Button>
              }
            />
            <DropdownMenuContent align="end">
              {secondaryActions.map((action) => (
                <DropdownMenuItem
                  key={action.label}
                  variant={action.destructive ? 'destructive' : 'default'}
                  disabled={action.disabled}
                  onClick={action.onClick}
                >
                  {action.icon}
                  {action.label}
                </DropdownMenuItem>
              ))}
            </DropdownMenuContent>
          </DropdownMenu>
        )}
      </div>
      {children}
    </div>
  )
}

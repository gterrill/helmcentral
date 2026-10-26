import { useEffect, type ReactNode } from 'react'
import { ArrowLeft, ChevronLeft, ChevronRight, MoreVertical } from 'lucide-react'

import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSub,
  DropdownMenuSubContent,
  DropdownMenuSubTrigger,
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
  /** Turns the entry into a submenu (Documents' "New note" and its kinds).
   * The parent's own onClick is not used then. */
  submenu?: PageAction[]
  /** Fires on hover or focus of a submenu trigger - for warming a lazy chunk
   * the entry is about to need. */
  onIntent?: () => void
}

/** A neighbouring record the pager can step to. */
export interface PagerTarget {
  /** What the neighbour is called, for the button's name. */
  label: string
  onSelect: () => void
}

/** Step to the record before or after this one. A missing end is a disabled
 * button, never a wrap. The page decides what "before" and "after" mean. */
export interface PagerProps {
  previous?: PagerTarget | null
  next?: PagerTarget | null
}

// Keys typed into a field are text, not navigation.
function isTypingTarget(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) return false
  if (target.closest('input, textarea, select')) return true
  return target.closest('[contenteditable]:not([contenteditable="false"])') !== null
}

// A key pressed inside an open dialog or menu belongs to it: moving page
// underneath would swap the record the dialog is about.
function isInOverlay(target: EventTarget | null): boolean {
  if (!(target instanceof Element)) return false
  return target.closest('[role="dialog"], [role="alertdialog"], [role="menu"], [role="listbox"]') !== null
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
  /** Previous and next arrows at the right of the title, and the [ and ] keys. */
  pager?: PagerProps
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
  pager,
  children,
  className,
}: PageProps) {
  const previous = pager?.previous ?? null
  const next = pager?.next ?? null
  const hasPager = pager !== undefined
  useEffect(() => {
    if (!hasPager) return
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.metaKey || event.ctrlKey || event.altKey) return
      if (event.key !== '[' && event.key !== ']') return
      if (event.repeat || event.defaultPrevented) return
      if (isTypingTarget(event.target) || isInOverlay(event.target)) return
      const target = event.key === '[' ? previous : next
      if (!target) return
      event.preventDefault()
      target.onSelect()
    }
    window.addEventListener('keydown', onKeyDown)
    return () => window.removeEventListener('keydown', onKeyDown)
  }, [hasPager, previous, next])

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
        {hasPager && (
          <div className="flex shrink-0 items-center">
            <Button
              type="button"
              variant="ghost"
              size="icon"
              aria-label={previous ? `Previous: ${previous.label}` : 'Previous'}
              title={previous ? `Previous: ${previous.label} ([)` : undefined}
              disabled={!previous}
              onClick={previous?.onSelect}
            >
              <ChevronLeft className="h-4 w-4" aria-hidden="true" />
            </Button>
            <Button
              type="button"
              variant="ghost"
              size="icon"
              aria-label={next ? `Next: ${next.label}` : 'Next'}
              title={next ? `Next: ${next.label} (])` : undefined}
              disabled={!next}
              onClick={next?.onSelect}
            >
              <ChevronRight className="h-4 w-4" aria-hidden="true" />
            </Button>
          </div>
        )}
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
              {secondaryActions.map((action) =>
                action.submenu ? (
                  <DropdownMenuSub key={action.label}>
                    <DropdownMenuSubTrigger
                      disabled={action.disabled}
                      onPointerEnter={action.onIntent}
                      onFocus={action.onIntent}
                    >
                      {action.icon}
                      {action.label}
                    </DropdownMenuSubTrigger>
                    <DropdownMenuSubContent>
                      {action.submenu.map((entry) => (
                        <DropdownMenuItem
                          key={entry.label}
                          variant={entry.destructive ? 'destructive' : 'default'}
                          disabled={entry.disabled}
                          onClick={entry.onClick}
                        >
                          {entry.icon}
                          {entry.label}
                        </DropdownMenuItem>
                      ))}
                    </DropdownMenuSubContent>
                  </DropdownMenuSub>
                ) : (
                  <DropdownMenuItem
                    key={action.label}
                    variant={action.destructive ? 'destructive' : 'default'}
                    disabled={action.disabled}
                    onClick={action.onClick}
                  >
                    {action.icon}
                    {action.label}
                  </DropdownMenuItem>
                ),
              )}
            </DropdownMenuContent>
          </DropdownMenu>
        )}
      </div>
      {children}
    </div>
  )
}

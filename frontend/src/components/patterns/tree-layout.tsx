import { Folder, PanelLeftClose, PanelLeftOpen } from 'lucide-react'
import { useCallback, useState, type ReactNode } from 'react'

import { Button } from '@/components/ui/button'
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from '@/components/ui/sheet'
import { useIsMobile } from '@/hooks/use-mobile'
import { cn } from '@/lib/utils'

// ADR 0170: the CRUD pattern library's tree layout - a navigation pane
// (typically a FolderTree) beside the content. On a wide screen the pane is
// always there and can be hidden; below the mobile breakpoint it lives in a
// left Sheet behind a "Folders" button, so the content keeps the full width.

export interface TreeLayoutProps {
  /** The pane's contents. A function receives `dismiss`, which closes the
   * Sheet on a phone (a no-op on a wide screen). */
  pane: ReactNode | ((ctx: { dismiss: () => void }) => ReactNode)
  /** Names the pane: the button, the sheet title and the region label. */
  paneLabel?: string
  /** localStorage key remembering whether the wide-screen pane is hidden. */
  collapsedStorageKey?: string
  children: ReactNode
  className?: string
}

function loadCollapsed(key: string | undefined): boolean {
  if (!key) return false
  try {
    return localStorage.getItem(key) === '1'
  } catch {
    return false
  }
}

function saveCollapsed(key: string | undefined, collapsed: boolean): void {
  if (!key) return
  try {
    localStorage.setItem(key, collapsed ? '1' : '0')
  } catch {
    // Remembering the pane state is a convenience only.
  }
}

export function TreeLayout({ pane, paneLabel = 'Folders', collapsedStorageKey, children, className }: TreeLayoutProps) {
  const isMobile = useIsMobile()
  const [sheetOpen, setSheetOpen] = useState(false)
  const [collapsed, setCollapsed] = useState(() => loadCollapsed(collapsedStorageKey))
  const dismiss = useCallback(() => setSheetOpen(false), [])
  const renderPane = (ctx: { dismiss: () => void }) => (typeof pane === 'function' ? pane(ctx) : pane)

  const setCollapsedAndRemember = (next: boolean) => {
    setCollapsed(next)
    saveCollapsed(collapsedStorageKey, next)
  }

  if (isMobile) {
    return (
      <div className={cn('flex min-h-0 flex-1 flex-col gap-4', className)}>
        <div>
          <Button type="button" variant="outline" size="sm" onClick={() => setSheetOpen(true)}>
            <Folder className="h-4 w-4" data-icon="inline-start" aria-hidden="true" />
            {paneLabel}
          </Button>
        </div>
        <Sheet open={sheetOpen} onOpenChange={setSheetOpen}>
          <SheetContent side="left" className="flex w-72 flex-col gap-2 p-3">
            <SheetHeader>
              <SheetTitle>{paneLabel}</SheetTitle>
              <SheetDescription className="sr-only">Choose where to look.</SheetDescription>
            </SheetHeader>
            <div className="min-h-0 flex-1 overflow-y-auto">{renderPane({ dismiss })}</div>
          </SheetContent>
        </Sheet>
        <div className="flex min-h-0 min-w-0 flex-1 flex-col gap-4">{children}</div>
      </div>
    )
  }

  return (
    <div className={cn('flex min-h-0 flex-1 gap-4', className)}>
      {collapsed ? null : (
        <aside
          aria-label={paneLabel}
          className="flex w-56 shrink-0 flex-col gap-1 rounded-md border border-border bg-card p-2"
        >
          <div className="flex items-center justify-between gap-2 px-1">
            <span className="text-[10px] font-medium uppercase tracking-wider text-muted-foreground">{paneLabel}</span>
            <Button
              type="button"
              variant="ghost"
              size="icon-sm"
              aria-label={`Hide ${paneLabel.toLowerCase()}`}
              onClick={() => setCollapsedAndRemember(true)}
            >
              <PanelLeftClose className="h-4 w-4" aria-hidden="true" />
            </Button>
          </div>
          <div className="min-h-0 flex-1 overflow-y-auto">{renderPane({ dismiss: () => {} })}</div>
        </aside>
      )}
      <div className="flex min-h-0 min-w-0 flex-1 flex-col gap-4">
        {collapsed && (
          <div>
            <Button type="button" variant="outline" size="sm" onClick={() => setCollapsedAndRemember(false)}>
              <PanelLeftOpen className="h-4 w-4" data-icon="inline-start" aria-hidden="true" />
              {`Show ${paneLabel.toLowerCase()}`}
            </Button>
          </div>
        )}
        {children}
      </div>
    </div>
  )
}

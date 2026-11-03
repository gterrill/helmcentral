import { Folder, type LucideIcon } from 'lucide-react'
import { useCallback, useId, useState, type ReactNode } from 'react'

import { SidebarPaneHeader, SidebarPaneRail } from '@/components/patterns/sidebar-pane'
import { Button } from '@/components/ui/button'
import { Sidebar, SidebarContent } from '@/components/ui/sidebar'
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from '@/components/ui/sheet'
import { useIsMobile } from '@/hooks/use-mobile'
import { cn } from '@/lib/utils'

// ADR 0170: the CRUD pattern library's tree layout - a navigation pane
// (typically a FolderTree) beside the content. On a wide screen the pane is
// the shared Sidebar, in the same form as the Rode Planner's (mirrored to the
// left), and hides to a slim rail; below the mobile breakpoint it lives in a
// left Sheet behind a "Folders" button, so the content keeps the full width.

export interface TreeLayoutProps {
  /** The pane's contents. A function receives `dismiss`, which closes the
   * Sheet on a phone (a no-op on a wide screen). */
  pane: ReactNode | ((ctx: { dismiss: () => void }) => ReactNode)
  /** Names the pane: the button, the sheet title and the region label. */
  paneLabel?: string
  /** The icon beside the pane's name in its header and on the collapsed rail. */
  paneIcon?: LucideIcon
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

export function TreeLayout({ pane, paneLabel = 'Folders', paneIcon: PaneIcon = Folder, collapsedStorageKey, children, className }: TreeLayoutProps) {
  const isMobile = useIsMobile()
  const contentId = useId()
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
            <PaneIcon className="h-4 w-4" data-icon="inline-start" aria-hidden="true" />
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
      {collapsed ? (
        <SidebarPaneRail
          side="left"
          icon={PaneIcon}
          label={`Show ${paneLabel.toLowerCase()}`}
          onShow={() => setCollapsedAndRemember(false)}
        />
      ) : (
        <div className="w-64 shrink-0">
          <Sidebar side="left" collapsible="none" role="complementary" aria-label={paneLabel} className="h-full w-full rounded-xl border">
            <SidebarPaneHeader
              side="left"
              icon={PaneIcon}
              label={paneLabel}
              hideLabel={`Hide ${paneLabel.toLowerCase()}`}
              controlsId={contentId}
              onHide={() => setCollapsedAndRemember(true)}
            />
            <SidebarContent id={contentId} className="p-2">
              {renderPane({ dismiss: () => {} })}
            </SidebarContent>
          </Sidebar>
        </div>
      )}
      <div className="flex min-h-0 min-w-0 flex-1 flex-col gap-4">{children}</div>
    </div>
  )
}

import { useLayoutEffect, useRef, useState } from 'react'
import { Anchor, ArrowDown, ArrowUp, ArrowUpDown, ChevronDown, MonitorPlay, Plus } from 'lucide-react'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { Button } from '@/components/ui/button'
import { Separator } from '@/components/ui/separator'
import type { DashboardPage } from '@/hooks/use-dashboard-pages'
import { BREAKPOINTS, useMinWidth } from '@/lib/breakpoints'

/**
 * A page flagged for the wall display (ADR 0089) stays in this same list -
 * there is no separate kiosk page list - so it needs a glyph rather than a
 * different location. MonitorPlay plus its duration; an Anchor glyph on top
 * for a page that only shows while anchored, since "conditional" is the one
 * fact a bare duration can't convey.
 */
export function KioskPageGlyph({ page }: { page: DashboardPage }) {
  if (!page.kiosk || !page.kiosk_seconds) return null
  const whenSuffix = page.kiosk_when && page.kiosk_when !== 'always' ? `, while ${page.kiosk_when}` : ''
  return (
    <span
      className="inline-flex shrink-0 items-center gap-0.5 text-[10px] text-muted-foreground"
      title={`On the wall display, ${page.kiosk_seconds}s${whenSuffix}`}
    >
      <MonitorPlay className="h-3 w-3" aria-hidden="true" />
      {page.kiosk_seconds}s
      {page.kiosk_when === 'anchored' && <Anchor className="h-3 w-3" aria-hidden="true" />}
    </span>
  )
}

interface DashboardPageSwitcherProps {
  pages: DashboardPage[]
  activePageId: string | null
  onSelect: (id: string) => void
  onCreate: () => void
  onReorder?: (ids: string[]) => Promise<boolean>
  reordering?: boolean
  canWrite?: boolean
}

export function DashboardPageSwitcher({
  pages,
  activePageId,
  onSelect,
  onCreate,
  onReorder,
  reordering = false,
  canWrite = true,
}: DashboardPageSwitcherProps) {
  const [open, setOpen] = useState(false)
  const [reorderMode, setReorderMode] = useState(false)
  const [orderMessage, setOrderMessage] = useState('')
  const restoreReorderFocus = useRef(false)
  const reorderButtonRef = useRef<HTMLButtonElement>(null)
  // Rename and Delete page both live in the layout toolbar now (ADR 0107),
  // and that toolbar only renders in layout mode, which itself only exists
  // at this same breakpoint (App.tsx's own canEditLayout). A page created
  // below it would be named "Untitled page" with no toolbar reachable to
  // fix that or delete it, so New Page follows the same gate. Select and
  // reorder need no toolbar, so neither is gated here.
  const canEditLayout = useMinWidth(BREAKPOINTS.lg)

  useLayoutEffect(() => {
    if (!reorderMode && restoreReorderFocus.current) {
      restoreReorderFocus.current = false
      reorderButtonRef.current?.focus()
    }
  }, [reorderMode])

  const activePage = pages.find((p) => p.id === activePageId)
  const displayName = activePage?.name ?? 'Dashboard'

  const handleSelectPage = (pageId: string) => {
    onSelect(pageId)
    setOpen(false)
  }

  const handleCreatePage = () => {
    onCreate()
    setOpen(false)
  }

  const handleMove = async (index: number, direction: -1 | 1) => {
    const destination = index + direction
    if (!canWrite || !onReorder || reordering || destination < 0 || destination >= pages.length) return
    const ids = pages.map((page) => page.id)
    ;[ids[index], ids[destination]] = [ids[destination], ids[index]]
    setOrderMessage('')
    const saved = await onReorder(ids)
    setOrderMessage(saved
      ? `${pages[index].name} moved to position ${destination + 1} of ${pages.length}.`
      : 'Order not saved. Try again; reload if the page list has changed.')
  }

  return (
    <Popover open={open} onOpenChange={(nextOpen) => {
      setOpen(nextOpen)
      if (!nextOpen) {
        setReorderMode(false)
        setOrderMessage('')
      }
    }}>
      <PopoverTrigger
        className="inline-flex items-center gap-1 rounded-md border px-2 py-1 text-[10px] font-semibold uppercase tracking-[0.08em] transition-colors border-border bg-background/70 text-muted-foreground hover:border-primary/40 hover:text-primary md:text-[11px]"
        aria-label="Switch dashboard page"
      >
        {/* Icon-only below `sm`: the header has no room for the page name at phone
            width, and the aria-label above carries the accessible name. The popover
            itself stays — create and select live only here, so hiding the whole
            control would strand them (the sidebar drawer only navigates). */}
        <span className="hidden sm:inline">{displayName}</span>
        <ChevronDown className="h-3.5 w-3.5" />
      </PopoverTrigger>
      <PopoverContent className="w-72 max-w-[calc(100vw-1rem)] max-h-(--available-height) overflow-y-auto p-1">
        {reorderMode && canWrite ? (
          <div className="flex min-w-0 flex-col gap-1">
            <div className="flex min-w-0 items-center justify-between gap-2 px-2">
              <h2 className="truncate text-sm font-semibold">Reorder pages</h2>
              <Button variant="ghost" size="sm" autoFocus onClick={() => {
                restoreReorderFocus.current = true
                setReorderMode(false)
              }}>Done</Button>
            </div>
            <p className="px-2 text-xs text-muted-foreground">Changes save automatically for all devices.</p>
            <ol role="list" aria-label="Dashboard page order" className="flex flex-col">
              {pages.map((page, index) => (
                <li key={page.id} className="flex min-w-0 items-center gap-2 rounded-xs px-2 py-1">
                  <span className="w-4 shrink-0 text-xs tabular-nums text-muted-foreground" aria-hidden="true">{index + 1}</span>
                  <span className="min-w-0 flex-1 truncate text-sm">{page.name}</span>
                  <div className="flex shrink-0 gap-1">
                    <Button variant="ghost" size="icon" className="data-disabled:opacity-50" aria-label={`Move ${page.name} up`}
                      disabled={reordering || index === 0} focusableWhenDisabled
                      onClick={() => { void handleMove(index, -1) }}>
                      <ArrowUp size={16} aria-hidden="true" />
                    </Button>
                    <Button variant="ghost" size="icon" className="data-disabled:opacity-50" aria-label={`Move ${page.name} down`}
                      disabled={reordering || index === pages.length - 1} focusableWhenDisabled
                      onClick={() => { void handleMove(index, 1) }}>
                      <ArrowDown size={16} aria-hidden="true" />
                    </Button>
                  </div>
                </li>
              ))}
            </ol>
            <p role="status" className="min-h-4 px-2 pb-1 text-xs text-muted-foreground">
              {reordering ? 'Saving order…' : orderMessage}
            </p>
          </div>
        ) : (
        <div className="flex flex-col">
          {pages.map((page) => (
            <div
              key={page.id}
              className="flex min-w-0 items-center justify-between rounded-xs px-2 py-1.5 text-sm hover:bg-accent hover:text-accent-foreground"
            >
              <button
                type="button"
                onClick={() => handleSelectPage(page.id)}
                className="flex min-h-10 min-w-0 flex-1 items-center gap-1.5 text-left"
              >
                <span className="min-w-0 flex-1 truncate">{page.name}</span>
                <KioskPageGlyph page={page} />
              </button>
              {/* Renaming and deleting both live in the layout toolbar now
                  (ADR 0107), not here — no pencil, no trash, no inline edit
                  state, no confirmation dialog. This popover only selects,
                  reorders and creates. */}
            </div>
          ))}
          {canWrite && <div className="mt-1 flex flex-col gap-1 pt-1">
            <Separator />
            {onReorder && pages.length > 1 && (
              <Button variant="ghost" className="w-full" ref={reorderButtonRef} onClick={() => {
                setOrderMessage('')
                setReorderMode(true)
              }}>
                <ArrowUpDown size={16} data-icon="inline-start" aria-hidden="true" />
                Reorder pages
              </Button>
            )}
            {canEditLayout && (
              <button
                type="button"
                onClick={handleCreatePage}
                disabled={reordering}
                className="inline-flex min-h-10 w-full items-center justify-center gap-1 rounded-xs px-3 py-1.5 text-xs font-semibold uppercase tracking-[0.1em] text-muted-foreground hover:bg-accent hover:text-accent-foreground"
              >
                <Plus className="h-3.5 w-3.5" />
                New Page
              </button>
            )}
          </div>}
        </div>
        )}
      </PopoverContent>
    </Popover>
  )
}

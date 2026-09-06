import { useLayoutEffect, useRef, useState } from 'react'
import { ArrowDown, ArrowUp, ArrowUpDown, ChevronDown, Plus, Pencil, Trash2 } from 'lucide-react'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { Button } from '@/components/ui/button'
import { Separator } from '@/components/ui/separator'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import type { DashboardPage } from '@/hooks/use-dashboard-pages'

interface DashboardPageSwitcherProps {
  pages: DashboardPage[]
  activePageId: string | null
  onSelect: (id: string) => void
  onCreate: () => void
  onRename: (id: string, name: string) => void
  onDelete: (id: string) => void
  onReorder?: (ids: string[]) => Promise<boolean>
  reordering?: boolean
  canWrite?: boolean
}

export function DashboardPageSwitcher({
  pages,
  activePageId,
  onSelect,
  onCreate,
  onRename,
  onDelete,
  onReorder,
  reordering = false,
  canWrite = true,
}: DashboardPageSwitcherProps) {
  const [open, setOpen] = useState(false)
  const [reorderMode, setReorderMode] = useState(false)
  const [orderMessage, setOrderMessage] = useState('')
  const restoreReorderFocus = useRef(false)
  const reorderButtonRef = useRef<HTMLButtonElement>(null)

  useLayoutEffect(() => {
    if (!reorderMode && restoreReorderFocus.current) {
      restoreReorderFocus.current = false
      reorderButtonRef.current?.focus()
    }
  }, [reorderMode])
  const [editing, setEditing] = useState<{ id: string; name: string } | null>(null)
  // A page holds a whole board of tiles and there is no undo, so the trash
  // icon confirms before it does anything (design critique batch). Named
  // pages get named here too: the copy says which page is about to go, not
  // just "are you sure".
  const [pendingDelete, setPendingDelete] = useState<{ id: string; name: string } | null>(null)

  const activePage = pages.find((p) => p.id === activePageId)
  const displayName = activePage?.name ?? 'Dashboard'

  const handleEditStart = (page: DashboardPage) => {
    setEditing({ id: page.id, name: page.name })
  }

  const handleEditCancel = () => {
    setEditing(null)
  }

  const handleEditConfirm = (pageId: string, currentName: string) => {
    const trimmed = (editing?.name ?? '').trim()
    if (trimmed && trimmed !== currentName) {
      onRename(pageId, trimmed)
    }
    setEditing(null)
  }

  // Enter-specific: the input selects all its text on focus (so typing
  // immediately replaces it), which means a single Backspace - an entirely
  // natural way to "remove characters" - wipes the whole name in one
  // keystroke. Silently closing back to the unchanged name here (as blur
  // does) would look exactly like "I renamed it and nothing happened," with
  // no indication why - so on Enter specifically, an empty name stays in
  // edit mode instead of closing, making the no-op visible.
  const handleEditSubmit = (pageId: string, currentName: string) => {
    if (!(editing?.name ?? '').trim()) return
    handleEditConfirm(pageId, currentName)
  }

  const handleKeyDown = (e: React.KeyboardEvent<HTMLInputElement>, pageId: string, currentName: string) => {
    if (e.key === 'Enter') {
      e.preventDefault()
      handleEditSubmit(pageId, currentName)
    } else if (e.key === 'Escape') {
      e.preventDefault()
      handleEditCancel()
    }
  }

  const handleSelectPage = (pageId: string) => {
    onSelect(pageId)
    setOpen(false)
  }

  const handleCreatePage = () => {
    onCreate()
    setOpen(false)
  }

  const handleConfirmDelete = () => {
    if (pendingDelete) onDelete(pendingDelete.id)
    setPendingDelete(null)
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
    <>
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
              itself stays — create/rename/delete live only here, so hiding the whole
              control would strand them (the sidebar drawer only navigates). */}
          <span className="hidden sm:inline">{displayName}</span>
          <ChevronDown className="h-3.5 w-3.5" />
        </PopoverTrigger>
        <PopoverContent className="w-72 max-w-[calc(100vw-1rem)] max-h-[var(--available-height)] overflow-y-auto p-1">
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
                  <li key={page.id} className="flex min-w-0 items-center gap-2 rounded-sm px-2 py-1">
                    <span className="w-4 shrink-0 text-xs tabular-nums text-muted-foreground" aria-hidden="true">{index + 1}</span>
                    <span className="min-w-0 flex-1 truncate text-sm">{page.name}</span>
                    <div className="flex shrink-0 gap-1">
                      <Button variant="ghost" size="icon" className="data-[disabled]:opacity-50" aria-label={`Move ${page.name} up`}
                        disabled={reordering || index === 0} focusableWhenDisabled
                        onClick={() => { void handleMove(index, -1) }}>
                        <ArrowUp size={16} aria-hidden="true" />
                      </Button>
                      <Button variant="ghost" size="icon" className="data-[disabled]:opacity-50" aria-label={`Move ${page.name} down`}
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
                className="flex min-w-0 items-center justify-between rounded-sm px-2 py-1.5 text-sm hover:bg-accent hover:text-accent-foreground"
              >
                {editing?.id === page.id ? (
                  <input
                    type="text"
                    value={editing.name}
                    onChange={(e) => setEditing({ id: page.id, name: e.target.value })}
                    onKeyDown={(e) => handleKeyDown(e, page.id, page.name)}
                    onBlur={() => handleEditConfirm(page.id, page.name)}
                    autoFocus
                    onFocus={(e) => e.currentTarget.select()}
                    className="min-w-0 flex-1 rounded border bg-background px-1 py-0.5 text-sm focus:outline-none focus:ring-1 focus:ring-primary"
                  />
                ) : (
                  <>
                    <button
                      type="button"
                      onClick={() => handleSelectPage(page.id)}
                      className="min-h-10 min-w-0 flex-1 truncate text-left"
                    >
                      {page.name}
                    </button>
                    {canWrite && <div className="flex shrink-0 items-center gap-1">
                      <button
                        type="button"
                        disabled={reordering}
                        onClick={() => handleEditStart(page)}
                        className="inline-flex size-10 items-center justify-center rounded p-0.5 hover:bg-accent"
                        aria-label={`Rename ${page.name}`}
                      >
                        <Pencil className="h-3.5 w-3.5" />
                      </button>
                      {pages.length > 1 && (
                        <button
                          type="button"
                          disabled={reordering}
                          onClick={() => setPendingDelete({ id: page.id, name: page.name })}
                          className="inline-flex size-10 items-center justify-center rounded p-0.5 hover:bg-accent hover:text-destructive"
                          aria-label={`Delete ${page.name}`}
                        >
                          <Trash2 className="h-3.5 w-3.5" />
                        </button>
                      )}
                    </div>}
                  </>
                )}
              </div>
            ))}
            {canWrite && <div className="mt-1 flex flex-col gap-1 pt-1">
              <Separator />
              {onReorder && pages.length > 1 && (
                <Button variant="ghost" className="w-full" ref={reorderButtonRef} onClick={() => {
                  setEditing(null)
                  setOrderMessage('')
                  setReorderMode(true)
                }}>
                  <ArrowUpDown size={16} data-icon="inline-start" aria-hidden="true" />
                  Reorder pages
                </Button>
              )}
              <button
                type="button"
                onClick={handleCreatePage}
                disabled={reordering}
                className="inline-flex min-h-10 w-full items-center justify-center gap-1 rounded-sm px-3 py-1.5 text-xs font-semibold uppercase tracking-[0.1em] text-muted-foreground hover:bg-accent hover:text-accent-foreground"
              >
                <Plus className="h-3.5 w-3.5" />
                New Page
              </button>
            </div>}
          </div>
          )}
        </PopoverContent>
      </Popover>

      {/* A sibling of Popover, not nested inside PopoverContent: the popover
          can close itself on an outside click while this is still open, and
          the confirmation must survive that. pendingDelete lives in this
          component's own state either way, so it does. */}
      <AlertDialog
        open={pendingDelete !== null}
        onOpenChange={(isOpen) => {
          if (!isOpen) setPendingDelete(null)
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Delete "{pendingDelete?.name}"?</AlertDialogTitle>
            <AlertDialogDescription>
              This removes the page and every tile on it. It can't be undone.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction
              className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
              onClick={handleConfirmDelete}
            >
              Delete
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  )
}

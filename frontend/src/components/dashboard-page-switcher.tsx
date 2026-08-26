import { useState } from 'react'
import { ChevronDown, Plus, Pencil, Trash2 } from 'lucide-react'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import type { DashboardPage } from '@/hooks/use-dashboard-pages'

interface DashboardPageSwitcherProps {
  pages: DashboardPage[]
  activePageId: string | null
  onSelect: (id: string) => void
  onCreate: () => void
  onRename: (id: string, name: string) => void
  onDelete: (id: string) => void
  onSetSkin: (id: string, skin: 'default' | 'instrument') => void
}

export function DashboardPageSwitcher({
  pages,
  activePageId,
  onSelect,
  onCreate,
  onRename,
  onDelete,
  onSetSkin,
}: DashboardPageSwitcherProps) {
  const [open, setOpen] = useState(false)
  const [editing, setEditing] = useState<{ id: string; name: string } | null>(null)

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

  return (
    <Popover open={open} onOpenChange={setOpen}>
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
      <PopoverContent className="w-56 p-1">
        <div className="flex flex-col">
          {pages.map((page) => (
            <div
              key={page.id}
              className="flex items-center justify-between rounded-sm px-2 py-1.5 text-sm hover:bg-accent hover:text-accent-foreground"
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
                  className="flex-1 rounded border bg-background px-1 py-0.5 text-sm focus:outline-none focus:ring-1 focus:ring-primary"
                />
              ) : (
                <>
                  <button
                    type="button"
                    onClick={() => handleSelectPage(page.id)}
                    className="flex-1 text-left"
                  >
                    {page.name}
                  </button>
                  <div className="flex items-center gap-1">
                    <button
                      type="button"
                      onClick={() => handleEditStart(page)}
                      className="inline-flex items-center rounded p-0.5 hover:bg-accent"
                      aria-label={`Rename ${page.name}`}
                    >
                      <Pencil className="h-3.5 w-3.5" />
                    </button>
                    {pages.length > 1 && (
                      <button
                        type="button"
                        onClick={() => onDelete(page.id)}
                        className="inline-flex items-center rounded p-0.5 hover:bg-accent hover:text-destructive"
                        aria-label={`Delete ${page.name}`}
                      >
                        <Trash2 className="h-3.5 w-3.5" />
                      </button>
                    )}
                  </div>
                </>
              )}
            </div>
          ))}
          {/* Edits the active page only (ADR 0060) — the per-page rows above
              already carry a rename input and two icon buttons, with no room
              for a third control, so the skin lives here instead. */}
          {activePage && (
            <div className="border-t pt-1 mt-1 px-2 pb-1.5">
              {/* Names the control, not just the page: a bare page name above a
                  select reads as "pick a page" rather than "skin for this one",
                  and the rows above already do the picking. */}
              <p className="mb-1 text-xs font-semibold uppercase tracking-[0.1em] text-muted-foreground">
                Skin &middot; {activePage.name}
              </p>
              <select
                aria-label="Instrument skin"
                className="h-9 w-full rounded-md border bg-transparent px-3 text-sm"
                value={activePage.skin ?? 'default'}
                onChange={(e) => onSetSkin(activePage.id, e.target.value as 'default' | 'instrument')}
              >
                <option value="instrument">Instrument (always dark)</option>
                <option value="default">Follow the app theme</option>
              </select>
            </div>
          )}
          <div className="border-t pt-1 mt-1">
            <button
              type="button"
              onClick={handleCreatePage}
              className="inline-flex w-full items-center justify-center gap-1 rounded-sm px-3 py-1.5 text-xs font-semibold uppercase tracking-[0.1em] text-muted-foreground hover:bg-accent hover:text-accent-foreground"
            >
              <Plus className="h-3.5 w-3.5" />
              New Page
            </button>
          </div>
        </div>
      </PopoverContent>
    </Popover>
  )
}

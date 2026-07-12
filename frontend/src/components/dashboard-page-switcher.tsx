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
}

export function DashboardPageSwitcher({
  pages,
  activePageId,
  onSelect,
  onCreate,
  onRename,
  onDelete,
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
        {displayName}
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

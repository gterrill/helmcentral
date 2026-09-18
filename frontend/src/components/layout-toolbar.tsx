import { useState } from 'react'
import { LampCeiling, Trash2 } from 'lucide-react'

import { AddWidgetPicker, type AddWidgetMultiInstanceEntry } from '@/components/add-widget-picker'
import { PageHeroSelect } from '@/components/page-hero-select'
import { PageKioskSelect, type KioskPatch } from '@/components/page-kiosk-select'
import { PageSkinSelect } from '@/components/page-skin-select'
import { PageTitleField } from '@/components/page-title-field'
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
import type { BuiltinWidgetId } from '@/lib/dashboard-widgets'

interface LayoutToolbarProps {
  page: DashboardPage | null
  /** App.tsx's namingPageId — the page whose name field starts empty and focused. */
  namingPageId: string | null
  /** Resolves to whether the save succeeded — see page-title-field.tsx's onSave. */
  onSaveName: (id: string, name: string) => Promise<boolean>
  onDoneNaming: () => void
  placedWidgetIds: readonly string[]
  onAddWidget: (id: BuiltinWidgetId) => void
  multiInstanceEntries: readonly AddWidgetMultiInstanceEntry[]
  onOpenRibbon: () => void
  onSetSkin: (id: string, skin: 'default' | 'instrument') => void
  onSetHero: (id: string, hero: string) => void
  onKioskPatch: (id: string, patch: KioskPatch) => void
  /** Deletes `page`. Same App.tsx handler the page switcher used to call,
   * just rewired to this toolbar (see the Delete control below). */
  onDeletePage: (id: string) => void
  /** Total page count, for the same "never delete the last page" rule the
   * switcher used to enforce (`pages.length > 1`). */
  pageCount: number
  canWrite?: boolean
}

/**
 * The layout toolbar (ADR 0107): sits where the "Layout Mode — Drag to
 * rearrange" pill used to, above the grid, and replaces the separate control
 * row that used to sit below it. One row, one fixed order — page name field,
 * Add Widget, Ribbon, Skin, Hero, Kiosk, then Delete page.
 *
 * Kiosk stays last among the ordinary controls: it's the one control that
 * grows extra fields (a seconds input and a condition select) the moment you
 * tick it, so it never pushes anything after it around. Delete page sits
 * after it, separated by a divider — it is the one destructive control in
 * this row and the popover it used to live in put the same clear space
 * around it (a Separator before "New Page"), so this keeps that same
 * distance rather than sitting flush against Kiosk.
 */
export function LayoutToolbar({
  page,
  namingPageId,
  onSaveName,
  onDoneNaming,
  placedWidgetIds,
  onAddWidget,
  multiInstanceEntries,
  onOpenRibbon,
  onSetSkin,
  onSetHero,
  onKioskPatch,
  onDeletePage,
  pageCount,
  canWrite = true,
}: LayoutToolbarProps) {
  // A page holds a whole board of tiles and there is no undo, so this
  // confirms before it does anything (moved here, with the same wording,
  // from the page switcher's old per-row trash icon — ADR 0107 follow-up).
  const [pendingDelete, setPendingDelete] = useState<{ id: string; name: string } | null>(null)

  if (!page) return null

  const canDelete = canWrite && pageCount > 1

  return (
    <>
      <div data-testid="layout-toolbar" className="flex w-fit flex-wrap items-center gap-2">
        {canWrite ? (
          <PageTitleField key={page.id} page={page} naming={namingPageId === page.id} onSave={onSaveName} onDone={onDoneNaming} />
        ) : (
          // The old rename pencil was behind canWrite; PageTitleField on its
          // own isn't. Without this, a read-only user could type a name the
          // backend then rejects — the one write surface in this row that
          // wasn't actually gated, same rule Delete page enforces below.
          <span className="min-w-0 w-40 truncate rounded-md border border-transparent px-3 py-1.5 text-xs font-semibold text-foreground sm:w-48">
            {page.name}
          </span>
        )}
        <AddWidgetPicker placedWidgetIds={placedWidgetIds} onAddWidget={onAddWidget} multiInstanceEntries={multiInstanceEntries} />
        <button
          type="button"
          onClick={onOpenRibbon}
          className="inline-flex w-fit items-center gap-1 rounded-md border border-border bg-background/70 px-3 py-1.5 text-xs font-semibold uppercase tracking-[0.1em] text-muted-foreground hover:border-primary/40 hover:text-primary"
        >
          <LampCeiling className="h-3.5 w-3.5" aria-hidden="true" />
          Ribbon
        </button>
        <PageSkinSelect page={page} onSetSkin={onSetSkin} />
        <PageHeroSelect page={page} onSetHero={onSetHero} />
        <PageKioskSelect page={page} onPatch={onKioskPatch} />
        {canDelete && (
          <>
            <Separator orientation="vertical" className="h-6" />
            <button
              type="button"
              onClick={() => setPendingDelete({ id: page.id, name: page.name })}
              className="inline-flex w-fit items-center gap-1 rounded-md border border-destructive/40 bg-background/70 px-3 py-1.5 text-xs font-semibold uppercase tracking-[0.1em] text-destructive hover:border-destructive hover:bg-destructive/10"
            >
              <Trash2 className="h-3.5 w-3.5" aria-hidden="true" />
              Delete page
            </button>
          </>
        )}
      </div>

      {/* A sibling of the toolbar row, not nested inside it: the same
          arrangement this had as a sibling of the switcher's Popover before
          the move, so the row's own flex layout never has to account for it. */}
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
              onClick={() => {
                if (pendingDelete) onDeletePage(pendingDelete.id)
                setPendingDelete(null)
              }}
            >
              Delete
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  )
}

export type { AddWidgetMultiInstanceEntry }

import type { ReactNode } from 'react'

import {
  AlertDialog,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Button } from '@/components/ui/button'

// ADR 0142: the CRUD pattern library's delete confirmation. Deliberately
// does NOT use AlertDialogAction for the destructive button - that wraps
// Base UI's own Close primitive, which closes the dialog on click no matter
// what the async delete does (see alert-dialog.tsx's own doc comment on
// AlertDialogAction: "an async action that only closes on success should
// not be a `.Close`"). A delete that fails should leave the dialog open
// with the caller's own error still visible in `description` or `children`,
// so this composes a plain destructive Button instead and lets the caller
// decide when to call onOpenChange(false) - on a successful delete, not
// before it starts.

export interface ConfirmDeleteProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  title: ReactNode
  description?: ReactNode
  confirmLabel?: string
  cancelLabel?: string
  onConfirm: () => void
  deleting?: boolean
  /** Extra content between the description and the actions - e.g. equipment-
   * editor.tsx's "also delete N photos only this item uses" checkbox and its
   * thumbnails. */
  children?: ReactNode
}

export function ConfirmDelete({
  open,
  onOpenChange,
  title,
  description,
  confirmLabel = 'Delete',
  cancelLabel = 'Cancel',
  onConfirm,
  deleting = false,
  children,
}: ConfirmDeleteProps) {
  return (
    <AlertDialog
      open={open}
      onOpenChange={(isOpen) => {
        // Escape and an outside click both fire this the same way a Cancel
        // click does - Cancel is disabled while deleting (above), but Base
        // UI's own dismiss handling isn't, so it has to be gated here too.
        // Without this, a delete in flight could be dismissed out from
        // under itself and a later failure would land in a dialog nobody
        // can see - the caller's own onConfirm decides when a failed delete
        // is done, not a stray Escape press.
        if (!isOpen && !deleting) onOpenChange(false)
      }}
    >
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{title}</AlertDialogTitle>
          {description && <AlertDialogDescription>{description}</AlertDialogDescription>}
        </AlertDialogHeader>
        {children}
        <AlertDialogFooter>
          <AlertDialogCancel disabled={deleting}>{cancelLabel}</AlertDialogCancel>
          <Button
            type="button"
            className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
            disabled={deleting}
            onClick={onConfirm}
          >
            {deleting ? 'Deleting…' : confirmLabel}
          </Button>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}

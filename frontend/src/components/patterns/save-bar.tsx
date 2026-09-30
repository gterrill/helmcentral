import { useLayoutEffect, useState } from 'react'
import { createPortal } from 'react-dom'

import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'
import { SAVE_BAR_SLOT_ID } from './save-bar-slot'

// ADR 0142: the CRUD pattern library's dirty-draft bar. Renders nothing at
// all while the draft matches its baseline - the caller's own dirty check
// (equipment-editor.tsx's `sameDraft`, or any other form's) decides when
// that's true, this component only ever draws the consequence of it.
//
// Polaris' contextual save bar: while dirty it takes over the app's top bar
// instead of floating at the bottom of the page. It never covers the sidebar
// or the form being edited, and it sits where the operator already looks
// for page-level actions. It gets there by portalling into the SaveBarSlot
// the shell mounts inside its header - resolved by id in a layout effect,
// after the whole commit, so a slot created in the SAME commit (the header
// and the page mounting together) is already in the DOM.
//
// A missing slot throws. A form the operator can edit but not save is worse
// than a loud failure, and quietly falling back to an inline bar would hide
// that the shell was wired wrong.

export interface SaveBarProps {
  dirty: boolean
  saving?: boolean
  error?: string | null
  onSave: () => void
  onDiscard: () => void
  saveLabel?: string
  discardLabel?: string
  className?: string
}

export function SaveBar({
  dirty,
  saving = false,
  error = null,
  onSave,
  onDiscard,
  saveLabel = 'Save',
  discardLabel = 'Discard',
  className,
}: SaveBarProps) {
  const [slot, setSlot] = useState<HTMLElement | null>(null)

  useLayoutEffect(() => {
    if (!dirty) return
    const found = document.getElementById(SAVE_BAR_SLOT_ID)
    if (!found) {
      throw new Error(
        `SaveBar: no #${SAVE_BAR_SLOT_ID} element in the document. The shell must render a <SaveBarSlot /> inside its header.`,
      )
    }
    if (found !== slot) setSlot(found)
  }, [dirty, slot])

  if (!dirty || !slot) return null

  return createPortal(
    <div
      data-slot="save-bar"
      className={cn('flex h-full w-full items-center gap-3 bg-card px-2 sm:px-4', className)}
    >
      <div className="min-w-0 flex-1">
        <p className="truncate text-sm font-medium text-foreground">Unsaved changes</p>
        {error && (
          <p role="alert" className="truncate text-xs text-destructive" title={error}>
            {error}
          </p>
        )}
      </div>
      <Button type="button" variant="outline" onClick={onDiscard} disabled={saving}>
        {discardLabel}
      </Button>
      <Button type="button" onClick={onSave} disabled={saving}>
        {saving ? 'Saving…' : saveLabel}
      </Button>
    </div>,
    slot,
  )
}

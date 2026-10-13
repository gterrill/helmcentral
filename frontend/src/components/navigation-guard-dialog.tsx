import { useState, useSyncExternalStore } from 'react'

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
import { Button } from '@/components/ui/button'
import { useAppRouter } from '@/hooks/use-app-location'
import type { PendingNavigation } from '@/lib/app-router'

// ADR 0171: the question a navigation guard asks. It renders whatever move the
// router is holding, in the words the guard gave it when it held the move
// (not read live: a save finishing under the dialog must not swap Leave/Stay
// for Save and Continue on a page that was never dirty in that sense).
//
// Cancel and Stay keep the move held-and-dropped; Discard and Leave make it;
// Save and Continue saves through the guard's own save and then makes it, and
// stays put with the server's reason when the save fails.
export function NavigationGuardDialog() {
  const router = useAppRouter()
  const pending = useSyncExternalStore(router.subscribe, () => router.getSnapshot().pending)
  // The last move held, kept while the dialog animates closed so its content
  // does not collapse to nothing on the way out.
  const [shown, setShown] = useState<PendingNavigation | null>(pending)
  if (pending !== null && pending !== shown) setShown(pending)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const cancel = () => {
    router.cancelPending()
    setError(null)
  }
  const discard = () => {
    router.confirmPending(shown ?? undefined)
    setError(null)
  }
  const saveAndContinue = async () => {
    setSaving(true)
    setError(null)
    try {
      await router.saveAndContinue()
    } catch (err) {
      // Stay on the page so the operator can fix it and retry. Every page
      // renders its own error banner too, but this dialog is modal and covers
      // it - without repeating the reason here, a rejected save (an
      // unreachable SignalK address, a duplicate document title, a zone/bin
      // that disagree) looks like the button simply did nothing.
      setError(err instanceof Error ? err.message : `Unable to save the ${shown?.label ?? ''} page`)
    } finally {
      setSaving(false)
    }
  }

  return (
    <AlertDialog
      open={pending !== null}
      onOpenChange={(open) => {
        // Not while a save is running: Escape would drop the move the save
        // is about to make.
        if (!open && !saving) cancel()
      }}
    >
      <AlertDialogContent>
        {shown?.kind === 'dirty' ? (
          <>
            <AlertDialogHeader>
              <AlertDialogTitle>Unsaved changes</AlertDialogTitle>
              <AlertDialogDescription>
                You have unsaved changes on the {shown.label} page. Save them before leaving, or discard them?
              </AlertDialogDescription>
            </AlertDialogHeader>
            {error && (
              <div className="rounded-md border border-destructive/30 bg-destructive/5 px-3 py-2 text-xs uppercase tracking-[0.08em] text-destructive">
                {error}
              </div>
            )}
            <AlertDialogFooter>
              <AlertDialogCancel onClick={cancel} disabled={saving}>Cancel</AlertDialogCancel>
              <AlertDialogAction onClick={discard} disabled={saving}>Discard</AlertDialogAction>
              <Button onClick={() => void saveAndContinue()} disabled={saving}>
                {saving ? 'Saving…' : 'Save and Continue'}
              </Button>
            </AlertDialogFooter>
          </>
        ) : (
          // Stocktake's scans and a staged quick-add have nothing to Save -
          // offering that button anyway (or the "Unsaved changes" copy, which
          // implies one) would be offering an action that does not exist.
          // Leave/Stay instead, worded for what is actually about to be cleared.
          <>
            <AlertDialogHeader>
              <AlertDialogTitle>
                {shown?.kind === 'stocktake-work' ? 'Leave stocktake?' : 'Leave this bin?'}
              </AlertDialogTitle>
              <AlertDialogDescription>
                {shown?.kind === 'stocktake-work'
                  ? 'The scans from this pass will be cleared.'
                  // Falls back to the generic copy only when the guard didn't
                  // give a more specific one: a photo still queued for Retry
                  // after a partial-failure save leaves the form's own
                  // name/photo fields empty, so "the name and photos you have
                  // added" would be describing nothing.
                  : (shown?.detail ?? 'The name and photos you have added will be cleared.')}
              </AlertDialogDescription>
            </AlertDialogHeader>
            <AlertDialogFooter>
              <AlertDialogCancel onClick={cancel}>Stay</AlertDialogCancel>
              <AlertDialogAction onClick={discard}>Leave</AlertDialogAction>
            </AlertDialogFooter>
          </>
        )}
      </AlertDialogContent>
    </AlertDialog>
  )
}

import { useEffect, useRef } from 'react'
import { toast } from 'sonner'

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
import type { VersionWatch } from '@/hooks/use-version-reload'

const UPDATE_TOAST_ID = 'update-available'
const CHUNK_TOAST_ID = 'chunk-failure'

interface UpdateNoticesProps {
  watch: VersionWatch
  /** Asks to reload: confirms first when there is unsaved work. */
  requestReload: () => void
}

/**
 * The "Update available" toast and the screen-failed-to-load toast. Rendered
 * beside the shell's <Toaster>. The update toast stays until it is acted on or
 * dismissed, and is raised once per detected build in this tab: dismissing it
 * does not bring it back, the sidebar line carries on.
 */
export function UpdateNotices({ watch, requestReload }: UpdateNoticesProps) {
  const { available, chunkFailure, clearChunkFailure } = watch
  const requestReloadRef = useRef(requestReload)
  requestReloadRef.current = requestReload
  const announced = useRef(new Set<string>())

  const availableKey = available ? `${available.version}@${available.revision}` : null
  useEffect(() => {
    // The build is current again (a rollback, say): the message is stale.
    if (availableKey === null) {
      toast.dismiss(UPDATE_TOAST_ID)
      return
    }
    if (announced.current.has(availableKey)) return
    announced.current.add(availableKey)
    toast('Update available', {
      id: UPDATE_TOAST_ID,
      duration: Infinity,
      action: { label: 'Reload', onClick: () => requestReloadRef.current() },
    })
  }, [availableKey])

  // Shown once and then cleared, so the next failure raises it again.
  useEffect(() => {
    if (chunkFailure === null) return
    toast(
      chunkFailure === 'needs-update'
        ? 'This screen needs the update. Reload to open it.'
        : "Couldn't load this screen. Check the connection.",
      {
        id: CHUNK_TOAST_ID,
        duration: Infinity,
        // A rejected import is cached by the page, so opening the screen again needs a fresh page.
        action: { label: 'Reload', onClick: () => requestReloadRef.current() },
      },
    )
    clearChunkFailure()
  }, [chunkFailure, clearChunkFailure])

  return null
}

interface ReloadConfirmDialogProps {
  open: boolean
  onCancel: () => void
  onConfirm: () => void
}

/** Asked before a requested reload that would drop unsaved work. */
export function ReloadConfirmDialog({ open, onCancel, onConfirm }: ReloadConfirmDialogProps) {
  return (
    <AlertDialog open={open} onOpenChange={(next) => { if (!next) onCancel() }}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>Reload Helmcentral?</AlertDialogTitle>
          <AlertDialogDescription>You have unsaved changes. Reload anyway?</AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel onClick={onCancel}>Stay</AlertDialogCancel>
          <AlertDialogAction onClick={onConfirm}>Reload</AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}

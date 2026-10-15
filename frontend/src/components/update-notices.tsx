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
  const { available, chunkFailure, recheckChunkFailure } = watch
  const requestReloadRef = useRef(requestReload)
  requestReloadRef.current = requestReload
  const announced = useRef(new Set<string>())

  const availableKey = available ? `${available.version}@${available.revision}` : null
  useEffect(() => {
    if (availableKey === null || announced.current.has(availableKey)) return
    announced.current.add(availableKey)
    toast('Update available', {
      id: UPDATE_TOAST_ID,
      duration: Infinity,
      action: { label: 'Reload', onClick: () => requestReloadRef.current() },
    })
  }, [availableKey])

  useEffect(() => {
    if (chunkFailure === 'needs-update') {
      toast('This screen needs the update. Reload to open it.', {
        id: CHUNK_TOAST_ID,
        duration: Infinity,
        action: { label: 'Reload', onClick: () => requestReloadRef.current() },
      })
    } else if (chunkFailure === 'connection') {
      toast("Couldn't load this screen. Check the connection.", {
        id: CHUNK_TOAST_ID,
        duration: Infinity,
        action: { label: 'Retry', onClick: () => recheckChunkFailure() },
      })
    }
  }, [chunkFailure, recheckChunkFailure])

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

import { ConfirmDelete } from '@/components/patterns'
import { errorMessage } from '@/lib/errors'
import { waitingLabel, type InboxQueueStore, type QueuedMeta } from '@/lib/inbox-queue'

export interface DiscardWaitingDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  entries: readonly QueuedMeta[]
  store: InboxQueueStore | null
  /** Called with the reason when the list could not be cleared, and with null when a try begins. */
  onError: (message: string | null) => void
}

/** Asks before the phone's waiting list is emptied, and empties it (ADR 0280). Shared by the banner and the upload page. */
export function DiscardWaitingDialog({ open, onOpenChange, entries, store, onError }: DiscardWaitingDialogProps) {
  const discard = async () => {
    onError(null)
    try {
      await store?.clear()
    } catch (err) {
      onError(errorMessage(err))
    }
    // Closed either way: a message behind an open dialog would not be seen.
    onOpenChange(false)
  }

  return (
    <ConfirmDelete
      open={open}
      onOpenChange={onOpenChange}
      title={`Discard ${waitingLabel(entries)} waiting on this phone?`}
      description="They are taken off the waiting list and will not be uploaded. The originals stay in your phone's photos and files."
      confirmLabel="Discard files"
      onConfirm={() => void discard()}
    />
  )
}

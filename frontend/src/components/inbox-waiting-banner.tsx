import { Inbox } from 'lucide-react'
import { useState } from 'react'

import { bannerClass } from '@/components/connection-banner'
import { DiscardWaitingDialog } from '@/components/discard-waiting-dialog'
import { Button } from '@/components/ui/button'
import { useInboxQueue, useUploadRun } from '@/hooks/use-inbox-queue'
import { waitingLabel, type QueuedMeta } from '@/lib/inbox-queue'
import { cn } from '@/lib/utils'

export interface InboxWaitingBannerProps {
  canWrite: boolean
  /** Opens the upload page, where the operator taps Upload. */
  onUploadNow: () => void
}

/**
 * Shown wherever Helmcentral opens on a phone that still has files waiting to
 * go to the Inbox: shared from the share sheet and not yet answered for by the
 * box (ADR 0280). Files picked on the upload page upload directly and never
 * wait here. Nothing is sent until the operator taps Upload on the upload page.
 */
export function InboxWaitingBanner({ canWrite, onUploadNow }: InboxWaitingBannerProps) {
  // The store reads the count first and the list only when something waits.
  const { entries, error: listError, store } = useInboxQueue()
  const { running } = useUploadRun()
  const [confirming, setConfirming] = useState(false)
  // Kept with the list it happened on: once the list changes the message is stale.
  const [discardError, setDiscardError] = useState<{ message: string; entries: readonly QueuedMeta[] } | null>(null)
  const shownDiscardError = discardError?.entries === entries ? discardError.message : null

  if (entries.length === 0 && !listError) return null

  return (
    <div role="status" className={cn(bannerClass('neutral'), 'flex-wrap items-center')}>
      <Inbox className="size-5 shrink-0 text-muted-foreground" aria-hidden="true" />
      <div className="min-w-0 flex-1">
        {entries.length > 0 && <p className="text-sm font-semibold tabular-nums">{waitingLabel(entries)} waiting to upload</p>}
        {listError && <p role="alert" className="text-xs text-destructive">Could not read the files waiting on this phone: {listError}</p>}
        {shownDiscardError && <p className="text-xs text-destructive">{shownDiscardError}</p>}
      </div>
      {entries.length > 0 && (
        <div className="flex gap-2">
          <Button type="button" size="sm" disabled={!canWrite} onClick={onUploadNow}>Upload now</Button>
          <Button type="button" size="sm" variant="outline" disabled={running} onClick={() => { setDiscardError(null); setConfirming(true) }}>Discard</Button>
        </div>
      )}
      <DiscardWaitingDialog
        open={confirming}
        onOpenChange={setConfirming}
        entries={entries}
        store={store}
        onError={(message) => setDiscardError(message === null ? null : { message, entries })}
      />
    </div>
  )
}

import { Inbox } from 'lucide-react'
import { useState } from 'react'

import { ConfirmDelete } from '@/components/patterns'
import { Button } from '@/components/ui/button'
import { useInboxQueue } from '@/hooks/use-inbox-queue'
import { errorMessage } from '@/lib/errors'
import { requestAutoUpload } from '@/lib/inbox-launch'

export interface InboxWaitingBannerProps {
  canWrite: boolean
  /** Opens the upload page, which starts sending as it opens. */
  onUploadNow: () => void
}

/**
 * Shown wherever Helmcentral opens on a phone that still has files waiting to
 * go to the Inbox: shared from the share sheet or picked on the upload page,
 * and not yet answered for by the box (ADR 0280). Nothing is sent until the
 * operator says so, except straight after a share.
 */
export function InboxWaitingBanner({ canWrite, onUploadNow }: InboxWaitingBannerProps) {
  const { entries, store } = useInboxQueue()
  const [confirming, setConfirming] = useState(false)
  const [error, setError] = useState<string | null>(null)
  if (entries.length === 0) return null

  const allPhotos = entries.every((q) => q.type.startsWith('image/'))
  const noun = allPhotos ? (entries.length === 1 ? 'photo' : 'photos') : (entries.length === 1 ? 'file' : 'files')

  const discard = async () => {
    try {
      await store?.clear()
      setConfirming(false)
    } catch (err) {
      setError(errorMessage(err))
    }
  }

  return (
    <div className="flex min-w-0 shrink-0 flex-wrap items-center gap-3 rounded-lg border border-border bg-card px-4 py-3">
      <Inbox className="size-5 shrink-0 text-muted-foreground" aria-hidden="true" />
      <div className="min-w-0 flex-1">
        <p role="status" className="text-sm font-semibold tabular-nums">{entries.length} {noun} waiting to upload</p>
        {error && <p role="alert" className="text-xs text-destructive">{error}</p>}
      </div>
      <div className="flex gap-2">
        <Button type="button" size="sm" disabled={!canWrite} onClick={() => { requestAutoUpload(); onUploadNow() }}>Upload now</Button>
        <Button type="button" size="sm" variant="outline" onClick={() => setConfirming(true)}>Discard</Button>
      </div>
      <ConfirmDelete
        open={confirming}
        onOpenChange={setConfirming}
        title={`Discard ${entries.length === 1 ? '1 file' : `${entries.length} files`} waiting on this phone?`}
        description="They are taken off the waiting list and will not be uploaded. The originals stay in your phone's photos and files."
        confirmLabel="Discard files"
        onConfirm={() => void discard()}
      />
    </div>
  )
}

import { AlertTriangle, Compass, Copy, FileText, FolderInput, ImagePlus, Route, Trash2 } from 'lucide-react'
import { useMemo, useState } from 'react'
import { toast } from 'sonner'

import { FolderPickerDialog } from '@/components/documents/folder-picker-dialog'
import { InboxAssignDialog } from '@/components/passages/inbox-assign-dialog'
import { ConfirmDelete, EmptyState, Page } from '@/components/patterns'
import { Button } from '@/components/ui/button'
import { useDocumentFolderTree } from '@/hooks/use-document-folder-tree'
import { useInbox } from '@/hooks/use-inbox'
import { useLocalStorageId } from '@/hooks/use-local-storage-id'
import { useVesselStateSelector } from '@/hooks/use-vessel-state'
import { errorMessage } from '@/lib/errors'
import { inboxApi, inboxPreviewUrl, type InboxItem } from '@/lib/inbox-api'
import { findBlurry, findDuplicates, groupInbox } from '@/lib/inbox-groups'
import type { PassageSummary } from '@/lib/passages'
import { cn } from '@/lib/utils'
import { formatTimeOrDayTime, selectTimeZone } from '@/lib/vessel-time'

export interface InboxViewProps {
  canWrite: boolean
  passages: PassageSummary[]
  onBack: () => void
  onOpenUpload: () => void
}

const MISMATCH_TEXT =
  "The phone's own position was more than a nautical mile from the boat's track when this was taken. The phone's clock or time zone may be wrong, so the time may not match the track."

// The Inbox: what was shared from a phone and is waiting to be sorted. A
// contact sheet per group; tapping a picture marks it, and whatever is marked
// can be added to a passage, sent to Documents or deleted. The duplicate and
// soft-focus flags are suggestions: nothing is marked or removed unless the
// operator says so.
export function InboxView({ canWrite, passages, onBack, onOpenUpload }: InboxViewProps) {
  const inbox = useInbox()
  const timeZone = useVesselStateSelector(selectTimeZone)
  const [marked, setMarked] = useState<ReadonlySet<string>>(new Set())
  const [dialog, setDialog] = useState<'passage' | 'documents' | 'bin' | null>(null)
  const [busy, setBusy] = useState(false)

  // The folder last sent to (a per-device convenience) opens the picker, but
  // only while it still exists: it may have been deleted since, on this or
  // another device.
  const [lastFolder, setLastFolder] = useLocalStorageId('inbox.lastFolder')
  const { folders, loading: foldersLoading } = useDocumentFolderTree()
  const rememberedFolder = lastFolder !== null && folders.some((f) => f.id === lastFolder) ? lastFolder : null

  const groups = useMemo(() => groupInbox(inbox.items, passages), [inbox.items, passages])
  const duplicates = useMemo(() => findDuplicates(inbox.items), [inbox.items])
  const blurry = useMemo(() => findBlurry(inbox.items), [inbox.items])
  const groupOf = useMemo(() => new Map(groups.flatMap((g) => g.items.map((i) => [i.id, g] as const))), [groups])

  // Marks only count for items still here: a filed or deleted one has gone.
  const selected = useMemo(() => inbox.items.filter((i) => marked.has(i.id)), [inbox.items, marked])
  const toggle = (id: string) => setMarked((prev) => {
    const next = new Set(prev)
    if (!next.delete(id)) next.add(id)
    return next
  })
  const markAll = (items: readonly InboxItem[]) => setMarked((prev) => {
    const next = new Set(prev)
    const allMarked = items.every((i) => next.has(i.id))
    for (const i of items) {
      if (allMarked) next.delete(i.id)
      else next.add(i.id)
    }
    return next
  })
  const suggestions = inbox.items.filter((i) => (duplicates.get(i.id) && !duplicates.get(i.id)!.isKeeper) || blurry.has(i.id))

  const cannotAssign =
    selected.length === 0 ? 'Mark some photos first'
    : selected.some((i) => !i.mime.startsWith('image/')) ? 'Only photos can be signs'
    : selected.some((i) => groupOf.get(i.id)?.canAssign === false) ? 'Photos taken underway can be added once the passage is recorded'
    : passages.length === 0 ? 'There is no recorded passage to add them to'
    : null

  const bin = async () => {
    setBusy(true)
    try {
      const { deleted: n, busy } = await inboxApi.bin(selected.map((i) => i.id))
      if (busy.length === 0) toast.success(n === 1 ? 'Deleted 1 item' : `Deleted ${n} items`)
      else if (n === 0) toast.error(busy.length === 1 ? '1 item is being filed, so it stayed' : `${busy.length} items are being filed, so they stayed`)
      else toast.success(`${n} deleted, ${busy.length} ${busy.length === 1 ? 'is' : 'are'} being filed`)
      setMarked(new Set())
      setDialog(null)
    } catch (err) {
      toast.error('Could not delete', { description: errorMessage(err) })
    } finally {
      setBusy(false)
    }
  }

  const sendToDocuments = async (folderId: string | null) => {
    if (folderId === null) return
    setDialog(null)
    setBusy(true)
    try {
      const results = await inboxApi.fileToDocuments(folderId, selected.map((i) => ({ id: i.id })))
      setLastFolder(folderId)
      const failed = results.filter((r) => !r.ok)
      const sent = results.length - failed.length
      if (sent > 0) toast.success(sent === 1 ? 'Sent 1 item to Documents' : `Sent ${sent} items to Documents`)
      if (failed.length > 0) {
        toast.error(failed.length === 1 ? '1 item stayed in the Inbox' : `${failed.length} items stayed in the Inbox`, { description: failed[0].error })
      }
      setMarked(new Set(failed.map((f) => f.id)))
    } catch (err) {
      toast.error('Could not send to Documents', { description: errorMessage(err) })
    } finally {
      setBusy(false)
    }
  }

  const toolbar = canWrite && inbox.items.length > 0 && (
    <div className="flex flex-wrap items-center gap-2" role="toolbar" aria-label="Inbox actions">
      <span className="text-sm tabular-nums text-muted-foreground" role="status">
        {selected.length === 0 ? 'Tap pictures to mark them' : `${selected.length} marked`}
      </span>
      <Button type="button" size="sm" disabled={busy || cannotAssign !== null} title={cannotAssign ?? undefined} onClick={() => setDialog('passage')}>
        <Route className="h-4 w-4" data-icon="inline-start" aria-hidden="true" />
        Add to passage
      </Button>
      <Button type="button" size="sm" variant="outline" disabled={busy || selected.length === 0} onClick={() => setDialog('documents')}>
        <FolderInput className="h-4 w-4" data-icon="inline-start" aria-hidden="true" />
        Send to Documents
      </Button>
      <Button type="button" size="sm" variant="outline" disabled={busy || selected.length === 0} onClick={() => setDialog('bin')}>
        <Trash2 className="h-4 w-4" data-icon="inline-start" aria-hidden="true" />
        Delete
      </Button>
      <Button type="button" size="sm" variant="ghost" disabled={suggestions.length === 0} onClick={() => setMarked(new Set(suggestions.map((i) => i.id)))}>
        Mark suggested deletions ({suggestions.length})
      </Button>
      {selected.length > 0 && <Button type="button" size="sm" variant="ghost" onClick={() => setMarked(new Set())}>Clear</Button>}
    </div>
  )

  return (
    <Page
      title="Inbox"
      onBack={onBack}
      backLabel="Back to passages"
      primaryAction={canWrite ? { label: 'Share photos', icon: <ImagePlus className="h-4 w-4" data-icon="inline-start" aria-hidden="true" />, onClick: onOpenUpload, variant: 'outline' } : undefined}
    >
      {inbox.error && (
        <div role="alert" className="flex items-center gap-2 text-sm text-destructive">
          Could not read the Inbox: {inbox.error}
          <Button type="button" size="sm" variant="outline" onClick={() => void inbox.refresh()}>Retry</Button>
        </div>
      )}
      {!inbox.error && inbox.loading && inbox.items.length === 0 && <p className="text-sm text-muted-foreground">Loading…</p>}
      {!inbox.error && !inbox.loading && inbox.items.length === 0 && (
        <EmptyState
          icon={<ImagePlus className="h-8 w-8 text-muted-foreground" aria-hidden="true" />}
          title="Nothing waiting"
          description="Photos and PDFs you share from a phone wait here until you put them on a passage, send them to Documents or delete them."
          action={canWrite ? <Button type="button" variant="outline" onClick={onOpenUpload}>Share photos</Button> : undefined}
        />
      )}
      {toolbar}
      <div className="flex flex-col gap-6">
        {groups.map((group) => (
          <section key={group.key} aria-label={group.label} className="flex flex-col gap-2">
            <div className="flex flex-wrap items-center gap-2">
              <h2 className="min-w-0 flex-1 truncate text-sm font-semibold text-foreground">
                {group.label} <span className="font-normal tabular-nums text-muted-foreground">· {group.items.length}</span>
              </h2>
              {canWrite && (
                <Button type="button" size="sm" variant="ghost" onClick={() => markAll(group.items)}>
                  {group.items.every((i) => marked.has(i.id)) ? 'Unmark all' : 'Mark all'}
                </Button>
              )}
            </div>
            {!group.canAssign && (
              <p className="text-xs text-muted-foreground">
                The passage is recorded once it ends. You can delete from this group now, and add the rest to the passage afterwards.
              </p>
            )}
            <ul className="grid grid-cols-[repeat(auto-fill,minmax(7rem,1fr))] gap-2">
              {group.items.map((item) => {
                const isMarked = marked.has(item.id)
                const dup = duplicates.get(item.id)
                const soft = blurry.has(item.id)
                const when = item.capture_ms !== null ? formatTimeOrDayTime(item.capture_ms, timeZone) : null
                return (
                  <li key={item.id} className="min-w-0">
                    <button
                      type="button"
                      aria-pressed={isMarked}
                      aria-label={`${item.filename}${when ? `, ${when}` : ''}`}
                      disabled={!canWrite}
                      onClick={() => toggle(item.id)}
                      className={cn(
                        'relative block aspect-square w-full overflow-hidden rounded-md border bg-muted text-left',
                        isMarked ? 'border-primary ring-2 ring-primary' : 'border-border',
                      )}
                    >
                      {inboxPreviewUrl(item) ? (
                        <img src={inboxPreviewUrl(item)} alt="" loading="lazy" className="h-full w-full object-cover" />
                      ) : (
                        <span className="flex h-full w-full flex-col items-center justify-center gap-1 p-2 text-muted-foreground">
                          <FileText className="h-8 w-8" aria-hidden="true" />
                          <span className="line-clamp-2 w-full break-words text-center text-xs">{item.filename}</span>
                        </span>
                      )}
                      <span className="absolute inset-x-0 bottom-0 flex min-w-0 items-center gap-1 bg-background/85 px-1.5 py-0.5 text-xs tabular-nums text-foreground">
                        <span className="min-w-0 flex-1 truncate">{when ?? '--'}</span>
                        {item.bearing_deg !== null && (
                          <span title={`The phone was facing ${Math.round(item.bearing_deg)}°`} aria-label={`Facing ${Math.round(item.bearing_deg)} degrees`}>
                            <Compass className="h-3.5 w-3.5" aria-hidden="true" />
                          </span>
                        )}
                        {item.gps_mismatch && (
                          <span title={MISMATCH_TEXT} aria-label="Phone position does not match the track">
                            <AlertTriangle className="h-3.5 w-3.5 text-amber-600 dark:text-amber-400" aria-hidden="true" />
                          </span>
                        )}
                      </span>
                      {(dup || soft) && (
                        <span className="absolute inset-x-0 top-0 flex min-w-0 flex-wrap gap-1 p-1">
                          {dup && (
                            <span
                              className="inline-flex items-center gap-1 rounded bg-background/85 px-1 text-xs text-foreground"
                              title={dup.isKeeper ? `The sharpest of ${dup.size} near-identical shots` : `One of ${dup.size} near-identical shots; the sharpest is the one to keep`}
                            >
                              <Copy className="h-3 w-3" aria-hidden="true" />
                              {dup.isKeeper ? 'Best of ' + dup.size : 'Duplicate'}
                            </span>
                          )}
                          {soft && (
                            <span className="rounded bg-background/85 px-1 text-xs text-foreground" title="Much softer than the rest of this batch">Soft</span>
                          )}
                        </span>
                      )}
                    </button>
                  </li>
                )
              })}
            </ul>
          </section>
        ))}
      </div>

      {dialog === 'passage' && <InboxAssignDialog items={selected} passages={passages} onClose={() => setDialog(null)} />}
      <FolderPickerDialog
        open={dialog === 'documents' && !foldersLoading}
        onOpenChange={(open) => { if (!open) setDialog(null) }}
        title={`Send ${selected.length === 1 ? '1 item' : `${selected.length} items`} to Documents`}
        confirmLabel="Send here"
        initialFolderId={rememberedFolder}
        canPick={(folderId) => folderId !== null}
        showLoadError
        onPick={(folderId) => void sendToDocuments(folderId)}
      />
      <ConfirmDelete
        open={dialog === 'bin'}
        onOpenChange={(open) => { if (!open) setDialog(null) }}
        title={`Delete ${selected.length === 1 ? '1 item' : `${selected.length} items`}?`}
        description="They are removed from the boat for good. The originals stay on your phone."
        confirmLabel="Delete"
        deleting={busy}
        onConfirm={() => void bin()}
      />
    </Page>
  )
}

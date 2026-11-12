import { AlertTriangle, Compass, Copy, FileText, FolderInput, ImagePlus, ListChecks, Route, Trash2, Wrench } from 'lucide-react'
import { useMemo, useState } from 'react'
import { toast } from 'sonner'

import { FolderPickerDialog } from '@/components/documents/folder-picker-dialog'
import { InboxAssignDialog } from '@/components/passages/inbox-assign-dialog'
import { InboxTargetDialog } from '@/components/passages/inbox-target-dialog'
import { ConfirmDelete, EmptyState, Page } from '@/components/patterns'
import { Button } from '@/components/ui/button'
import { useDocumentFolderTree } from '@/hooks/use-document-folder-tree'
import { useInbox } from '@/hooks/use-inbox'
import { useLocalStorageId } from '@/hooks/use-local-storage-id'
import { useVesselStateSelector } from '@/hooks/use-vessel-state'
import { errorMessage } from '@/lib/errors'
import { filingOutcome, inboxApi, inboxPreviewUrl, isPhotoItem, plural, type InboxFilingResult, type InboxItem } from '@/lib/inbox-api'
import { findBlurry, findDuplicates, groupInbox, isUnsortedByMate, type InboxGroup } from '@/lib/inbox-groups'
import type { PassageSummary } from '@/lib/passages'
import { cn } from '@/lib/utils'
import { formatTimeOrDayTime, selectTimeZone } from '@/lib/vessel-time'

export interface InboxViewProps {
  canWrite: boolean
  passages: PassageSummary[]
  onOpenUpload: () => void
}

type PhotoDestinationKind = 'equipment' | 'job'

interface PhotoDestination {
  kind: PhotoDestinationKind
  /** Where the photos go, as the buttons and toasts say it. */
  noun: string
  /** localStorage key for the item used last on this device. */
  lastKey: string
  file: (targetId: string, photos: { id: string }[]) => Promise<InboxFilingResult[]>
  failure: string
}

const photoDestinations: Record<PhotoDestinationKind, PhotoDestination> = {
  equipment: {
    kind: 'equipment',
    noun: 'equipment',
    lastKey: 'inbox.lastEquipment',
    file: (id, photos) => inboxApi.fileToEquipment(id, photos),
    failure: 'Could not add to equipment',
  },
  job: {
    kind: 'job',
    noun: 'a job',
    lastKey: 'inbox.lastJob',
    file: (id, photos) => inboxApi.fileToOpenJob(id, photos),
    failure: 'Could not add to the job',
  },
}

// What a group Mate fills says it is for. The group sets where the photos go by
// default; every button still works on them.
const GROUP_HINTS: Partial<Record<InboxGroup['kind'], string>> = {
  documents: 'Mate took these for receipts and paperwork. Send them to Documents, or add any of them to a passage.',
  equipment: 'Mate took these for pictures of gear. Add them to an equipment item or a job, or to a passage.',
  unsorted: 'Mate has not sorted these, and they were not taken on a passage. Mark them and send them where they go.',
}

/** What to say beside an item Mate has not sorted: that it has not looked, or why it could not. */
function mateNote(item: InboxItem, mateSorting: boolean): string | null {
  if (item.classify_error) return `Mate could not sort this: ${item.classify_error}`
  return isUnsortedByMate(item, mateSorting) ? 'Mate has not looked at this yet' : null
}

const MISMATCH_TEXT =
  "The phone's own position was more than a nautical mile from the boat's track when this was taken. The phone's clock or time zone may be wrong, so the time may not match the track."

// The Inbox: what was shared from a phone and is waiting to be sorted. A
// contact sheet per group; tapping a picture marks it, and whatever is marked
// can be added to a passage, an equipment item or a job, sent to Documents or
// deleted. The duplicate and
// soft-focus flags are suggestions: nothing is marked or removed unless the
// operator says so.
export function InboxView({ canWrite, passages, onOpenUpload }: InboxViewProps) {
  const inbox = useInbox()
  const timeZone = useVesselStateSelector(selectTimeZone)
  const [marked, setMarked] = useState<ReadonlySet<string>>(new Set())
  const [dialog, setDialog] = useState<'passage' | 'documents' | 'equipment' | 'job' | 'bin' | null>(null)
  const [busy, setBusy] = useState(false)
  // Why an item stayed after the last filing, by item id, shown beside its
  // picture until the next action.
  const [reasons, setReasons] = useState<Readonly<Record<string, string>>>({})
  // The equipment item and job last used (a per-device convenience). The
  // picker checks the id is among those shown before highlighting it.
  const lastUsed = {
    equipment: useLocalStorageId(photoDestinations.equipment.lastKey),
    job: useLocalStorageId(photoDestinations.job.lastKey),
  }

  // The folder last sent to (a per-device convenience) opens the picker, but
  // only while it still exists: it may have been deleted since, on this or
  // another device.
  const [lastFolder, setLastFolder] = useLocalStorageId('inbox.lastFolder')
  const { folders, loading: foldersLoading } = useDocumentFolderTree()
  const rememberedFolder = lastFolder !== null && folders.some((f) => f.id === lastFolder) ? lastFolder : null

  const groups = useMemo(() => groupInbox(inbox.items, passages, inbox.mateSorting), [inbox.items, passages, inbox.mateSorting])
  const duplicates = useMemo(() => findDuplicates(inbox.items), [inbox.items])
  const blurry = useMemo(() => findBlurry(inbox.items), [inbox.items])

  // Marks only count for items still here: a filed or deleted one has gone.
  const selected = useMemo(() => inbox.items.filter((i) => marked.has(i.id)), [inbox.items, marked])
  // Equipment and jobs take photos only (a GIF or WebP is turned away
  // before anything is sent); the rest of a selection stays marked.
  const photos = useMemo(() => selected.filter(isPhotoItem), [selected])
  const others = useMemo(() => selected.filter((i) => !isPhotoItem(i)), [selected])
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
    : selected.some((i) => i.group.kind === 'underway') ? 'Photos taken underway can be added once the passage is recorded'
    : passages.length === 0 ? 'There is no recorded passage to add them to'
    : null

  const onlyPhotosOn = (d: PhotoDestination) => `Only JPEG or PNG photos can go on ${d.noun}`
  const cannotAddTo = (d: PhotoDestination): string | null =>
    selected.length === 0 ? 'Mark some photos first' : photos.length === 0 ? onlyPhotosOn(d) : null

  const bin = async () => {
    setBusy(true)
    setReasons({})
    try {
      const { deleted: n, busy } = await inboxApi.bin(selected.map((i) => i.id))
      if (busy.length === 0) toast.success(`Deleted ${plural(n, 'item')}`)
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

  // The photos in the selection go; anything else is left out of the call and
  // stays marked, so Send to Documents is one tap away.
  const addPhotos = async (dest: PhotoDestination, targetId: string, targetName: string) => {
    setDialog(null)
    setBusy(true)
    setReasons({})
    try {
      // A request that failed part-way still hands back what the earlier ones filed.
      const { results, error } = await filingOutcome(dest.file(targetId, photos.map((i) => ({ id: i.id }))))
      const failed = results.filter((r) => !r.ok)
      const added = results.length - failed.length
      const answered = new Set(results.map((r) => r.id))
      const unsent = error === null ? [] : photos.filter((p) => !answered.has(p.id))
      // Only an item that took a photo is worth remembering; a refused one is not.
      if (added > 0) {
        lastUsed[dest.kind][1](targetId)
        if (error === null) toast.success(`Added ${plural(added, 'photo')} to ${targetName}`)
      }
      if (error !== null) {
        toast.error(added > 0 ? `Added ${plural(added, 'photo')} to ${targetName}; the rest could not be sent` : dest.failure, { description: error })
        // Nothing was answered: leave the marks as they were, so the operator can try again.
        if (results.length === 0) return
      }
      if (failed.length > 0) {
        toast.error(`${plural(failed.length, 'item')} stayed in the Inbox`, { description: failed[0].error })
      }
      if (others.length > 0) {
        toast.message(`${plural(others.length, 'file')} stayed in the Inbox`, { description: `${onlyPhotosOn(dest)}. Send the rest to Documents.` })
      }
      setReasons({
        ...Object.fromEntries(others.map((o) => [o.id, onlyPhotosOn(dest)])),
        ...Object.fromEntries(unsent.map((u) => [u.id, error ?? ''])),
        ...Object.fromEntries(failed.map((f) => [f.id, f.error ?? 'It was not added'])),
      })
      setMarked(new Set([...failed.map((f) => f.id), ...unsent.map((u) => u.id), ...others.map((o) => o.id)]))
    } finally {
      setBusy(false)
    }
  }

  const sendToDocuments = async (folderId: string | null) => {
    if (folderId === null) return
    setDialog(null)
    setBusy(true)
    setReasons({})
    try {
      const { results, error } = await filingOutcome(inboxApi.fileToDocuments(folderId, selected.map((i) => ({ id: i.id }))))
      const failed = results.filter((r) => !r.ok)
      const sent = results.length - failed.length
      const answered = new Set(results.map((r) => r.id))
      const unsent = error === null ? [] : selected.filter((i) => !answered.has(i.id))
      if (sent > 0) {
        setLastFolder(folderId)
        if (error === null) toast.success(`Sent ${plural(sent, 'item')} to Documents`)
      }
      if (error !== null) {
        toast.error(sent > 0 ? `Sent ${plural(sent, 'item')} to Documents; the rest could not be sent` : 'Could not send to Documents', { description: error })
        if (results.length === 0) return
      }
      if (failed.length > 0) {
        toast.error(`${plural(failed.length, 'item')} stayed in the Inbox`, { description: failed[0].error })
      }
      setReasons({
        ...Object.fromEntries(unsent.map((u) => [u.id, error ?? ''])),
        ...Object.fromEntries(failed.map((f) => [f.id, f.error ?? 'It was not sent'])),
      })
      setMarked(new Set([...failed.map((f) => f.id), ...unsent.map((u) => u.id)]))
    } finally {
      setBusy(false)
    }
  }

  const toolbar = canWrite && inbox.items.length > 0 && (
    <div className="flex flex-wrap items-center gap-2" role="toolbar" aria-label="Inbox actions">
      <span className="text-sm tabular-nums text-muted-foreground" role="status">
        {selected.length === 0 ? 'Tap pictures to mark them' : `${selected.length} marked`}
      </span>
      <Button type="button" size="sm" disabled={busy || cannotAssign !== null} title={cannotAssign ?? undefined} onClick={() => { setReasons({}); setDialog('passage') }}>
        <Route className="h-4 w-4" data-icon="inline-start" aria-hidden="true" />
        Add to passage
      </Button>
      <Button type="button" size="sm" variant="outline" disabled={busy || selected.length === 0} onClick={() => setDialog('documents')}>
        <FolderInput className="h-4 w-4" data-icon="inline-start" aria-hidden="true" />
        Send to Documents
      </Button>
      {([[photoDestinations.equipment, Wrench, 'Add to equipment'], [photoDestinations.job, ListChecks, 'Add to a job']] as const).map(([dest, Icon, label]) => {
        const cannot = cannotAddTo(dest)
        return (
          <Button key={dest.kind} type="button" size="sm" variant="outline" disabled={busy || cannot !== null} title={cannot ?? undefined} onClick={() => setDialog(dest.kind)}>
            <Icon className="h-4 w-4" data-icon="inline-start" aria-hidden="true" />
            {label}
          </Button>
        )
      })}
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
          description="Photos and PDFs you share from a phone wait here until you put them on a passage, equipment item or job, send them to Documents or delete them."
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
            {GROUP_HINTS[group.kind] && <p className="text-xs text-muted-foreground">{GROUP_HINTS[group.kind]}</p>}
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
                    {mateNote(item, inbox.mateSorting) && (
                      <p className={cn('mt-1 line-clamp-3 break-words text-xs', item.classify_error ? 'text-destructive' : 'text-muted-foreground')}>{mateNote(item, inbox.mateSorting)}</p>
                    )}
                    {reasons[item.id] && <p className="mt-1 line-clamp-3 break-words text-xs text-destructive">{reasons[item.id]}</p>}
                  </li>
                )
              })}
            </ul>
          </section>
        ))}
      </div>

      {dialog === 'passage' && <InboxAssignDialog items={selected} passages={passages} mateTitles={inbox.mateSorting} onClose={() => setDialog(null)} />}
      <FolderPickerDialog
        open={dialog === 'documents' && !foldersLoading}
        onOpenChange={(open) => { if (!open) setDialog(null) }}
        title={`Send ${plural(selected.length, 'item')} to Documents`}
        confirmLabel="Send here"
        initialFolderId={rememberedFolder}
        canPick={(folderId) => folderId !== null}
        showLoadError
        onPick={(folderId) => void sendToDocuments(folderId)}
      />
      {(dialog === 'equipment' || dialog === 'job') && (
        <InboxTargetDialog
          key={dialog}
          kind={dialog}
          count={photos.length}
          lastId={lastUsed[dialog][0]}
          onClose={() => setDialog(null)}
          onPick={(id, name) => void addPhotos(photoDestinations[dialog], id, name)}
        />
      )}
      <ConfirmDelete
        open={dialog === 'bin'}
        onOpenChange={(open) => { if (!open) setDialog(null) }}
        title={`Delete ${plural(selected.length, 'item')}?`}
        description="They are removed from the boat for good. The originals stay on your phone."
        confirmLabel="Delete"
        deleting={busy}
        onConfirm={() => void bin()}
      />
    </Page>
  )
}

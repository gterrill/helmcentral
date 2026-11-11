import { ImagePlus, Inbox, X } from 'lucide-react'
import { useCallback, useEffect, useRef, useState, type ChangeEvent } from 'react'

import { ConfirmDelete, Page } from '@/components/patterns'
import { Button } from '@/components/ui/button'
import { useInboxQueue } from '@/hooks/use-inbox-queue'
import { useVesselStateSelector } from '@/hooks/use-vessel-state'
import { errorMessage } from '@/lib/errors'
import { inboxApi } from '@/lib/inbox-api'
import { takeLaunch, type Launch, type ShareError } from '@/lib/inbox-launch'
import { runQueue } from '@/lib/inbox-queue-runner'
import { hashFile, prepareFile, summariseTally, type UploadTally } from '@/lib/inbox-upload'
import { selectTimeZone, selectTimeZoneSource } from '@/lib/vessel-time'

export interface InboxUploadPageProps {
  canWrite: boolean
  onBack: () => void
  onOpenInbox: () => void
}

// The page that shares photos and PDFs from a phone (or a Mac) to the Inbox.
// It is its own address, /inbox/upload, so it can sit on a Home Screen and be the
// place the phone's share sheet opens (ADR 0280). Files picked here and files
// shared to the app both go on the phone's waiting list first; a file leaves the
// list only when the box has stored it or already holds it. Each photo is read,
// shrunk and scored on the device, then sent on its own, so the count is true
// and a weak connection works on one file at a time.

const SHARE_ERROR_TEXT: Record<ShareError, string> = {
  'not-kept': 'Helmcentral could not keep the files you shared on this phone, so nothing was sent. Choose them again below.',
  'no-worker': 'The files you shared did not reach the Helmcentral app on this phone, so nothing was kept. Open Helmcentral from its Home Screen icon, then share again, or choose the files below.',
}

export function InboxUploadPage({ canWrite, onBack, onOpenInbox }: InboxUploadPageProps) {
  const fileInput = useRef<HTMLInputElement>(null)
  const launch = useRef<Launch | null>(null)
  launch.current ??= takeLaunch()
  const started = useRef(false)
  const queue = useInboxQueue()
  const [tally, setTally] = useState<UploadTally | null>(null)
  const [running, setRunning] = useState(false)
  const [busyElsewhere, setBusyElsewhere] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [discarding, setDiscarding] = useState(false)
  // A boat that has no fix yet reports UTC as a placeholder, which would put a
  // photo with no zone of its own hours out; so it is treated as unknown.
  const boatZone = useVesselStateSelector(selectTimeZone)
  const zoneSource = useVesselStateSelector(selectTimeZoneSource)
  const boatTimeZone = zoneSource === 'none' ? undefined : boatZone
  const { store } = queue

  // Sends what is on the list. It does not stop when the page is left: the list
  // is the record of what is still to go, and a run that is cut off part way
  // would only have to be started again.
  const send = useCallback(async () => {
    if (!store) return
    setRunning(true)
    setError(null)
    setBusyElsewhere(false)
    try {
      const result = await runQueue(store, {
        hash: hashFile,
        known: inboxApi.known,
        prepare: (file, sourceSha256) => prepareFile(file, sourceSha256, { boatTimeZone }),
        upload: (p) => inboxApi.upload(p.blob, p.filename, p.meta, p.thumb),
        onProgress: setTally,
        onCheckpoint: () => { void inboxApi.refreshCount() },
      })
      if (result.busy) setBusyElsewhere(true)
      else setTally(result.tally)
    } catch (err) {
      setError(`Nothing was sent: ${errorMessage(err)}. The files are still on the waiting list.`)
    } finally {
      setRunning(false)
      // Once, now the run is done: the list and the badge pick up everything stored.
      void inboxApi.refresh()
    }
  }, [store, boatTimeZone])

  useEffect(() => {
    if (!store || !canWrite || !launch.current?.autoStart || started.current) return
    started.current = true
    void send()
  }, [store, canWrite, send])

  const handleFiles = async (e: ChangeEvent<HTMLInputElement>) => {
    const files = Array.from(e.target.files ?? [])
    e.target.value = ''
    if (files.length === 0 || !store) return
    setError(null)
    try {
      await store.add(files)
    } catch (err) {
      // Not sent anyway: a list that cannot be kept is the one thing the upload must not go without.
      setError(`Nothing was added to the waiting list: ${errorMessage(err)}`)
      return
    }
    await send()
  }

  const removeOne = async (id: string) => {
    try {
      await store?.remove([id])
    } catch (err) {
      setError(errorMessage(err))
    }
  }

  const discardAll = async () => {
    try {
      await store?.clear()
      setDiscarding(false)
    } catch (err) {
      setError(errorMessage(err))
    }
  }

  const waiting = queue.entries
  const shareError = launch.current.shareError

  const finished = tally !== null && !running

  return (
    <Page title="Share to Inbox" onBack={onBack} backLabel="Back to Inbox">
      <input
        ref={fileInput}
        type="file"
        multiple
        accept="image/*,application/pdf"
        className="hidden"
        data-testid="inbox-file-input"
        onChange={(e) => void handleFiles(e)}
      />
      <div className="flex max-w-xl flex-col gap-4">
        <p className="text-sm text-muted-foreground">
          Pick photos and PDFs from this device. Photos are shrunk here before they are sent, and each one is checked
          against what is already in the Inbox or has been filed from it, and the boat turns away a file Documents
          already holds, so picking the same ones twice is harmless.
        </p>
        <div className="flex flex-wrap gap-2">
          <Button type="button" size="lg" disabled={!canWrite || running || !store} onClick={() => fileInput.current?.click()}>
            <ImagePlus className="h-4 w-4" data-icon="inline-start" aria-hidden="true" />
            {running ? 'Sending…' : 'Choose photos and files'}
          </Button>
          <Button type="button" size="lg" variant="outline" onClick={onOpenInbox}>
            <Inbox className="h-4 w-4" data-icon="inline-start" aria-hidden="true" />
            Open the Inbox
          </Button>
        </div>
        {!canWrite && <p role="status" className="text-sm text-muted-foreground">Your sign-in can look but not change anything, so it cannot share to the Inbox.</p>}
        {shareError && <p role="alert" className="text-sm text-destructive">{SHARE_ERROR_TEXT[shareError]}</p>}
        {(error ?? queue.error) && <p role="alert" className="text-sm text-destructive">{error ?? queue.error}</p>}
        {busyElsewhere && (
          <p role="status" className="text-sm text-muted-foreground">
            Another window of Helmcentral on this phone is already uploading. The files you added will go with it.
          </p>
        )}

        {waiting.length > 0 && (
          <section aria-label="Waiting to upload" className="flex flex-col gap-2 rounded-md border border-border p-3">
            <p className="text-sm font-medium text-foreground tabular-nums">{waiting.length} waiting to upload</p>
            <ul className="flex flex-col gap-1">
              {waiting.map((q) => (
                <li key={q.id} className="flex min-w-0 items-start gap-2 text-sm">
                  <span className="min-w-0 flex-1 break-words">
                    {q.name}
                    {q.error && <span className="block text-xs text-destructive">Not sent: {q.error}</span>}
                  </span>
                  <Button type="button" size="icon-sm" variant="ghost" aria-label={`Remove ${q.name}`} disabled={running} onClick={() => void removeOne(q.id)}>
                    <X className="h-4 w-4" aria-hidden="true" />
                  </Button>
                </li>
              ))}
            </ul>
            <div className="flex flex-wrap gap-2">
              <Button type="button" size="sm" disabled={!canWrite || running} onClick={() => void send()}>Upload now</Button>
              <Button type="button" size="sm" variant="outline" disabled={running} onClick={() => setDiscarding(true)}>Discard all</Button>
            </div>
          </section>
        )}

        {tally && (
          <section aria-label="Upload progress" className="flex flex-col gap-2 rounded-md border border-border p-3">
            <p role="status" className="text-sm font-medium text-foreground tabular-nums">
              {running
                ? tally.stage === 'checking'
                  ? `Checking ${Math.min(tally.checked + 1, tally.total)} of ${tally.total} against what is already here`
                  : `Sending ${Math.min(tally.done + 1, tally.total)} of ${tally.total}`
                : summariseTally(tally)}
            </p>
            <div className="h-1.5 w-full overflow-hidden rounded-full bg-muted" aria-hidden="true">
              <div className="h-full bg-primary transition-[width]" style={{ width: `${tally.total === 0 ? 0 : (tally.done / tally.total) * 100}%` }} />
            </div>
            {running && <p className="text-xs text-muted-foreground tabular-nums">{summariseTally(tally)} so far</p>}
            {finished && tally.noTime > 0 && (
              <p className="text-xs text-muted-foreground">
                {tally.noTime} {tally.noTime === 1 ? 'file carried' : 'files carried'} no capture time and will sit under No time in the Inbox.
              </p>
            )}
            {tally.warnings.length > 0 && (
              <ul className="flex flex-col gap-1 text-xs text-muted-foreground">
                {tally.warnings.map((w, i) => <li key={i} className="min-w-0 break-words">{w.name}: {w.warning}</li>)}
              </ul>
            )}
            {tally.failed.length > 0 && (
              <ul className="flex flex-col gap-1 text-sm text-destructive">
                {tally.failed.map((f, i) => <li key={i} className="min-w-0 break-words">{f.name} was not sent: {f.error}</li>)}
              </ul>
            )}
          </section>
        )}
      </div>
      <ConfirmDelete
        open={discarding}
        onOpenChange={setDiscarding}
        title={`Discard ${waiting.length === 1 ? '1 file' : `${waiting.length} files`} waiting on this phone?`}
        description="They are taken off the waiting list and will not be uploaded. The originals stay in your phone's photos and files."
        confirmLabel="Discard"
        onConfirm={() => void discardAll()}
      />
    </Page>
  )
}

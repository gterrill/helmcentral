import { ImagePlus, Inbox, X } from 'lucide-react'
import { useCallback, useEffect, useRef, useState, type ChangeEvent } from 'react'

import { DiscardWaitingDialog } from '@/components/discard-waiting-dialog'
import { Page } from '@/components/patterns'
import { Button } from '@/components/ui/button'
import { useInboxQueue, useUploadRun } from '@/hooks/use-inbox-queue'
import { runningFromHomeScreen } from '@/hooks/use-screen-wake-lock'
import { useVesselStateSelector } from '@/hooks/use-vessel-state'
import { errorMessage } from '@/lib/errors'
import { inboxApi } from '@/lib/inbox-api'
import { clearLaunch, takeLaunch, type ShareError } from '@/lib/inbox-launch'
import { waitingLabel } from '@/lib/inbox-queue'
import { runPicked, runQueue, type QueueRunDeps } from '@/lib/inbox-queue-runner'
import { hashFile, prepareFile, summariseTally } from '@/lib/inbox-upload'
import { selectTimeZone, selectTimeZoneSource } from '@/lib/vessel-time'

export interface InboxUploadPageProps {
  canWrite: boolean
  onBack: () => void
  onOpenInbox: () => void
}

// The page that shares photos and PDFs from a phone (or a Mac) to the Inbox.
// It is its own address, /inbox/upload, so it can sit on a Home Screen and be the
// place the phone's share sheet opens (ADR 0280). Files picked here upload
// directly; resuming a pick means picking again, and the box skips what it
// already holds. Files shared to the app wait on the phone's list, because a
// share cannot be picked again; a file leaves the list only when the box has
// stored it or already holds it. Each photo is read, shrunk and scored on the
// device, then sent on its own, so the count is true and a weak connection
// works on one file at a time. The run itself lives in lib/inbox-queue-runner,
// not here, so a page that is left and opened again shows it going.

const SHARE_ERROR_TEXT: Record<ShareError, string> = {
  'store-failed': 'Helmcentral could not keep the files you shared on this phone, so nothing was sent. Choose them again below.',
  'no-files': 'What you shared held no files Helmcentral can take. It takes photos and PDFs. Choose some below.',
  'no-worker': 'The files you shared did not reach the Helmcentral app on this phone, so nothing was kept. Open Helmcentral from its Home Screen icon, then share again, or choose the files below.',
}

/** How long the buttons wait for the boat's time zone before going ahead without it. */
const ZONE_WAIT_MS = 10_000

export function InboxUploadPage({ canWrite, onBack, onOpenInbox }: InboxUploadPageProps) {
  const fileInput = useRef<HTMLInputElement>(null)
  // Read, not consumed, while drawing: React may throw this draw away and make
  // another. The flags are cleared once the page is mounted.
  const [launch] = useState(takeLaunch)
  useEffect(() => { clearLaunch() }, [])
  const [installedApp] = useState(runningFromHomeScreen)
  const queue = useInboxQueue()
  const { running, tally, error: runError, busyElsewhere } = useUploadRun()
  const [error, setError] = useState<string | null>(null)
  const [discarding, setDiscarding] = useState(false)
  // A boat that has no fix yet reports UTC as a placeholder, which would put a
  // photo with no zone of its own hours out; so it is treated as unknown. The
  // source is null until the boat's state has loaded: the buttons wait for it,
  // or a photo would lose its capture time. They wait a few seconds at most.
  const boatZone = useVesselStateSelector(selectTimeZone)
  const zoneSource = useVesselStateSelector(selectTimeZoneSource)
  const zoneSettled = zoneSource !== null
  const [zoneWaitedOut, setZoneWaitedOut] = useState(false)
  useEffect(() => {
    if (zoneSettled) return
    const timer = setTimeout(() => setZoneWaitedOut(true), ZONE_WAIT_MS)
    return () => clearTimeout(timer)
  }, [zoneSettled])
  const zoneReady = zoneSettled || zoneWaitedOut
  // Read as each file is prepared, so a run that began before the zone arrived
  // (or outlives this render) uses the zone as it is by then.
  const boatTimeZone = useRef<string | undefined>(undefined)
  boatTimeZone.current = zoneSource === 'none' ? undefined : (boatZone ?? undefined)
  const { store } = queue

  const deps = useCallback((): QueueRunDeps => ({
    hash: hashFile,
    known: inboxApi.known,
    prepare: (file, sourceSha256) => prepareFile(file, sourceSha256, { boatTimeZone: boatTimeZone.current }),
    upload: (p) => inboxApi.upload(p.blob, p.filename, p.meta, p.thumb),
    onCheckpoint: () => { void inboxApi.refreshCount() },
  }), [])

  // Sends what is on the list. It does not stop when the page is left: the list
  // is the record of what is still to go, and a run that is cut off part way
  // would only have to be started again.
  const send = useCallback(async () => {
    if (!store) return
    setError(null)
    try {
      await runQueue(store, deps())
    } catch {
      // Reported by the runner.
    }
    // Once, now the run is done: the list and the badge pick up everything stored.
    void inboxApi.refresh()
  }, [store, deps])

  const handleFiles = async (e: ChangeEvent<HTMLInputElement>) => {
    const files = Array.from(e.target.files ?? [])
    e.target.value = ''
    if (files.length === 0) return
    setError(null)
    try {
      await runPicked(files, deps())
    } catch {
      // Reported by the runner.
    }
    void inboxApi.refresh()
  }

  const removeOne = async (id: string) => {
    try {
      await store?.remove([id])
    } catch (err) {
      setError(errorMessage(err))
    }
  }

  const waiting = queue.entries
  const { shared, shareError } = launch

  const finished = tally !== null && !running
  // A list that cannot be read matters where files can arrive for it: in the
  // installed app, and on a page a share opened. Elsewhere a browser with no
  // IndexedDB simply uploads picked files directly.
  const listProblem = queue.error !== null && (shared || shareError !== null || installedApp)
  const sendOff = !canWrite || running || !zoneReady

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
          Pick photos and PDFs from this device. Photos are shrunk here before they are sent. Anything Helmcentral
          already holds is skipped, so picking the same ones twice is harmless.
        </p>
        <div className="flex flex-wrap gap-2">
          <Button type="button" size="lg" disabled={sendOff} onClick={() => fileInput.current?.click()}>
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
        {listProblem && <p role="alert" className="text-sm text-destructive">Could not read the files waiting on this phone: {queue.error}</p>}
        {(error ?? runError) && <p role="alert" className="text-sm text-destructive">{error ?? runError}</p>}
        {busyElsewhere && !running && (
          <p role="status" className="text-sm text-muted-foreground">
            Another window of Helmcentral on this phone is already uploading. Files shared now will wait for the next upload.
          </p>
        )}
        {!zoneReady && (
          <p role="status" className="text-sm text-muted-foreground">Getting the boat's time zone…</p>
        )}
        {shared && waiting.length > 0 && (
          <p role="status" className="text-sm text-muted-foreground">Your shared photos are below. Tap Upload to send them.</p>
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
              <Button type="button" size="lg" disabled={sendOff} onClick={() => void send()}>Upload {waitingLabel(waiting)}</Button>
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
            {finished && waiting.length > 0 && (
              <p className="text-xs text-muted-foreground tabular-nums">{waiting.length} more waiting</p>
            )}
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
      <DiscardWaitingDialog open={discarding} onOpenChange={setDiscarding} entries={waiting} store={store} onError={setError} />
    </Page>
  )
}

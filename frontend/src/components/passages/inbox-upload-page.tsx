import { ImagePlus, Inbox } from 'lucide-react'
import { useEffect, useRef, useState, type ChangeEvent } from 'react'

import { Page } from '@/components/patterns'
import { Button } from '@/components/ui/button'
import { useVesselStateSelector } from '@/hooks/use-vessel-state'
import { inboxApi } from '@/lib/inbox-api'
import { prepareFile, summariseTally, uploadFiles, type UploadTally } from '@/lib/inbox-upload'
import { selectTimeZone, selectTimeZoneSource } from '@/lib/vessel-time'

export interface InboxUploadPageProps {
  canWrite: boolean
  onBack: () => void
  onOpenInbox: () => void
}

// The page that shares photos and PDFs from a phone (or a Mac) to the Inbox.
// It is its own address, /passages/upload, so it can sit on a Home Screen.
// Each photo is read, shrunk and scored on the device, then sent on its own, so
// the count is true and a weak connection works on one file at a time.
export function InboxUploadPage({ canWrite, onBack, onOpenInbox }: InboxUploadPageProps) {
  const fileInput = useRef<HTMLInputElement>(null)
  const stop = useRef<AbortController | null>(null)
  const [tally, setTally] = useState<UploadTally | null>(null)
  const [running, setRunning] = useState(false)
  // A boat that has no fix yet reports UTC as a placeholder, which would put a
  // photo with no zone of its own hours out; so it is treated as unknown.
  const boatZone = useVesselStateSelector(selectTimeZone)
  const zoneSource = useVesselStateSelector(selectTimeZoneSource)
  const boatTimeZone = zoneSource === 'none' ? undefined : boatZone

  useEffect(() => () => stop.current?.abort(), [])

  const handleFiles = async (e: ChangeEvent<HTMLInputElement>) => {
    const files = Array.from(e.target.files ?? [])
    e.target.value = ''
    if (files.length === 0) return
    const controller = new AbortController()
    stop.current = controller
    setRunning(true)
    try {
      const final = await uploadFiles(files, {
        prepare: (file) => prepareFile(file, { boatTimeZone }),
        upload: (p) => inboxApi.upload(p.blob, p.filename, p.meta),
        onProgress: setTally,
        signal: controller.signal,
      })
      setTally(final)
    } finally {
      setRunning(false)
    }
  }

  const finished = tally !== null && !running

  return (
    <Page title="Share to Inbox" onBack={onBack} backLabel="Back to passages">
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
          against what is already in the Inbox and Documents, so picking the same ones twice is harmless.
        </p>
        <div className="flex flex-wrap gap-2">
          <Button type="button" size="lg" disabled={!canWrite || running} onClick={() => fileInput.current?.click()}>
            <ImagePlus className="h-4 w-4" data-icon="inline-start" aria-hidden="true" />
            {running ? 'Sending…' : 'Choose photos and files'}
          </Button>
          <Button type="button" size="lg" variant="outline" onClick={onOpenInbox}>
            <Inbox className="h-4 w-4" data-icon="inline-start" aria-hidden="true" />
            Open the Inbox
          </Button>
        </div>
        {!canWrite && <p role="status" className="text-sm text-muted-foreground">Your sign-in can look but not change anything, so it cannot share to the Inbox.</p>}

        {tally && (
          <section aria-label="Upload progress" className="flex flex-col gap-2 rounded-md border border-border p-3">
            <p role="status" className="text-sm font-medium text-foreground tabular-nums">
              {running ? `Sending ${Math.min(tally.done + 1, tally.total)} of ${tally.total}` : summariseTally(tally)}
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
              <ul role="alert" className="flex flex-col gap-1 text-sm text-destructive">
                {tally.failed.map((f, i) => <li key={i} className="min-w-0 break-words">{f.name} was not sent: {f.error}</li>)}
              </ul>
            )}
          </section>
        )}
      </div>
    </Page>
  )
}

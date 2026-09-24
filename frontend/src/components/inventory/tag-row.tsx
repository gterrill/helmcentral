import { useEffect, useRef, useState } from 'react'
import { Check, Copy } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { nfcSupported, writeUrlTag } from '@/lib/nfc'

// ADR 0127 §1/§4: what a tag holds (a plain NDEF URL record, this row's own
// `url`) and who can write one (Web NFC exists only in Chrome on Android -
// everywhere else this row offers Copy and says why, never a broken or
// silently-degraded "Write tag" button). Appears on the bin page
// (bin-page.tsx) and in the equipment editor for a saved item.

interface TagRowProps {
  /** The app-relative path this tag should open, e.g. `/inventory/bins/LAZ-02`. */
  path: string
}

type WriteState = 'idle' | 'writing' | 'written' | 'error'

export function TagRow({ path }: TagRowProps) {
  const url = new URL(path, window.location.origin).toString()
  const [copied, setCopied] = useState(false)
  const [writeState, setWriteState] = useState<WriteState>('idle')
  const [writeError, setWriteError] = useState<string | null>(null)
  // Web NFC's own scan/write never resolves until a tag is presented (or
  // aborted) - with no way to cancel, an operator who picked up the wrong
  // phone or changed their mind was stuck staring at "Hold the phone to the
  // tag" until they either found a tag or reloaded the page. One
  // AbortController per write, aborted by the Cancel button and, so a write
  // never outlives the row that started it, on unmount too.
  const abortRef = useRef<AbortController | null>(null)

  useEffect(() => () => { abortRef.current?.abort() }, [])

  const handleCopy = async () => {
    try {
      await navigator.clipboard.writeText(url)
      setCopied(true)
      window.setTimeout(() => setCopied(false), 2000)
    } catch {
      // A clipboard write can fail (denied permission, no clipboard API in
      // this context) - AGENTS.md fallback policy: say nothing claiming
      // success rather than a copy that silently didn't happen. The URL is
      // already on screen in full for the operator to select by hand.
    }
  }

  const handleWrite = async () => {
    const controller = new AbortController()
    abortRef.current = controller
    setWriteState('writing')
    setWriteError(null)
    try {
      await writeUrlTag(url, controller.signal)
      // A write racing an abort still resolves through this same try in a
      // browser that doesn't actually reject on an aborted signal - only
      // apply it if this is still the write Cancel or a newer write hasn't
      // already moved past.
      if (abortRef.current === controller) setWriteState('written')
    } catch (err) {
      if (abortRef.current !== controller) return
      // A deliberate Cancel click aborts the SAME signal writeUrlTag was
      // given - that's an operator choice, not a failure, so it returns
      // quietly to 'idle' rather than showing an "AbortError" the operator
      // never asked to see. handleCancel below already moved the UI back to
      // 'idle' the instant Cancel was clicked; this only matters for a
      // browser where writeUrlTag's own rejection lands after that.
      if (err instanceof Error && err.name === 'AbortError') {
        setWriteState('idle')
        return
      }
      setWriteState('error')
      // AGENTS.md fallback policy: the browser's own thrown reason (a
      // permission refusal, no tag presented), never an invented one.
      setWriteError(err instanceof Error ? err.message : String(err))
    } finally {
      if (abortRef.current === controller) abortRef.current = null
    }
  }

  // Aborts the signal (so writeUrlTag's own promise settles, in whatever way
  // this browser's Web NFC implementation actually honors an abort) AND
  // moves the UI back to 'idle' immediately - not every implementation is
  // guaranteed to reject write() the instant its signal aborts, and an
  // operator who clicked Cancel should not be left staring at "Hold the
  // phone to the tag" waiting for a promise that may never settle at all.
  const handleCancel = () => {
    abortRef.current?.abort()
    abortRef.current = null
    setWriteState('idle')
  }

  return (
    <div className="flex flex-col gap-2 rounded-md border border-border bg-card p-3">
      <div className="flex min-w-0 items-center gap-2">
        <span className="min-w-0 flex-1 truncate font-mono text-sm">{url}</span>
        <Button
          type="button"
          variant="outline"
          size="sm"
          className="shrink-0 gap-1.5"
          onClick={() => { void handleCopy() }}
        >
          {copied ? <Check className="h-3.5 w-3.5" aria-hidden="true" /> : <Copy className="h-3.5 w-3.5" aria-hidden="true" />}
          {copied ? 'Copied' : 'Copy'}
        </Button>
        {nfcSupported() && writeState === 'writing' && (
          <Button
            type="button"
            variant="outline"
            size="sm"
            className="shrink-0"
            onClick={handleCancel}
          >
            Cancel
          </Button>
        )}
        {nfcSupported() && (
          <Button
            type="button"
            variant="outline"
            size="sm"
            className="shrink-0"
            disabled={writeState === 'writing'}
            onClick={() => { void handleWrite() }}
          >
            {writeState === 'writing' ? 'Hold the phone to the tag' : writeState === 'written' ? 'Written' : 'Write tag'}
          </Button>
        )}
      </div>

      {!nfcSupported() && (
        <p className="text-[11px] text-muted-foreground">
          Writing a tag needs Chrome on Android. Copy the address into an NFC app instead.
        </p>
      )}
      {!window.isSecureContext && (
        <p className="text-[11px] text-muted-foreground">
          Write tags from the tailnet https address, so they still open on a phone once away from the boat's own network.
        </p>
      )}
      {writeState === 'error' && writeError && (
        <p role="alert" className="text-[11px] text-destructive">{writeError}</p>
      )}
    </div>
  )
}

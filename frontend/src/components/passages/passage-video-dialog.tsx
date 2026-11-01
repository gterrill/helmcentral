import { AlertTriangle, Download, Share2 } from 'lucide-react'
import { useCallback, useEffect, useRef, useState } from 'react'

import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import type { PassageVideoInput } from '@/lib/passage-video/setup'
import { VIDEO_SIZES, videoFileName, type VideoOrientation } from '@/lib/passage-video/schedule'
import { PassageVideoError } from '@/lib/passage-video/errors'
import { saveBlob } from '@/lib/save-blob'

/** What the video is made from, less the frame it is made in. */
export type PassageVideoSource = Omit<PassageVideoInput, 'orientation'>

export interface PassageVideoDialogProps {
  open: boolean
  onClose: () => void
  /** The replay as it is set now. Read when the video is started. */
  source: () => PassageVideoSource
}

type Phase =
  | { name: 'choose' }
  | { name: 'making'; done: number; total: number }
  | { name: 'done'; file: File; canShare: boolean }
  | { name: 'failed'; message: string }

const sizeMB = (bytes: number) => `${(bytes / (1024 * 1024)).toFixed(1)} MB`

/** Asks for a landscape or portrait video of the replay, makes it in this
 * browser with a progress bar and Cancel, and offers the file to download (or,
 * on a device that has a share sheet, to share). Closing the box, or leaving the
 * page, stops the work and lets go of what it was using. */
export function PassageVideoDialog({ open, onClose, source }: PassageVideoDialogProps) {
  const [orientation, setOrientation] = useState<VideoOrientation>('landscape')
  const [phase, setPhase] = useState<Phase>({ name: 'choose' })
  const abort = useRef<AbortController | null>(null)

  // Leaving the page stops it.
  useEffect(() => () => { abort.current?.abort() }, [])

  // Stops the work, if there is any, and goes back to the choice.
  const stop = useCallback(() => {
    abort.current?.abort()
    abort.current = null
    setPhase({ name: 'choose' })
  }, [])
  const close = () => { stop(); onClose() }

  const make = async () => {
    const controller = new AbortController()
    abort.current = controller
    setPhase({ name: 'making', done: 0, total: 1 })
    try {
      const input: PassageVideoInput = { ...source(), orientation }
      const { renderPassageVideo } = await import('@/lib/passage-video/render')
      const blob = await renderPassageVideo(input, {
        signal: controller.signal,
        onProgress: (p) => { if (!controller.signal.aborted) setPhase({ name: 'making', done: p.done, total: p.total }) },
      })
      if (controller.signal.aborted) return
      const file = new File([blob], videoFileName(input.summary), { type: 'video/mp4' })
      setPhase({ name: 'done', file, canShare: typeof navigator !== 'undefined' && !!navigator.canShare?.({ files: [file] }) })
    } catch (err) {
      if (controller.signal.aborted || (err instanceof PassageVideoError && err.kind === 'cancelled')) return
      setPhase({ name: 'failed', message: err instanceof Error ? err.message : String(err) })
    } finally {
      if (abort.current === controller) abort.current = null
    }
  }

  const share = async () => {
    if (phase.name !== 'done') return
    try {
      await navigator.share({ files: [phase.file], title: phase.file.name })
    } catch (err) {
      // Closing the share sheet is not a fault.
      if (!(err instanceof DOMException && err.name === 'AbortError')) setPhase({ name: 'failed', message: `Could not share the video (${err instanceof Error ? err.message : String(err)})` })
    }
  }

  // While the video is being made, only Cancel stops it: a stray click outside, Escape or the corner X does not.
  return (
    <Dialog open={open} onOpenChange={(next) => { if (!next && phase.name !== 'making') close() }}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Video</DialogTitle>
          <DialogDescription>
            {phase.name === 'done'
              ? 'The video is ready.'
              : 'Makes an MP4 of the replay as it is set now: its length, camera shots, map and 3D, and the signs with their photos.'}
          </DialogDescription>
        </DialogHeader>

        {phase.name === 'choose' && (
          <div className="flex flex-col gap-3">
            <div role="group" aria-label="Shape" className="grid grid-cols-2 gap-2">
              {(['landscape', 'portrait'] as const).map((o) => (
                <Button
                  key={o}
                  type="button"
                  variant={orientation === o ? 'default' : 'outline'}
                  aria-pressed={orientation === o}
                  className="h-auto py-2"
                  onClick={() => setOrientation(o)}
                >
                  {VIDEO_SIZES[o].label}
                </Button>
              ))}
            </div>
            <p className="text-xs text-muted-foreground">
              It's made in this browser, frame by frame, and takes a few minutes. Keep this tab open and in front until it's done.
              Nothing is stored on the boat.
            </p>
          </div>
        )}

        {phase.name === 'making' && (
          <div className="flex flex-col gap-2" role="status">
            <div
              role="progressbar"
              aria-label="Making the video"
              aria-valuemin={0}
              aria-valuemax={phase.total}
              aria-valuenow={phase.done}
              className="h-2 w-full overflow-hidden rounded-full bg-muted"
            >
              <div className="h-full bg-primary" style={{ width: `${Math.min(100, (phase.done / Math.max(1, phase.total)) * 100)}%` }} />
            </div>
            <p className="text-sm tabular-nums text-muted-foreground">Frame {phase.done} of {phase.total}</p>
          </div>
        )}

        {phase.name === 'done' && (
          <p className="text-sm text-muted-foreground tabular-nums">
            <span className="text-foreground">{phase.file.name}</span>, {sizeMB(phase.file.size)}, {VIDEO_SIZES[orientation].label}
          </p>
        )}

        {phase.name === 'failed' && (
          <div role="alert" className="flex items-start gap-2 rounded-md border border-destructive/40 p-3 text-sm text-destructive">
            <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0" aria-hidden="true" />
            <p className="min-w-0">{phase.message}</p>
          </div>
        )}

        <DialogFooter>
          {phase.name === 'choose' && (
            <>
              <Button type="button" variant="outline" onClick={close}>Close</Button>
              <Button type="button" onClick={() => void make()}>Make video</Button>
            </>
          )}
          {phase.name === 'making' && <Button type="button" variant="outline" onClick={stop}>Cancel</Button>}
          {phase.name === 'done' && (
            <>
              <Button type="button" variant="outline" onClick={close}>Close</Button>
              {phase.canShare && (
                <Button type="button" variant="outline" onClick={() => void share()}>
                  <Share2 className="h-4 w-4" data-icon="inline-start" aria-hidden="true" />
                  Share
                </Button>
              )}
              <Button type="button" onClick={() => saveBlob(phase.file, phase.file.name)}>
                <Download className="h-4 w-4" data-icon="inline-start" aria-hidden="true" />
                Download
              </Button>
            </>
          )}
          {phase.name === 'failed' && (
            <>
              <Button type="button" variant="outline" onClick={close}>Close</Button>
              <Button type="button" onClick={() => setPhase({ name: 'choose' })}>Try again</Button>
            </>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

import { useCallback, useRef, useState } from 'react'
import { toast } from 'sonner'
import { formatRadiusDisplay } from '@/lib/anchor-adjust'
import { isRetryableAnchorError } from '@/lib/anchor-request'

export interface AnchorAdjustTarget {
  /** Optional, both or neither: omitted entirely when the draft position wasn't actually moved (lib/anchor-adjust.ts's buildAdjustCommitTargets) — see useAnchorWatch's adjustAnchor for why. */
  lat?: number
  lon?: number
  radiusMeters: number
}

export interface UseAnchorAdjustCommitParams {
  /** useAnchorWatch's own adjustAnchor — one atomic PATCH, rethrows on failure. */
  adjustAnchor: (params: AnchorAdjustTarget) => Promise<void>
  /** Follows the units setting for the "Anchor moved" toast's own radius figure — defaults to metric (false) so existing callers that predate this field keep compiling and behaving unchanged. */
  isImperial?: boolean
}

export interface AnchorAdjustCommitParams {
  draft: AnchorAdjustTarget
  /** The watch's values before this Adjust session started — what Undo restores. */
  previous: AnchorAdjustTarget
  /** Called once the write has actually landed — the caller's cue to exit Adjust mode. Never called on failure: the caller stays in Adjust with the draft intact. */
  onSuccess: () => void
}

export interface UseAnchorAdjustCommitResult {
  committing: boolean
  commit: (params: AnchorAdjustCommitParams) => void
}

/**
 * Owns the Adjust mode's one write (Set) and its own undo/retry, mirroring
 * every other anchor-watch mutation's Retry-toast shape (anchor-request.ts's
 * isRetryableAnchorError) rather than inventing a new one.
 *
 * Nothing is written until `commit` is actually called — no effect in this
 * hook starts a request or a timer merely from being rendered, which is what
 * makes it StrictMode-safe: React mounting, discarding and remounting this
 * hook in development never fires a spurious PATCH or leaves an orphaned
 * timer running.
 *
 * On success: exits Adjust (via onSuccess) and raises a 10s toast offering
 * Undo, which re-sends `previous` — note for the caller (and ADR 0136): if
 * another client changes the watch during that 10s window, Undo overwrites
 * whatever they set, the same as any other last-write-wins PATCH in this
 * app.
 *
 * On failure: does not call onSuccess (the caller stays in Adjust, draft
 * intact) and shows an error toast with Retry, which re-sends the exact same
 * draft.
 */
export function useAnchorAdjustCommit({ adjustAnchor, isImperial = false }: UseAnchorAdjustCommitParams): UseAnchorAdjustCommitResult {
  const [committing, setCommitting] = useState(false)
  // A ref, not just the `committing` state: the guard below has to be true
  // the instant a second call can happen (Enter reaching this function
  // again, or a rapid double-tap of Set) — synchronously, in the same tick
  // as the first call, well before a state update has re-rendered anyone
  // with a closure that could see it. Guarding only on the button's own
  // `disabled={committing}` left Enter (the map's own scoped keydown, wired
  // straight to this same commit path) free to fire a second, overlapping
  // PATCH and a second Undo toast (code-review finding) — the guard belongs
  // on commit() itself.
  const committingRef = useRef(false)

  const commit = useCallback(({ draft, previous, onSuccess }: AnchorAdjustCommitParams) => {
    if (committingRef.current) return
    committingRef.current = true
    setCommitting(true)
    adjustAnchor(draft)
      .then(() => {
        committingRef.current = false
        setCommitting(false)
        onSuccess()

        const performUndo = () => {
          adjustAnchor(previous).catch((error: unknown) => {
            toast.error('Could not undo the anchor move', {
              description: error instanceof Error ? error.message : 'Request failed',
              action: isRetryableAnchorError(error) ? { label: 'Retry', onClick: performUndo } : undefined,
            })
          })
        }

        // "Anchor moved" only when the committed draft actually carries a
        // position (AnchorAdjustTarget's lat/lon are omitted entirely when
        // the draft position wasn't moved - lib/anchor-adjust.ts's
        // buildAdjustCommitTargets). A radius-only Set (the bar's own
        // +/-/chips, or the whole no-WebGL2 path) never touches where the
        // anchor is, so claiming it moved would be wrong (code-review
        // finding).
        const movedPosition = draft.lat !== undefined && draft.lon !== undefined
        toast(movedPosition ? 'Anchor moved' : 'Alarm radius set', {
          description: `Radius ${formatRadiusDisplay(draft.radiusMeters, isImperial)}`,
          duration: 10000,
          action: { label: 'Undo', onClick: performUndo },
        })
      })
      .catch((error: unknown) => {
        committingRef.current = false
        setCommitting(false)
        // onSuccess deliberately not called: the draft stays exactly as it
        // was, the caller's own Adjust UI stays open, and this is the only
        // feedback the operator gets that the write didn't land.
        toast.error('Could not move the anchor', {
          description: error instanceof Error ? error.message : 'Request failed',
          action: isRetryableAnchorError(error)
            ? { label: 'Retry', onClick: () => { commit({ draft, previous, onSuccess }) } }
            : undefined,
        })
      })
  }, [adjustAnchor, isImperial])

  return { committing, commit }
}

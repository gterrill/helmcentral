/**
 * ADR 0122: only one SpeechRecognition session can usefully run at a time -
 * two live sessions fight over the same microphone and can misrecognise
 * each other's audio as their own. hooks/use-mate-voice.ts already solves
 * this for its own two modes (push-to-talk taking over from the "Hey Mate"
 * wake-word listener) by composing exactly one useSpeechInput instance for
 * both. In-field dictation (components/dictation.tsx) is a second,
 * independent useSpeechInput instance living in a different component tree
 * entirely - the Mate composer or the note capture sheet, not App.tsx where
 * useMateVoice is mounted - so it has no `recognitionRef` in common with
 * wake mode to coordinate through directly.
 *
 * This module is the small thing that lets them coordinate anyway: a
 * module-level claim, not a React context. There is exactly one
 * useMateVoice instance for the lifetime of the app (App.tsx mounts it
 * once, per ADR 0094 "the listener lives in the app shell") and any number
 * of dictation instances that come and go with whatever field is mounted,
 * so a singleton is enough - a context/provider would only be buying
 * indirection for a relationship that already has exactly one listener.
 * Same module-singleton-plus-useSyncExternalStore shape as
 * lib/mate-watch-store.ts, hooks/use-vessel-identity.ts and
 * hooks/use-gauge-values.ts.
 *
 * Ref-counted rather than a plain boolean: claimVoice() returns its own
 * release function so a caller never has to track whether it, specifically,
 * is the one still holding the claim. Two overlapping claims (unlikely, but
 * not impossible if a second dictation field mounts before the first one's
 * cleanup runs) resolve correctly - the claim only actually lifts once
 * every claimant has released.
 *
 * `notify()` calls every subscriber synchronously, in the same call stack as
 * claimVoice()/the release function - not via a microtask or a React
 * re-render. That is load-bearing for use-mate-voice.ts's own subscription
 * (see the pause/resume effect there): a claimant that claims and then
 * immediately starts its own recognition, the way components/dictation.tsx's
 * start() does, needs wake mode's recognizer already stopped by the time it
 * does - a subscriber that only found out via a state update and a later
 * effect would still be racing it.
 *
 * preemptVoice() is a second, one-way signal for the opposite direction: an
 * explicit operator action (push-to-talk) that must win over whatever
 * currently holds the claim, rather than politely waiting for it to release.
 * It does not touch refCount - a claimant that reacts to it releases its own
 * claim through the ordinary release function it already holds, the same as
 * if it had stopped on its own.
 */

type ClaimListener = () => void
type PreemptListener = () => void

let refCount = 0
const listeners = new Set<ClaimListener>()
const preemptListeners = new Set<PreemptListener>()

function notify(): void {
  for (const listener of listeners) listener()
}

/** useSyncExternalStore's getSnapshot, and also called directly (e.g. from
 * inside a timer callback, where a reactive subscription would be stale) -
 * a plain boolean needs no referential-equality care the way
 * getMateWatchSnapshot's array does. */
export function getVoiceClaimSnapshot(): boolean {
  return refCount > 0
}

/**
 * Claims the shared recognizer for the caller's own SpeechRecognition
 * session. Returns a release function - call it once the session ends
 * (finish, stop, error, or unmount). Idempotent: calling the returned
 * function more than once after the first has no further effect.
 */
export function claimVoice(): () => void {
  refCount += 1
  if (refCount === 1) notify()

  let released = false
  return () => {
    if (released) return
    released = true
    refCount -= 1
    if (refCount === 0) notify()
  }
}

/** Subscribes to claim/release transitions. Returns an unsubscribe function. */
export function subscribeVoiceClaim(listener: ClaimListener): () => void {
  listeners.add(listener)
  return () => listeners.delete(listener)
}

/**
 * ADR 0122: push-to-talk (hooks/use-mate-voice.ts) is an explicit,
 * deliberate operator action - unlike wake mode, it does not wait its turn.
 * Calling this tells every current claimant to abort and release right now,
 * before push-to-talk starts its own recognition. A no-op if nothing is
 * claiming (every subscriber's own reaction, e.g. useSpeechInput's stop(),
 * is already a no-op when it isn't running).
 */
export function preemptVoice(): void {
  for (const listener of preemptListeners) listener()
}

/** Subscribes to a preempt request. Returns an unsubscribe function. */
export function subscribeVoicePreempt(listener: PreemptListener): () => void {
  preemptListeners.add(listener)
  return () => preemptListeners.delete(listener)
}

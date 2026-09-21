import { useCallback, useEffect, useRef, useState } from 'react'

import { SPEECH_INPUT_FATAL_ERRORS, useSpeechInput, type SpeechInputUnsupportedReason } from '@/hooks/use-speech-input'
import { stripWakeWord } from '@/lib/spoken-summary'
import { getVoiceClaimSnapshot, preemptVoice, subscribeVoiceClaim } from '@/lib/voice-arbiter'

// ADR 0093 voice phase, "App-wide voice": mounted once in App.tsx (not per
// panel) so push-to-talk - and, once enabled, "Hey Mate" - work from any
// page, not just the Mate panel/sheet. Composes exactly one useSpeechInput
// instance: only one SpeechRecognition session can usefully run at a time,
// and push-to-talk taking over from a running wake-word session (then
// handing back once it's done) needs them to share the same one. ADR 0122
// extends that same rule to in-field dictation (components/dictation.tsx),
// which runs an entirely separate useSpeechInput instance in a different
// component tree - see lib/voice-arbiter.ts and the pause/resume effect
// near the bottom of this hook for how the two stay out of each other's way.
const ARM_WINDOW_MS = 8_000
const WAKE_RESTART_DELAY_MS = 500

export interface UseMateVoiceOptions {
  /** Settings → Mate → "Voice input". */
  voiceInput: boolean
  /** Settings → Mate → "Listen for Hey Mate". */
  wakeWord: boolean
  /** Settings → Mate → "Read replies aloud" - not read directly, only
   * gates whether pushToTalk() bothers priming speech output at all. */
  readAloud: boolean
  canWrite: boolean
  /** Primes speechSynthesis from push-to-talk's user gesture (a tap), so a
   * reply that arrives later from a network callback can still be read
   * aloud on iOS Safari. Owned by the caller (App.tsx), not this hook -
   * MateSheet holds the useSpeechOutput instance that actually speaks. */
  prime: () => void
  onQuestion: (text: string) => void
}

export interface UseMateVoiceResult {
  supported: boolean
  unsupportedReason: SpeechInputUnsupportedReason
  listening: boolean
  /** True from a bare "Hey Mate" (no question attached) until the next
   * final result (treated as the question) or the 8s arm window expires. */
  armed: boolean
  interim: string
  error: string | null
  pushToTalk: () => void
  cancel: () => void
}

/**
 * Push-to-talk (Phase 1) plus the "Hey Mate" wake word (Phase 2 option B),
 * both funnelled through onQuestion so App.tsx only has to know "a question
 * arrived by voice, open Mate with it" - see openMate() in App.tsx.
 */
export function useMateVoice({
  voiceInput,
  wakeWord,
  canWrite,
  prime,
  onQuestion,
}: UseMateVoiceOptions): UseMateVoiceResult {
  const [armed, setArmed] = useState(false)

  // 'idle': nothing running. 'push-to-talk': one-shot, ends on its own final
  // result. 'wake': continuous, restarted per the effects below. A ref, not
  // state - it's read inside timers and event handlers, never rendered.
  const modeRef = useRef<'idle' | 'push-to-talk' | 'wake'>('idle')
  const armTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const restartTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  // Latched true by a fatal (not-allowed/audio-capture) error, so the
  // restart-on-end and visibility effects stop trying - reset only when
  // wake mode is turned on again (an operator retrying after fixing the
  // permission).
  const wakeStoppedRef = useRef(false)
  const wasListeningRef = useRef(false)

  const onQuestionRef = useRef(onQuestion)
  useEffect(() => { onQuestionRef.current = onQuestion }, [onQuestion])

  // Never on the kiosk shell, per the plan - App.tsx already folds that into
  // the `wakeWord`/`voiceInput` it passes down (isKiosk), so this hook just
  // takes the flags as given.
  const wakeDesired = wakeWord && voiceInput && canWrite
  const wakeDesiredRef = useRef(wakeDesired)
  useEffect(() => { wakeDesiredRef.current = wakeDesired }, [wakeDesired])

  const clearArmTimer = useCallback(() => {
    if (armTimerRef.current !== null) {
      clearTimeout(armTimerRef.current)
      armTimerRef.current = null
    }
  }, [])

  const clearRestartTimer = useCallback(() => {
    if (restartTimerRef.current !== null) {
      clearTimeout(restartTimerRef.current)
      restartTimerRef.current = null
    }
  }, [])

  const armForWakeWord = useCallback(() => {
    setArmed(true)
    clearArmTimer()
    armTimerRef.current = setTimeout(() => {
      armTimerRef.current = null
      setArmed(false)
    }, ARM_WINDOW_MS)
  }, [clearArmTimer])

  const handleFinal = useCallback((text: string) => {
    if (modeRef.current === 'push-to-talk') {
      modeRef.current = 'idle'
      // The wake word is optional for push-to-talk: a leading "Hey Mate" is
      // stripped if present, but its absence doesn't discard the question -
      // the whole transcript is the question either way.
      const question = (stripWakeWord(text) || text).trim()
      if (question !== '') onQuestionRef.current(question)
      return
    }

    if (modeRef.current === 'wake') {
      if (armed) {
        clearArmTimer()
        setArmed(false)
        if (text.trim() !== '') onQuestionRef.current(text.trim())
        return
      }

      const remainder = stripWakeWord(text)
      if (remainder === null) return // not addressed to Mate - ignore
      if (remainder === '') {
        armForWakeWord()
      } else {
        onQuestionRef.current(remainder)
      }
    }
  }, [armed, armForWakeWord, clearArmTimer])

  const handleError = useCallback((message: string) => {
    if (message === SPEECH_INPUT_FATAL_ERRORS.micBlocked || message === SPEECH_INPUT_FATAL_ERRORS.noMicrophone) {
      wakeStoppedRef.current = true
      clearRestartTimer()
    }
  }, [clearRestartTimer])

  const speech = useSpeechInput({ onFinal: handleFinal, onError: handleError })
  const { start: startRecognition, stop: stopRecognition, listening } = speech

  const pushToTalk = useCallback(() => {
    if (!canWrite) return
    clearArmTimer()
    setArmed(false)
    prime()
    // ADR 0122: push-to-talk is a deliberate, explicit operator action - it
    // wins over an in-progress dictation (components/dictation.tsx) rather
    // than starting a second recognition alongside it. preemptVoice() tells
    // whatever currently holds lib/voice-arbiter.ts's claim to abort and
    // release before this starts its own; a no-op if nothing is claiming.
    preemptVoice()
    modeRef.current = 'push-to-talk'
    startRecognition({ continuous: false })
  }, [canWrite, prime, startRecognition, clearArmTimer])

  const cancel = useCallback(() => {
    clearArmTimer()
    setArmed(false)
    modeRef.current = 'idle'
    stopRecognition()
  }, [stopRecognition, clearArmTimer])

  // Turning wake mode on or off. Runs on mount too, so a switch already on
  // from a previous session starts listening immediately.
  useEffect(() => {
    if (!wakeDesired) {
      clearRestartTimer()
      clearArmTimer()
      setArmed(false)
      if (modeRef.current === 'wake') {
        modeRef.current = 'idle'
        stopRecognition()
      }
      return
    }

    // Freshly (re)enabled: an earlier fatal error no longer applies - this
    // is the operator trying again, e.g. after granting the permission.
    wakeStoppedRef.current = false
    if (modeRef.current === 'idle' && !document.hidden && !getVoiceClaimSnapshot()) {
      modeRef.current = 'wake'
      startRecognition({ continuous: true })
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [wakeDesired])

  // Chrome stops a continuous session after a silence, Safari after around a
  // minute - restart 500ms later while wake mode is still wanted. Also how
  // push-to-talk hands back to wake mode once its own (non-continuous)
  // session ends on its own.
  useEffect(() => {
    const wasListening = wasListeningRef.current
    wasListeningRef.current = listening
    if (!(wasListening && !listening)) return

    modeRef.current = 'idle'
    clearRestartTimer()
    if (!wakeDesiredRef.current || wakeStoppedRef.current) return

    restartTimerRef.current = setTimeout(() => {
      restartTimerRef.current = null
      if (!wakeDesiredRef.current || wakeStoppedRef.current) return
      if (document.hidden || getVoiceClaimSnapshot()) return
      modeRef.current = 'wake'
      startRecognition({ continuous: true })
    }, WAKE_RESTART_DELAY_MS)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [listening])

  // Pausing in the background matters here specifically: an engine room or
  // a pocket is exactly where a continuous mic would otherwise burn battery
  // and stream audio to a cloud recognizer for nothing.
  useEffect(() => {
    const handleVisibility = () => {
      if (!wakeDesiredRef.current) return
      if (document.hidden) {
        if (modeRef.current === 'wake') {
          modeRef.current = 'idle'
          stopRecognition()
        }
      } else if (modeRef.current === 'idle' && !wakeStoppedRef.current && !getVoiceClaimSnapshot()) {
        modeRef.current = 'wake'
        startRecognition({ continuous: true })
      }
    }
    document.addEventListener('visibilitychange', handleVisibility)
    return () => document.removeEventListener('visibilitychange', handleVisibility)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  // ADR 0122: in-field dictation claiming lib/voice-arbiter.ts pauses wake
  // mode exactly the way the visibility effect above pauses it for a
  // backgrounded tab, and resumes it the same way once the claim lifts -
  // this hook's own push-to-talk already takes over from wake mode by a
  // different path (starting a fresh recognition on the same
  // useSpeechInput instance aborts the running one), so this only has to
  // cover a claim originating *outside* this hook.
  //
  // Code review: this used to be a `useSyncExternalStore` read
  // (`voiceClaimedElsewhere`) driving a plain `useEffect` keyed on it -
  // which only runs on the render *after* the claim changed. A claimant
  // that claims and then immediately starts its own recognizer in the same
  // synchronous call (components/dictation.tsx's start()) needs this
  // stopped *before* that happens, not on a later render. Subscribing
  // directly instead - lib/voice-arbiter.ts's notify() calls every
  // subscriber synchronously, in the same call stack as claimVoice() itself
  // - closes that gap: this callback runs, and stops the recognizer, before
  // claimVoice()'s caller gets control back.
  useEffect(() => subscribeVoiceClaim(() => {
    if (!wakeDesiredRef.current) return
    if (getVoiceClaimSnapshot()) {
      if (modeRef.current === 'wake') {
        modeRef.current = 'idle'
        stopRecognition()
      }
    } else if (modeRef.current === 'idle' && !wakeStoppedRef.current && !document.hidden) {
      modeRef.current = 'wake'
      startRecognition({ continuous: true })
    }
  }), [stopRecognition, startRecognition])

  return {
    supported: speech.supported,
    unsupportedReason: speech.unsupportedReason,
    listening: speech.listening,
    armed,
    interim: speech.interim,
    error: speech.error,
    pushToTalk,
    cancel,
  }
}

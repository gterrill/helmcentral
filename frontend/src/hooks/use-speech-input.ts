import { useCallback, useEffect, useRef, useState } from 'react'

// ADR 0093 voice phase: minimal ambient types for the Web Speech API's
// recognition side. TypeScript's own DOM lib ships SpeechRecognitionResult /
// SpeechRecognitionResultList / SpeechRecognitionAlternative (reused below
// as-is) but not SpeechRecognition itself, SpeechRecognitionEvent, or
// webkitSpeechRecognition - it's still vendor-prefixed on Safari and
// non-standard enough that the type isn't in lib.dom.d.ts. Declared here
// under "Like" names so nothing collides with a future lib.dom addition.
interface SpeechRecognitionEventLike extends Event {
  readonly resultIndex: number
  readonly results: SpeechRecognitionResultList
}

interface SpeechRecognitionErrorEventLike extends Event {
  readonly error: string
}

interface SpeechRecognitionLike extends EventTarget {
  lang: string
  continuous: boolean
  interimResults: boolean
  start(): void
  stop(): void
  abort(): void
  onresult: ((event: SpeechRecognitionEventLike) => void) | null
  onerror: ((event: SpeechRecognitionErrorEventLike) => void) | null
  onend: (() => void) | null
}

type SpeechRecognitionCtor = new () => SpeechRecognitionLike

declare global {
  interface Window {
    SpeechRecognition?: SpeechRecognitionCtor
    webkitSpeechRecognition?: SpeechRecognitionCtor
  }
}

export type SpeechInputUnsupportedReason = 'no-api' | 'insecure-context' | null

// Real recognizers report these codes on `.error`; anything else falls
// through to the raw code itself (still a string, just not one this app has
// a friendlier sentence for yet). The two fatal ones are exported: this
// hook's `onError` carries only the mapped sentence (not the raw code), and
// hooks/use-mate-voice.ts needs to recognise exactly these two - a blocked
// microphone or none at all - to stop retrying its "Hey Mate" restart loop
// rather than spinning forever against a permission the operator hasn't
// granted.
export const SPEECH_INPUT_FATAL_ERRORS = {
  micBlocked: 'Microphone blocked. Allow it for this site in the browser.',
  noMicrophone: 'No microphone found.',
} as const

const ERROR_MESSAGES: Record<string, string> = {
  'not-allowed': SPEECH_INPUT_FATAL_ERRORS.micBlocked,
  'no-speech': 'No speech heard.',
  network: 'The speech service could not be reached.',
  'audio-capture': SPEECH_INPUT_FATAL_ERRORS.noMicrophone,
}

function recognitionCtor(): SpeechRecognitionCtor | null {
  return window.SpeechRecognition ?? window.webkitSpeechRecognition ?? null
}

// Strict `=== false`, not a falsy check: jsdom (and some real embedders)
// leave `isSecureContext` as `undefined` rather than reporting it, and that
// is not the same claim as "this origin is insecure." Only an explicit
// `false` should disable voice input.
function isInsecureContext(): boolean {
  return window.isSecureContext === false
}

function unsupportedReasonFor(ctor: SpeechRecognitionCtor | null): SpeechInputUnsupportedReason {
  if (!ctor) return 'no-api'
  if (isInsecureContext()) return 'insecure-context'
  return null
}

export interface UseSpeechInputOptions {
  onFinal: (text: string) => void
  onError?: (message: string) => void
  /** BCP 47 language tag for recognition. Defaults to en-AU (ADR 0093). */
  lang?: string
}

export interface UseSpeechInputResult {
  supported: boolean
  unsupportedReason: SpeechInputUnsupportedReason
  listening: boolean
  interim: string
  error: string | null
  start: (options?: { continuous?: boolean }) => void
  stop: () => void
  finish: () => void
}

/**
 * Wraps the browser's SpeechRecognition (webkitSpeechRecognition on Safari
 * and Chrome) for both push-to-talk (Phase 1, non-continuous) and the "Hey
 * Mate" wake-word listener (Phase 2, continuous) - see hooks/use-mate-voice.ts,
 * which composes exactly one instance of this hook for both. Also backs
 * in-field dictation (components/dictation.tsx, ADR 0122), which composes
 * its own separate instance.
 *
 * Fails fast rather than silently: an unsupported browser or an insecure
 * origin never pretends to listen - `supported`/`unsupportedReason` say why,
 * and `start()` on an unsupported hook sets `error` to a sentence naming the
 * cause instead of doing nothing.
 *
 * Two ways to end a session, and they are not interchangeable. `stop()`
 * aborts and throws away whatever phrase is in progress - right for
 * Escape/cancel, where the operator wants out, not a partial answer.
 * `finish()` calls the recognizer's own stop(), which still lets a pending
 * final result arrive before `listening` clears on the `end` event - right
 * for an operator tapping the mic to say "I'm done talking," where the last
 * few words matter.
 */
export function useSpeechInput({ onFinal, onError, lang = 'en-AU' }: UseSpeechInputOptions): UseSpeechInputResult {
  const [listening, setListening] = useState(false)
  const [interim, setInterim] = useState('')
  const [error, setError] = useState<string | null>(null)

  const recognitionRef = useRef<SpeechRecognitionLike | null>(null)
  const onFinalRef = useRef(onFinal)
  const onErrorRef = useRef(onError)
  useEffect(() => { onFinalRef.current = onFinal }, [onFinal])
  useEffect(() => { onErrorRef.current = onError }, [onError])

  const ctor = recognitionCtor()
  const unsupportedReason = unsupportedReasonFor(ctor)
  const supported = unsupportedReason === null

  const start = useCallback((options?: { continuous?: boolean }) => {
    const Ctor = recognitionCtor()
    const reason = unsupportedReasonFor(Ctor)
    if (reason !== null || !Ctor) {
      setError(
        reason === 'no-api'
          ? 'This browser has no speech recognition.'
          : 'Voice input needs the app opened over https',
      )
      return
    }

    // A previous session (push-to-talk taking over from wake mode, or a
    // fresh tap before the last one's `end` event landed) is discarded
    // rather than reused - recognizers throw on a second start() against the
    // same live instance, so every start() gets its own.
    recognitionRef.current?.abort()

    const recognition = new Ctor()
    recognitionRef.current = recognition
    recognition.lang = lang
    recognition.interimResults = true
    recognition.continuous = options?.continuous ?? false

    recognition.onresult = (event) => {
      if (recognitionRef.current !== recognition) return
      for (let i = event.resultIndex; i < event.results.length; i++) {
        const result = event.results[i]
        const transcript = result[0]?.transcript ?? ''
        if (result.isFinal) {
          setInterim('')
          onFinalRef.current(transcript.trim())
        } else {
          setInterim(transcript)
        }
      }
    }

    recognition.onerror = (event) => {
      if (recognitionRef.current !== recognition) return
      const message = ERROR_MESSAGES[event.error] ?? event.error
      setError(message)
      onErrorRef.current?.(message)
    }

    recognition.onend = () => {
      if (recognitionRef.current !== recognition) return
      setListening(false)
      recognitionRef.current = null
    }

    setError(null)
    setInterim('')
    setListening(true)
    recognition.start()
  }, [lang])

  // Abort rather than the recognizer's own stop(): a caller-initiated stop
  // (Escape while listening, wake mode pausing, or the wake-word switch
  // turning off) means "discard this session," not "let one last final
  // result through." Handlers are detached and the ref cleared before
  // aborting, and `listening` is cleared optimistically here rather than
  // waiting on the browser's own (sometimes considerably delayed) `end`
  // event - a caller that just asked to stop needs to be able to trust
  // `listening` immediately, e.g. to decide whether it's safe to start a
  // new session right away.
  const stop = useCallback(() => {
    const recognition = recognitionRef.current
    if (!recognition) return
    recognitionRef.current = null
    recognition.onresult = null
    recognition.onerror = null
    recognition.onend = null
    recognition.abort()
    setListening(false)
    setInterim('')
  }, [])

  // finish() ends the session the way an operator tapping the mic to stop
  // dictating expects: the recognizer's own stop(), not abort(). A real
  // recognizer still delivers whatever phrase was already being recognised
  // as one last final result (through the same onresult handler above)
  // before firing its `end` event - stop()'s abort() above throws that
  // phrase away instead, which is right for Escape/cancel but would lose
  // the last words of a dictation the operator just finished speaking.
  // Handlers are left attached and `listening` is left alone here - both
  // clear themselves the ordinary way, from the recognizer's own `end`
  // event (see onend above), exactly as if it had stopped on its own.
  const finish = useCallback(() => {
    recognitionRef.current?.stop()
  }, [])

  useEffect(() => () => { recognitionRef.current?.abort() }, [])

  return { supported, unsupportedReason, listening, interim, error, start, stop, finish }
}

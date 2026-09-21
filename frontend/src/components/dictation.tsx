import { Mic, MicOff, Square } from 'lucide-react'
import { useCallback, useEffect, useRef, type KeyboardEvent } from 'react'

import { InputGroupButton } from '@/components/ui/input-group'
import { useSpeechInput, type SpeechInputUnsupportedReason } from '@/hooks/use-speech-input'
import { claimVoice, subscribeVoicePreempt } from '@/lib/voice-arbiter'

// ADR 0122: dictation, as opposed to the header's push-to-talk mic
// (App.tsx, "Talk to Mate" - the only voice control that sends by itself).
// A mic inside a text field appends whatever it hears to that field and
// never sends anything; the operator still presses Send/Capture themselves.
// This is the one implementation both the Mate composer
// (components/assistant-thread.tsx) and the note capture sheet
// (components/documents/note-capture-sheet.tsx) share, so the two fields
// don't drift into two different mic behaviours for no reason.

export interface UseDictationOptions {
  /** Same shape as a React state setter's functional-update form - append
   * semantics (a single space between what was already there and the new
   * phrase, skip a final that's empty after trimming) live here so both
   * call sites don't each reimplement the three lines note-capture-sheet.tsx
   * used to carry directly. Pass the field's own setState function. */
  setValue: (updater: (prev: string) => string) => void
  /** BCP 47 language tag, forwarded to useSpeechInput. Defaults to en-AU. */
  lang?: string
}

export interface UseDictationResult {
  supported: boolean
  unsupportedReason: SpeechInputUnsupportedReason
  listening: boolean
  interim: string
  error: string | null
  /** Tap-to-start. */
  start: () => void
  /** Tap-to-stop: ends via the recognizer's own stop(), so whatever phrase
   * was already being recognised still lands (use-speech-input.ts's
   * finish() - the same distinction as Stop vs Escape below). */
  finish: () => void
  /** Escape-to-cancel: discards the in-flight phrase (use-speech-input.ts's
   * stop()). Exposed separately from finish() for a caller that wants a
   * dedicated cancel affordance; handleFieldKeyDown below is what actually
   * wires Escape to it for the field itself. */
  cancel: () => void
  /** Wire this into the field's own onKeyDown (compose with whatever else
   * it already does for Enter/Ctrl+Enter, order doesn't matter). Escape
   * while listening cancels dictation and stops the keydown there -
   * stopPropagation is load-bearing: both call sites sit inside a Sheet or
   * a component that itself treats Escape as "close/cancel", and a mid
   * dictation Escape must mean "stop dictating," not "close the sheet out
   * from under me." */
  handleFieldKeyDown: (event: KeyboardEvent<HTMLTextAreaElement>) => void
}

export function useDictation({ setValue, lang }: UseDictationOptions): UseDictationResult {
  const handleFinal = useCallback((text: string) => {
    const trimmed = text.trim()
    if (trimmed === '') return
    setValue((prev) => (prev.trim() === '' ? trimmed : `${prev} ${trimmed}`))
  }, [setValue])

  const speech = useSpeechInput({ onFinal: handleFinal, lang })
  const { listening } = speech

  // ADR 0122: claim the shared recognizer (lib/voice-arbiter.ts) for
  // exactly as long as this field is listening, so App.tsx's "Hey Mate"
  // wake-word session (hooks/use-mate-voice.ts) pauses itself rather than
  // fighting this one over the same microphone, and resumes once this
  // releases.
  //
  // Code review: the claim used to be taken here reactively, in an effect
  // keyed off `listening` transitioning to true - but that effect only runs
  // on the render *after* start() (below) already constructed and started
  // the underlying recognizer, so wake mode's own session was still running
  // when this one began. The claim has to happen inside start() itself,
  // synchronously and before speech.start() is called, so a subscriber
  // (use-mate-voice.ts) reacting to lib/voice-arbiter.ts's synchronous
  // notify() has already stopped its own recognizer by the time this one's
  // is created. Only the *release* stays reactive here, off `listening`
  // going false - that covers every way a session actually ends (finish()'s
  // pending stop, cancel()'s immediate one, or the recognizer's own
  // end/error), all of which report through `listening` regardless of which
  // one it was.
  const releaseRef = useRef<(() => void) | null>(null)
  useEffect(() => {
    if (!listening) {
      releaseRef.current?.()
      releaseRef.current = null
    }
  }, [listening])

  // Defensive: a field can unmount mid-dictation (the operator navigates
  // away, or a sheet closes) without `listening` ever transitioning back to
  // false through the effect above.
  useEffect(() => () => {
    releaseRef.current?.()
    releaseRef.current = null
  }, [])

  const start = useCallback(() => {
    // Guards against claiming with nothing to release it: an unsupported
    // hook's start() sets an error and never actually starts listening, so
    // `listening` would never transition and the effect above would never
    // fire to release this. Unreachable through DictateButton today (it
    // renders nothing with no API, and disables itself on an insecure
    // origin) but start() is exported, not private to that button.
    if (!speech.supported) return
    // Idempotent: a second start() while already claiming (e.g. a stray
    // double-tap before the first click's effects have settled) must not
    // claim twice - claimVoice() is ref-counted, and a second, unmatched
    // claim here would need a second release to ever lift it.
    if (releaseRef.current === null) releaseRef.current = claimVoice()
    speech.start({ continuous: true })
  }, [speech])
  const finish = useCallback(() => { speech.finish() }, [speech])
  const cancel = useCallback(() => { speech.stop() }, [speech])

  // ADR 0122: push-to-talk (hooks/use-mate-voice.ts) is an explicit,
  // deliberate operator action and wins over an in-progress dictation - it
  // preempts this claim (lib/voice-arbiter.ts's preemptVoice()) rather than
  // waiting for it to finish. speech.stop() is already a no-op when nothing
  // is running, so every mounted dictation field can react the same way
  // without checking whether it's the one actually holding the claim.
  useEffect(() => subscribeVoicePreempt(() => { speech.stop() }), [speech])

  const handleFieldKeyDown = useCallback((event: KeyboardEvent<HTMLTextAreaElement>) => {
    if (event.key === 'Escape' && speech.listening) {
      event.preventDefault()
      event.stopPropagation()
      cancel()
    }
  }, [speech.listening, cancel])

  return {
    supported: speech.supported,
    unsupportedReason: speech.unsupportedReason,
    listening: speech.listening,
    interim: speech.interim,
    error: speech.error,
    start,
    finish,
    cancel,
    handleFieldKeyDown,
  }
}

export interface DictateButtonProps {
  dictation: UseDictationResult
  disabled?: boolean
  className?: string
}

/**
 * Sized and styled to sit beside a composer's Paperclip/Send
 * (`InputGroupButton`, `size="icon-sm"` - the same 40x40 floor DESIGN.md
 * requires everywhere else). Idle is a plain ghost mic; listening fills
 * primary the same way Send does, so "this is recording" reads at a glance
 * in direct sun rather than depending on a `title` tooltip that never shows
 * on touch. No speech API at all: renders nothing, matching the header
 * mic's own no-api rule (App.tsx). An insecure origin still renders the
 * button, disabled, with `MicOff` - see DictationError below for the
 * visible reason line, since a disabled button with no visible explanation
 * looks broken rather than gated.
 */
export function DictateButton({ dictation, disabled, className }: DictateButtonProps) {
  if (dictation.unsupportedReason === 'no-api') return null

  const insecure = dictation.unsupportedReason === 'insecure-context'
  const { listening } = dictation

  return (
    <InputGroupButton
      type="button"
      variant={listening ? 'default' : 'ghost'}
      size="icon-sm"
      aria-label={listening ? 'Stop dictation' : 'Dictate'}
      aria-pressed={listening}
      disabled={disabled || insecure}
      className={className}
      onClick={() => (listening ? dictation.finish() : dictation.start())}
    >
      {insecure
        ? <MicOff className="h-4 w-4" aria-hidden="true" />
        : listening
          ? <Square className="h-4 w-4" aria-hidden="true" />
          : <Mic className="h-4 w-4" aria-hidden="true" />}
    </InputGroupButton>
  )
}

/**
 * A visible, `aria-live="polite"` line for the field's block-end addon:
 * "Listening…" until the first interim words arrive, then the interim
 * transcript itself. `text-sm text-foreground`, not micro-text - this is
 * content the operator has to actually read back in sunlight, not a label.
 * Renders nothing while idle.
 */
export function DictationStatus({ dictation }: { dictation: UseDictationResult }) {
  if (!dictation.listening) return null

  return (
    <span aria-live="polite" className="min-w-0 flex-1 truncate text-sm text-foreground">
      {dictation.interim !== ''
        ? dictation.interim
        : <span className="text-muted-foreground">Listening…</span>}
    </span>
  )
}

/**
 * The error line for under the field - same shape the composer already
 * uses for upload errors (`role="alert"`, `text-[11px] text-destructive`).
 * An insecure-context origin isn't really an "error" so much as a standing
 * fact about how the app was opened, so it renders muted rather than red,
 * but in the same spot: a `title` tooltip never shows on a touchscreen, so
 * this is the only place the reason is readable at all.
 */
export function DictationError({ dictation }: { dictation: UseDictationResult }) {
  if (dictation.unsupportedReason === 'insecure-context') {
    return <p role="alert" className="text-[11px] text-muted-foreground">Voice input needs the app opened over https</p>
  }
  if (dictation.error) {
    return <p role="alert" className="text-[11px] text-destructive">{dictation.error}</p>
  }
  return null
}

import { useCallback, useEffect, useState } from 'react'

// ADR 0093 voice phase: unlike SpeechRecognition, SpeechSynthesis is
// standard enough that TypeScript's own DOM lib already declares
// `window.speechSynthesis` and `SpeechSynthesisUtterance` - no ambient
// types needed here, only the feature-detection and voice-picking policy.

const PREFERRED_LANGS = ['en-AU', 'en-GB']

function pickVoice(voices: SpeechSynthesisVoice[]): SpeechSynthesisVoice | null {
  for (const prefix of PREFERRED_LANGS) {
    const match = voices.find((voice) => voice.lang.startsWith(prefix))
    if (match) return match
  }
  return voices.find((voice) => voice.default) ?? voices[0] ?? null
}

export interface UseSpeechOutputResult {
  supported: boolean
  speaking: boolean
  /** Cancels anything already queued, then speaks `text`. */
  speak: (text: string) => void
  /** Cancels whatever is queued or playing. */
  stop: () => void
  /**
   * Speaks an empty utterance. Call this from a user gesture (the
   * push-to-talk tap) - iOS Safari only allows speechSynthesis to start
   * from a gesture, and the real reply arrives later from a network
   * callback, which isn't one. Priming here makes the later real speak()
   * work.
   */
  prime: () => void
}

/**
 * Wraps window.speechSynthesis to read Mate's spoken summary aloud (ADR
 * 0093 voice phase). Prefers an en-AU voice, then en-GB, then whatever the
 * platform defaults to - matching the Vessel's own en-AU recognition
 * language (use-speech-input.ts) so a question asked and its answer spoken
 * back both sound local rather than one of them defaulting to US English.
 */
export function useSpeechOutput(): UseSpeechOutputResult {
  const [speaking, setSpeaking] = useState(false)

  const supported = typeof window !== 'undefined'
    && typeof window.speechSynthesis !== 'undefined'
    && typeof window.SpeechSynthesisUtterance !== 'undefined'

  const speak = useCallback((text: string) => {
    if (!supported) return
    const synth = window.speechSynthesis
    synth.cancel()

    const utterance = new SpeechSynthesisUtterance(text)
    utterance.rate = 1
    const voice = pickVoice(synth.getVoices())
    if (voice) utterance.voice = voice
    utterance.onstart = () => setSpeaking(true)
    utterance.onend = () => setSpeaking(false)
    utterance.onerror = () => setSpeaking(false)
    synth.speak(utterance)
  }, [supported])

  const stop = useCallback(() => {
    if (!supported) return
    window.speechSynthesis.cancel()
    setSpeaking(false)
  }, [supported])

  const prime = useCallback(() => {
    if (!supported) return
    window.speechSynthesis.speak(new SpeechSynthesisUtterance(''))
  }, [supported])

  // Live lookup at cleanup time, not a captured `supported` snapshot from
  // mount: this only ever runs once, on unmount, and by then the feature
  // check that mattered is simply "is there anything to cancel."
  useEffect(() => {
    return () => { window.speechSynthesis?.cancel() }
  }, [])

  return { supported, speaking, speak, stop, prime }
}

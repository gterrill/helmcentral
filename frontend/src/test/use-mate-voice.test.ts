import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { act, renderHook } from '@testing-library/react'

import { useMateVoice } from '@/hooks/use-mate-voice'
import { claimVoice, getVoiceClaimSnapshot, subscribeVoicePreempt } from '@/lib/voice-arbiter'

// ADR 0093 voice phase (Commit 2: push-to-talk) and its wake-word follow-on
// (Commit 3). FakeSpeechRecognition mirrors use-speech-input.test.ts's fake -
// useMateVoice composes exactly one useSpeechInput instance for both modes.

interface FakeResultAlternative { transcript: string }
interface FakeResult extends Array<FakeResultAlternative> { isFinal: boolean }

class FakeSpeechRecognition {
  lang = ''
  continuous = false
  interimResults = false
  onresult: ((event: { resultIndex: number; results: ArrayLike<FakeResult> }) => void) | null = null
  onerror: ((event: { error: string }) => void) | null = null
  onend: (() => void) | null = null
  started = false
  aborted = false

  constructor() {
    instances.push(this)
  }

  start() { this.started = true }
  stop() {}
  abort() {
    this.aborted = true
    // A real recognizer eventually fires `end` after abort(); tests that
    // care about the restart loop drive this explicitly via emitEnd()
    // instead, so this stub deliberately does *not* auto-fire it - it would
    // otherwise race the very state transitions those tests are checking.
  }

  emitResult(transcript: string, isFinal: boolean) {
    const result: FakeResult = Object.assign([{ transcript }], { isFinal })
    this.onresult?.({ resultIndex: 0, results: [result] })
  }

  emitError(error: string) {
    this.onerror?.({ error })
  }

  emitEnd() {
    this.onend?.()
  }
}

let instances: FakeSpeechRecognition[] = []

function current(): FakeSpeechRecognition {
  return instances[instances.length - 1]
}

beforeEach(() => {
  instances = []
  vi.stubGlobal('SpeechRecognition', FakeSpeechRecognition)
  vi.useFakeTimers()
})

afterEach(() => {
  vi.runOnlyPendingTimers()
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

describe('useMateVoice push-to-talk', () => {
  it('primes speech output and starts a non-continuous recognition', () => {
    const prime = vi.fn()
    const onQuestion = vi.fn()
    const { result } = renderHook(() => useMateVoice({
      voiceInput: true, wakeWord: false, readAloud: false, canWrite: true, prime, onQuestion,
    }))

    act(() => result.current.pushToTalk())

    expect(prime).toHaveBeenCalled()
    expect(current().continuous).toBe(false)
    expect(current().started).toBe(true)
    expect(result.current.listening).toBe(true)
  })

  it('delivers a final transcript with no wake word straight to onQuestion', () => {
    const onQuestion = vi.fn()
    const { result } = renderHook(() => useMateVoice({
      voiceInput: true, wakeWord: false, readAloud: false, canWrite: true, prime: vi.fn(), onQuestion,
    }))

    act(() => result.current.pushToTalk())
    act(() => current().emitResult('how does the passage look', true))

    expect(onQuestion).toHaveBeenCalledWith('how does the passage look')
  })

  it('strips a leading wake word from a push-to-talk transcript before delivering it', () => {
    const onQuestion = vi.fn()
    const { result } = renderHook(() => useMateVoice({
      voiceInput: true, wakeWord: false, readAloud: false, canWrite: true, prime: vi.fn(), onQuestion,
    }))

    act(() => result.current.pushToTalk())
    act(() => current().emitResult('Hey Mate, how does the passage look', true))

    expect(onQuestion).toHaveBeenCalledWith('how does the passage look')
  })

  it('shows interim text while listening and clears it on the final result', () => {
    const { result } = renderHook(() => useMateVoice({
      voiceInput: true, wakeWord: false, readAloud: false, canWrite: true, prime: vi.fn(), onQuestion: vi.fn(),
    }))

    act(() => result.current.pushToTalk())
    act(() => current().emitResult('how does the', false))
    expect(result.current.interim).toBe('how does the')

    act(() => current().emitResult('how does the passage look', true))
    expect(result.current.interim).toBe('')
  })

  it('is unsupported and sets error when there is no recognition API', () => {
    vi.unstubAllGlobals()
    const { result } = renderHook(() => useMateVoice({
      voiceInput: true, wakeWord: false, readAloud: false, canWrite: true, prime: vi.fn(), onQuestion: vi.fn(),
    }))

    expect(result.current.supported).toBe(false)

    act(() => result.current.pushToTalk())

    expect(result.current.error).toBe('This browser has no speech recognition.')
  })

  it('maps a recognition error to a plain sentence', () => {
    const { result } = renderHook(() => useMateVoice({
      voiceInput: true, wakeWord: false, readAloud: false, canWrite: true, prime: vi.fn(), onQuestion: vi.fn(),
    }))

    act(() => result.current.pushToTalk())
    act(() => current().emitError('not-allowed'))

    expect(result.current.error).toBe('Microphone blocked. Allow it for this site in the browser.')
  })

  it('cancel aborts the recognition and clears listening', () => {
    const { result } = renderHook(() => useMateVoice({
      voiceInput: true, wakeWord: false, readAloud: false, canWrite: true, prime: vi.fn(), onQuestion: vi.fn(),
    }))

    act(() => result.current.pushToTalk())
    act(() => result.current.cancel())

    expect(current().aborted).toBe(true)
  })

  // Code review: pushToTalk used to start a second recognition without any
  // regard for an in-field dictation session already holding
  // lib/voice-arbiter.ts's claim (components/dictation.tsx) - two live
  // SpeechRecognition sessions fighting over one microphone. Push-to-talk is
  // a deliberate, explicit operator action, so it wins: it preempts whatever
  // currently holds the claim (telling it to abort and release) before
  // starting its own. Simulates the claim the same way the wake-mode-pause
  // test below does - by calling claimVoice() directly and reacting to the
  // preempt exactly as components/dictation.tsx does - rather than mounting
  // a real dictation field.
  it('preempts an active dictation claim before starting its own recognition', () => {
    const { result } = renderHook(() => useMateVoice({
      voiceInput: true, wakeWord: false, readAloud: false, canWrite: true, prime: vi.fn(), onQuestion: vi.fn(),
    }))
    const release = claimVoice()
    const unsubscribe = subscribeVoicePreempt(() => { release() })
    expect(getVoiceClaimSnapshot()).toBe(true)

    act(() => result.current.pushToTalk())

    expect(getVoiceClaimSnapshot()).toBe(false)
    expect(current().started).toBe(true)
    unsubscribe()
  })
})

describe('useMateVoice wake word', () => {
  function renderWake(overrides: Partial<Parameters<typeof useMateVoice>[0]> = {}) {
    return renderHook(() => useMateVoice({
      voiceInput: true, wakeWord: true, readAloud: false, canWrite: true, prime: vi.fn(), onQuestion: vi.fn(),
      ...overrides,
    }))
  }

  it('starts a continuous recognition as soon as wake mode is desired', () => {
    renderWake()

    expect(instances).toHaveLength(1)
    expect(current().continuous).toBe(true)
    expect(current().started).toBe(true)
  })

  it('delivers "hey mate what is the tide" as "what is the tide"', () => {
    const onQuestion = vi.fn()
    renderWake({ onQuestion })

    act(() => current().emitResult("hey mate what's the tide", true))

    expect(onQuestion).toHaveBeenCalledWith("what's the tide")
  })

  it('arms on a bare wake word and delivers the next final as the question', () => {
    const onQuestion = vi.fn()
    const { result } = renderWake({ onQuestion })

    act(() => current().emitResult('hey mate', true))
    expect(result.current.armed).toBe(true)
    expect(onQuestion).not.toHaveBeenCalled()

    act(() => current().emitResult("what's the tide doing", true))

    expect(onQuestion).toHaveBeenCalledWith("what's the tide doing")
    expect(result.current.armed).toBe(false)
  })

  it('expires the arm window after 8 seconds', () => {
    const onQuestion = vi.fn()
    const { result } = renderWake({ onQuestion })

    act(() => current().emitResult('hey mate', true))
    expect(result.current.armed).toBe(true)

    act(() => { vi.advanceTimersByTime(8000) })
    expect(result.current.armed).toBe(false)

    act(() => current().emitResult("what's the tide doing", true))
    expect(onQuestion).not.toHaveBeenCalled()
  })

  it('ignores a final result with no wake word at all', () => {
    const onQuestion = vi.fn()
    renderWake({ onQuestion })

    act(() => current().emitResult('the wind has picked up', true))

    expect(onQuestion).not.toHaveBeenCalled()
  })

  it('restarts recognition 500ms after the browser stops it on its own', () => {
    renderWake()
    expect(instances).toHaveLength(1)

    act(() => current().emitEnd())
    expect(instances).toHaveLength(1)

    act(() => { vi.advanceTimersByTime(500) })

    expect(instances).toHaveLength(2)
    expect(current().continuous).toBe(true)
  })

  it('does not restart after a not-allowed error, and surfaces it', () => {
    const { result } = renderWake()

    act(() => current().emitError('not-allowed'))
    act(() => current().emitEnd())
    act(() => { vi.advanceTimersByTime(5000) })

    expect(instances).toHaveLength(1)
    expect(result.current.error).toBe('Microphone blocked. Allow it for this site in the browser.')
  })

  it('does not restart after an audio-capture error either', () => {
    renderWake()

    act(() => current().emitError('audio-capture'))
    act(() => current().emitEnd())
    act(() => { vi.advanceTimersByTime(5000) })

    expect(instances).toHaveLength(1)
  })

  it('pauses while the document is hidden and resumes when it becomes visible', () => {
    renderWake()
    expect(instances).toHaveLength(1)

    Object.defineProperty(document, 'hidden', { value: true, configurable: true })
    act(() => { document.dispatchEvent(new Event('visibilitychange')) })
    expect(current().aborted).toBe(true)

    Object.defineProperty(document, 'hidden', { value: false, configurable: true })
    act(() => { document.dispatchEvent(new Event('visibilitychange')) })

    expect(instances).toHaveLength(2)
    expect(current().continuous).toBe(true)

    Object.defineProperty(document, 'hidden', { value: false, configurable: true })
  })

  it('stops recognition and stays stopped once wakeWord is turned off', () => {
    const { result, rerender } = renderHook(
      (props: { wakeWord: boolean }) => useMateVoice({
        voiceInput: true, wakeWord: props.wakeWord, readAloud: false, canWrite: true, prime: vi.fn(), onQuestion: vi.fn(),
      }),
      { initialProps: { wakeWord: true } },
    )
    expect(instances).toHaveLength(1)

    rerender({ wakeWord: false })

    expect(current().aborted).toBe(true)
    expect(result.current.listening).toBe(false)

    act(() => { vi.advanceTimersByTime(5000) })
    expect(instances).toHaveLength(1)
  })

  it('push-to-talk takes over from wake mode, which resumes once it is done', () => {
    const onQuestion = vi.fn()
    const { result } = renderWake({ onQuestion })
    expect(instances).toHaveLength(1)
    const wakeInstance = current()

    act(() => result.current.pushToTalk())

    expect(wakeInstance.aborted).toBe(true)
    expect(instances).toHaveLength(2)
    expect(current().continuous).toBe(false)

    act(() => current().emitResult('what about tomorrow', true))
    expect(onQuestion).toHaveBeenCalledWith('what about tomorrow')

    act(() => current().emitEnd())
    act(() => { vi.advanceTimersByTime(500) })

    expect(instances).toHaveLength(3)
    expect(current().continuous).toBe(true)
  })

  // ADR 0122: in-field dictation (components/dictation.tsx) runs its own,
  // entirely separate SpeechRecognition instance and claims
  // lib/voice-arbiter.ts for as long as it's listening - this hook has no
  // direct handle on that session the way it does push-to-talk's, so the
  // coordination goes through the shared arbiter instead.
  it('pauses wake mode while the voice arbiter is claimed elsewhere, and resumes once released', () => {
    renderWake()
    expect(instances).toHaveLength(1)
    const wakeInstance = current()

    let release: () => void = () => {}
    act(() => { release = claimVoice() })

    expect(wakeInstance.aborted).toBe(true)
    expect(instances).toHaveLength(1) // nothing new started while claimed

    act(() => { release() })

    expect(instances).toHaveLength(2)
    expect(current().continuous).toBe(true)
  })

  it('does not start wake mode while the voice arbiter is already claimed when it becomes desired', () => {
    const release = claimVoice()

    renderWake()
    expect(instances).toHaveLength(0)

    act(() => { release() })

    expect(instances).toHaveLength(1)
    expect(current().continuous).toBe(true)
  })

  // Code review: this hook used to learn about a claim only through
  // useSyncExternalStore's reactive value, driving a useEffect keyed on that
  // value - which only runs on a later render. A claimant like
  // components/dictation.tsx's start() claims and then immediately starts
  // its own recognizer in the very same synchronous call, so "stop wake mode
  // on the next render" is too late - the two sessions briefly overlap.
  // Deliberately not wrapped in act(): that's exactly what would paper over
  // this bug, by flushing the pending effect before the assertion runs the
  // same way a real synchronous claimant never does. The fix has this hook
  // stop its own recognizer directly from lib/voice-arbiter.ts's
  // subscription callback, which runs synchronously inside claimVoice()'s
  // own call stack.
  it('stops wake mode synchronously the instant the arbiter is claimed, with no render in between', () => {
    renderWake()
    const wakeInstance = current()

    const release = claimVoice()

    expect(wakeInstance.aborted).toBe(true)
    act(() => { release() })
  })
})

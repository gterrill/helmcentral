import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { act, renderHook, waitFor } from '@testing-library/react'

import { useSpeechInput } from '@/hooks/use-speech-input'

// ADR 0093 voice phase. FakeSpeechRecognition stands in for the browser's
// SpeechRecognition/webkitSpeechRecognition (jsdom has neither), giving the
// tests a hand-crank for onresult/onerror/onend - the same shape the
// telemetry-stream FakeEventSource tests use for EventSource.

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
  stopped = false

  constructor() {
    instances.push(this)
  }

  start() {
    this.started = true
  }

  stop() {
    this.stopped = true
  }

  abort() {
    this.aborted = true
  }

  // --- test helpers, not part of the real SpeechRecognition API ---

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

function stubSecureContext(value: boolean | undefined) {
  Object.defineProperty(window, 'isSecureContext', { value, configurable: true })
}

beforeEach(() => {
  instances = []
})

afterEach(() => {
  vi.unstubAllGlobals()
  Object.defineProperty(window, 'isSecureContext', { value: undefined, configurable: true })
})

describe('useSpeechInput support detection', () => {
  it('is supported when the API exists', () => {
    vi.stubGlobal('SpeechRecognition', FakeSpeechRecognition)

    const { result } = renderHook(() => useSpeechInput({ onFinal: vi.fn() }))

    expect(result.current.supported).toBe(true)
    expect(result.current.unsupportedReason).toBeNull()
  })

  it('falls back to webkitSpeechRecognition', () => {
    vi.stubGlobal('webkitSpeechRecognition', FakeSpeechRecognition)

    const { result } = renderHook(() => useSpeechInput({ onFinal: vi.fn() }))

    expect(result.current.supported).toBe(true)
  })

  it('is unsupported with reason no-api when neither constructor exists', () => {
    const { result } = renderHook(() => useSpeechInput({ onFinal: vi.fn() }))

    expect(result.current.supported).toBe(false)
    expect(result.current.unsupportedReason).toBe('no-api')
  })

  it('is unsupported with reason insecure-context when the origin is not secure', () => {
    vi.stubGlobal('SpeechRecognition', FakeSpeechRecognition)
    stubSecureContext(false)

    const { result } = renderHook(() => useSpeechInput({ onFinal: vi.fn() }))

    expect(result.current.supported).toBe(false)
    expect(result.current.unsupportedReason).toBe('insecure-context')
  })

  it('start() on an unsupported hook (no api) sets a named error', () => {
    const { result } = renderHook(() => useSpeechInput({ onFinal: vi.fn() }))

    act(() => result.current.start())

    expect(result.current.error).toBe('This browser has no speech recognition.')
  })

  it('start() on an unsupported hook (insecure context) sets a named error', () => {
    vi.stubGlobal('SpeechRecognition', FakeSpeechRecognition)
    stubSecureContext(false)

    const { result } = renderHook(() => useSpeechInput({ onFinal: vi.fn() }))

    act(() => result.current.start())

    expect(result.current.error).toBe('Voice input needs the app opened over https')
  })
})

describe('useSpeechInput recognition lifecycle', () => {
  beforeEach(() => {
    vi.stubGlobal('SpeechRecognition', FakeSpeechRecognition)
  })

  it('start sets lang, interimResults and continuous on a fresh instance', () => {
    const { result } = renderHook(() => useSpeechInput({ onFinal: vi.fn(), lang: 'en-AU' }))

    act(() => result.current.start({ continuous: true }))

    expect(instances).toHaveLength(1)
    expect(instances[0].lang).toBe('en-AU')
    expect(instances[0].interimResults).toBe(true)
    expect(instances[0].continuous).toBe(true)
    expect(instances[0].started).toBe(true)
    expect(result.current.listening).toBe(true)
  })

  it('defaults continuous to false and lang to en-AU', () => {
    const { result } = renderHook(() => useSpeechInput({ onFinal: vi.fn() }))

    act(() => result.current.start())

    expect(instances[0].lang).toBe('en-AU')
    expect(instances[0].continuous).toBe(false)
  })

  it('updates interim on a non-final result', () => {
    const { result } = renderHook(() => useSpeechInput({ onFinal: vi.fn() }))
    act(() => result.current.start())

    act(() => instances[0].emitResult('how does the', false))

    expect(result.current.interim).toBe('how does the')
  })

  it('calls onFinal and clears interim on a final result', () => {
    const onFinal = vi.fn()
    const { result } = renderHook(() => useSpeechInput({ onFinal }))
    act(() => result.current.start())

    act(() => instances[0].emitResult('how does the passage look', false))
    expect(result.current.interim).toBe('how does the passage look')

    act(() => instances[0].emitResult('how does the passage look', true))

    expect(onFinal).toHaveBeenCalledWith('how does the passage look')
    expect(result.current.interim).toBe('')
  })

  it.each([
    ['not-allowed', 'Microphone blocked. Allow it for this site in the browser.'],
    ['no-speech', 'No speech heard.'],
    ['network', 'The speech service could not be reached.'],
    ['audio-capture', 'No microphone found.'],
    ['aborted', 'aborted'],
  ])('maps recognition error %s to a plain sentence', (code, message) => {
    const onError = vi.fn()
    const { result } = renderHook(() => useSpeechInput({ onFinal: vi.fn(), onError }))
    act(() => result.current.start())

    act(() => instances[0].emitError(code))

    expect(result.current.error).toBe(message)
    expect(onError).toHaveBeenCalledWith(message)
  })

  it('clears listening on end', () => {
    const { result } = renderHook(() => useSpeechInput({ onFinal: vi.fn() }))
    act(() => result.current.start())
    expect(result.current.listening).toBe(true)

    act(() => instances[0].emitEnd())

    expect(result.current.listening).toBe(false)
  })

  it('stop() aborts the underlying recognition', () => {
    const { result } = renderHook(() => useSpeechInput({ onFinal: vi.fn() }))
    act(() => result.current.start())

    act(() => result.current.stop())

    expect(instances[0].aborted).toBe(true)
  })

  it('stop() discards a phrase already in progress - a final result after stop() never reaches onFinal', () => {
    const onFinal = vi.fn()
    const { result } = renderHook(() => useSpeechInput({ onFinal }))
    act(() => result.current.start())
    act(() => instances[0].emitResult('how does the', false))

    act(() => result.current.stop())

    // A real recognizer can still fire a queued result after abort() - the
    // hook detaches its handlers before aborting specifically so a race like
    // that lands on nothing.
    act(() => instances[0].emitResult('how does the passage look', true))
    expect(onFinal).not.toHaveBeenCalled()
    expect(result.current.listening).toBe(false)
  })

  // finish() (used by a mic tap that ends dictation, as opposed to
  // Escape/cancel) calls the recognizer's own stop() rather than abort() -
  // real speech APIs still deliver whatever phrase was already being
  // recognised as a final result before firing `end`, so the last words
  // spoken are not lost the way stop() above discards them.
  it('finish() stops rather than aborts the underlying recognition', () => {
    const { result } = renderHook(() => useSpeechInput({ onFinal: vi.fn() }))
    act(() => result.current.start())

    act(() => result.current.finish())

    expect(instances[0].stopped).toBe(true)
    expect(instances[0].aborted).toBe(false)
    // Unlike stop(), listening does not clear immediately - only once the
    // recognizer's own `end` event lands, same as an unprompted stop.
    expect(result.current.listening).toBe(true)
  })

  it('finish() still lets a pending final result through before ending', () => {
    const onFinal = vi.fn()
    const { result } = renderHook(() => useSpeechInput({ onFinal }))
    act(() => result.current.start())
    act(() => instances[0].emitResult('how does the', false))
    expect(result.current.interim).toBe('how does the')

    act(() => result.current.finish())
    // The browser delivers the final result for the phrase that was already
    // in flight, then ends on its own - both after finish() was called.
    act(() => instances[0].emitResult('how does the passage look', true))
    expect(onFinal).toHaveBeenCalledWith('how does the passage look')
    expect(result.current.listening).toBe(true)

    act(() => instances[0].emitEnd())
    expect(result.current.listening).toBe(false)
  })

  it('finish() on an already-idle hook is a no-op', () => {
    const { result } = renderHook(() => useSpeechInput({ onFinal: vi.fn() }))

    expect(() => act(() => result.current.finish())).not.toThrow()
    expect(instances).toHaveLength(0)
  })

  it('aborts the recognition on unmount', () => {
    const { result, unmount } = renderHook(() => useSpeechInput({ onFinal: vi.fn() }))
    act(() => result.current.start())

    unmount()

    expect(instances[0].aborted).toBe(true)
  })

  it('starting again aborts the previous instance rather than reusing it', async () => {
    const { result } = renderHook(() => useSpeechInput({ onFinal: vi.fn() }))
    act(() => result.current.start({ continuous: true }))
    const first = instances[0]

    act(() => result.current.start({ continuous: false }))

    expect(first.aborted).toBe(true)
    expect(instances).toHaveLength(2)
    await waitFor(() => expect(result.current.listening).toBe(true))
  })
})

describe('useSpeechInput continuous requests', () => {
  const ANDROID_UA = 'Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 Chrome/126.0 Mobile Safari/537.36'
  const originalUA = navigator.userAgent

  function stubUA(ua: string) {
    Object.defineProperty(navigator, 'userAgent', { value: ua, configurable: true })
  }

  afterEach(() => {
    stubUA(originalUA)
    vi.useRealTimers()
  })

  function setup(onError?: (m: string) => void) {
    vi.stubGlobal('SpeechRecognition', FakeSpeechRecognition)
    const onFinal = vi.fn()
    const hook = renderHook(() => useSpeechInput({ onFinal, onError }))
    return { onFinal, hook }
  }

  describe('on Android', () => {
    beforeEach(() => stubUA(ANDROID_UA))

    it('runs non-continuous recognizers for a continuous request', () => {
      const { hook } = setup()
      act(() => hook.result.current.start({ continuous: true }))
      expect(instances).toHaveLength(1)
      expect(instances[0].continuous).toBe(false)
      expect(instances[0].started).toBe(true)
    })

    it('starts a fresh recognizer on each end, stays listening, delivers each final once', () => {
      const { onFinal, hook } = setup()
      act(() => hook.result.current.start({ continuous: true }))
      act(() => instances[0].emitResult('I can', false))
      expect(hook.result.current.interim).toBe('I can')
      act(() => instances[0].emitResult("I can't fill it", true))
      act(() => instances[0].emitEnd())
      expect(instances).toHaveLength(2)
      expect(instances[1].started).toBe(true)
      expect(instances[1].continuous).toBe(false)
      expect(hook.result.current.listening).toBe(true)
      expect(hook.result.current.interim).toBe('')
      act(() => instances[1].emitResult('how far is it', true))
      act(() => instances[1].emitEnd())
      expect(onFinal.mock.calls.map((c) => c[0])).toEqual(["I can't fill it", 'how far is it'])
    })

    it('delivers repeated phrases in full', () => {
      const { onFinal, hook } = setup()
      act(() => hook.result.current.start({ continuous: true }))
      act(() => instances[0].emitResult('yes', true))
      act(() => instances[0].emitEnd())
      act(() => instances[1].emitResult('yes', true))
      expect(onFinal.mock.calls.map((c) => c[0])).toEqual(['yes', 'yes'])
    })

    it('does not restart after stop()', () => {
      const { hook } = setup()
      act(() => hook.result.current.start({ continuous: true }))
      act(() => hook.result.current.stop())
      act(() => instances[0].emitEnd())
      expect(instances).toHaveLength(1)
      expect(hook.result.current.listening).toBe(false)
    })

    it('finish() lets the last final through and does not restart', () => {
      const { onFinal, hook } = setup()
      act(() => hook.result.current.start({ continuous: true }))
      act(() => hook.result.current.finish())
      expect(instances[0].stopped).toBe(true)
      act(() => instances[0].emitResult('last words', true))
      act(() => instances[0].emitEnd())
      expect(onFinal).toHaveBeenCalledWith('last words')
      expect(instances).toHaveLength(1)
      expect(hook.result.current.listening).toBe(false)
    })

    it.each(['not-allowed', 'service-not-allowed', 'audio-capture'])('does not restart after a %s error', (code) => {
      const onError = vi.fn()
      const { hook } = setup(onError)
      act(() => hook.result.current.start({ continuous: true }))
      act(() => instances[0].emitError(code))
      act(() => instances[0].emitEnd())
      expect(instances).toHaveLength(1)
      expect(hook.result.current.listening).toBe(false)
      expect(onError).toHaveBeenCalledTimes(1)
    })

    it('restarts after a no-speech error', () => {
      const { hook } = setup()
      act(() => hook.result.current.start({ continuous: true }))
      act(() => instances[0].emitError('no-speech'))
      act(() => instances[0].emitEnd())
      expect(instances).toHaveLength(2)
      expect(hook.result.current.listening).toBe(true)
    })

    it('stops and reports an error when sessions keep ending at once with nothing heard', () => {
      const onError = vi.fn()
      const { hook } = setup(onError)
      act(() => hook.result.current.start({ continuous: true }))
      for (let i = 0; i < 10 && hook.result.current.listening; i++) {
        act(() => instances[instances.length - 1].emitEnd())
      }
      expect(hook.result.current.listening).toBe(false)
      expect(instances.length).toBeLessThan(6)
      expect(onError).toHaveBeenCalledTimes(1)
      expect(hook.result.current.error).toBeTruthy()
    })

    it('does not trip the guard when sessions produce results', () => {
      const { hook } = setup()
      act(() => hook.result.current.start({ continuous: true }))
      for (let i = 0; i < 8; i++) {
        act(() => instances[instances.length - 1].emitResult(`word ${i}`, true))
        act(() => instances[instances.length - 1].emitEnd())
      }
      expect(hook.result.current.listening).toBe(true)
      expect(instances).toHaveLength(9)
    })

    it('leaves a non-continuous request as a single session', () => {
      const { hook } = setup()
      act(() => hook.result.current.start({ continuous: false }))
      act(() => instances[0].emitEnd())
      expect(instances).toHaveLength(1)
      expect(hook.result.current.listening).toBe(false)
    })
  })

  describe('off Android', () => {
    beforeEach(() => stubUA('Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 Safari/605.1.15'))

    it('keeps one continuous recognizer and does not restart on end', () => {
      const { hook } = setup()
      act(() => hook.result.current.start({ continuous: true }))
      expect(instances[0].continuous).toBe(true)
      act(() => instances[0].emitEnd())
      expect(instances).toHaveLength(1)
      expect(hook.result.current.listening).toBe(false)
    })

    it('delivers distinct finals and repeats in full', () => {
      const { onFinal, hook } = setup()
      act(() => hook.result.current.start({ continuous: true }))
      act(() => instances[0].emitResult('yes', true))
      act(() => instances[0].emitResult('yes', true))
      act(() => instances[0].emitResult('check the bilge', true))
      act(() => instances[0].emitResult('check the bilge pump', true))
      expect(onFinal.mock.calls.map((c) => c[0])).toEqual(['yes', 'yes', 'check the bilge', 'check the bilge pump'])
    })
  })
})

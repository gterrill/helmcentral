import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { act, renderHook } from '@testing-library/react'

import { useSpeechOutput } from '@/hooks/use-speech-output'

// ADR 0093 voice phase: reads a reply's spoken summary aloud. jsdom has no
// speechSynthesis, so these tests stand up a fake `speechSynthesis` and
// `SpeechSynthesisUtterance` (vi.stubGlobal), the same pattern
// use-speech-input.test.ts uses for SpeechRecognition.

class FakeUtterance {
  text: string
  rate = 1
  voice: { lang: string; default?: boolean } | null = null
  onstart: (() => void) | null = null
  onend: (() => void) | null = null
  onerror: (() => void) | null = null

  constructor(text?: string) {
    this.text = text ?? ''
  }
}

class FakeSpeechSynthesis {
  spoken: FakeUtterance[] = []
  cancelCallOrder: string[] = []
  private voices: Array<{ lang: string; default?: boolean }>

  constructor(voices: Array<{ lang: string; default?: boolean }> = []) {
    this.voices = voices
  }

  getVoices() {
    return this.voices
  }

  speak(utterance: FakeUtterance) {
    this.cancelCallOrder.push('speak')
    this.spoken.push(utterance)
  }

  cancel() {
    this.cancelCallOrder.push('cancel')
  }
}

let synth: FakeSpeechSynthesis

function install(voices: Array<{ lang: string; default?: boolean }> = []) {
  synth = new FakeSpeechSynthesis(voices)
  vi.stubGlobal('speechSynthesis', synth)
  vi.stubGlobal('SpeechSynthesisUtterance', FakeUtterance)
}

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('useSpeechOutput support detection', () => {
  it('is unsupported when speechSynthesis is absent', () => {
    const { result } = renderHook(() => useSpeechOutput())
    expect(result.current.supported).toBe(false)
  })

  it('is supported when speechSynthesis and SpeechSynthesisUtterance both exist', () => {
    install()
    const { result } = renderHook(() => useSpeechOutput())
    expect(result.current.supported).toBe(true)
  })
})

describe('useSpeechOutput', () => {
  beforeEach(() => {
    install([
      { lang: 'en-US', default: true },
      { lang: 'en-GB' },
      { lang: 'en-AU' },
    ])
  })

  it('prefers an en-AU voice', () => {
    const { result } = renderHook(() => useSpeechOutput())

    act(() => result.current.speak('Fine for Monday.'))

    expect(synth.spoken[0].voice?.lang).toBe('en-AU')
    expect(synth.spoken[0].rate).toBe(1)
    expect(synth.spoken[0].text).toBe('Fine for Monday.')
  })

  it('falls back to en-GB when no en-AU voice is installed', () => {
    install([{ lang: 'en-US', default: true }, { lang: 'en-GB' }])
    const { result } = renderHook(() => useSpeechOutput())

    act(() => result.current.speak('Fine for Monday.'))

    expect(synth.spoken[0].voice?.lang).toBe('en-GB')
  })

  it('falls back to the default voice when neither en-AU nor en-GB is installed', () => {
    install([{ lang: 'fr-FR' }, { lang: 'de-DE', default: true }])
    const { result } = renderHook(() => useSpeechOutput())

    act(() => result.current.speak('Fine for Monday.'))

    expect(synth.spoken[0].voice?.lang).toBe('de-DE')
  })

  it('cancels anything already queued before speaking', () => {
    const { result } = renderHook(() => useSpeechOutput())

    act(() => result.current.speak('Fine for Monday.'))

    expect(synth.cancelCallOrder).toEqual(['cancel', 'speak'])
  })

  it('tracks speaking from the utterance start and end events', () => {
    const { result } = renderHook(() => useSpeechOutput())

    act(() => result.current.speak('Fine for Monday.'))
    expect(result.current.speaking).toBe(false)

    act(() => synth.spoken[0].onstart?.())
    expect(result.current.speaking).toBe(true)

    act(() => synth.spoken[0].onend?.())
    expect(result.current.speaking).toBe(false)
  })

  it('clears speaking on an utterance error too', () => {
    const { result } = renderHook(() => useSpeechOutput())

    act(() => result.current.speak('Fine for Monday.'))
    act(() => synth.spoken[0].onstart?.())
    expect(result.current.speaking).toBe(true)

    act(() => synth.spoken[0].onerror?.())
    expect(result.current.speaking).toBe(false)
  })

  it('stop() cancels and clears speaking', () => {
    const { result } = renderHook(() => useSpeechOutput())
    act(() => result.current.speak('Fine for Monday.'))
    act(() => synth.spoken[0].onstart?.())

    act(() => result.current.stop())

    expect(result.current.speaking).toBe(false)
    expect(synth.cancelCallOrder.at(-1)).toBe('cancel')
  })

  it('prime() speaks an empty utterance', () => {
    const { result } = renderHook(() => useSpeechOutput())

    act(() => result.current.prime())

    expect(synth.spoken).toHaveLength(1)
    expect(synth.spoken[0].text).toBe('')
  })

  it('cancels on unmount', () => {
    const { unmount } = renderHook(() => useSpeechOutput())

    unmount()

    expect(synth.cancelCallOrder.at(-1)).toBe('cancel')
  })
})

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { act, render, screen, fireEvent } from '@testing-library/react'
import { useState } from 'react'

import { useDictation, DictateButton, DictationStatus, DictationError } from '@/components/dictation'
import { getVoiceClaimSnapshot, preemptVoice, subscribeVoiceClaim } from '@/lib/voice-arbiter'

// ADR 0122. Same FakeSpeechRecognition shape as use-speech-input.test.ts's,
// since useDictation composes that hook directly.

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

  start() { this.started = true }
  stop() { this.stopped = true }
  abort() { this.aborted = true }

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

function stubSecureContext(value: boolean | undefined) {
  Object.defineProperty(window, 'isSecureContext', { value, configurable: true })
}

function TestField() {
  const [value, setValue] = useState('')
  const dictation = useDictation({ setValue })
  return (
    <div>
      <textarea
        aria-label="Field"
        value={value}
        onChange={(e) => setValue(e.target.value)}
        onKeyDown={dictation.handleFieldKeyDown}
      />
      <DictateButton dictation={dictation} />
      <DictationStatus dictation={dictation} />
      <DictationError dictation={dictation} />
    </div>
  )
}

beforeEach(() => {
  instances = []
})

afterEach(() => {
  vi.unstubAllGlobals()
  Object.defineProperty(window, 'isSecureContext', { value: undefined, configurable: true })
})

describe('DictateButton visibility', () => {
  it('renders nothing when the browser has no SpeechRecognition API at all', () => {
    render(<TestField />)
    expect(screen.queryByRole('button', { name: /dictate/i })).not.toBeInTheDocument()
  })

  it('shows a disabled mic with the https reason when the API exists but the page is not a secure context', () => {
    vi.stubGlobal('SpeechRecognition', FakeSpeechRecognition)
    stubSecureContext(false)

    render(<TestField />)

    const mic = screen.getByRole('button', { name: 'Dictate' })
    expect(mic).toBeDisabled()
    expect(screen.getByRole('alert')).toHaveTextContent('Voice input needs the app opened over https')
  })

  it('shows an enabled ghost mic labelled Dictate when supported', () => {
    vi.stubGlobal('SpeechRecognition', FakeSpeechRecognition)
    stubSecureContext(true)

    render(<TestField />)

    expect(screen.getByRole('button', { name: 'Dictate' })).toBeEnabled()
  })
})

describe('dictation lifecycle', () => {
  beforeEach(() => {
    vi.stubGlobal('SpeechRecognition', FakeSpeechRecognition)
    stubSecureContext(true)
  })

  it('tapping Dictate starts listening and swaps the label/aria-pressed to Stop dictation', () => {
    render(<TestField />)

    fireEvent.click(screen.getByRole('button', { name: 'Dictate' }))

    expect(instances).toHaveLength(1)
    expect(current().started).toBe(true)
    const button = screen.getByRole('button', { name: 'Stop dictation' })
    expect(button).toHaveAttribute('aria-pressed', 'true')
  })

  it('tapping Stop dictation calls finish() (stop, not abort) so a pending final still lands', () => {
    render(<TestField />)
    fireEvent.click(screen.getByRole('button', { name: 'Dictate' }))

    fireEvent.click(screen.getByRole('button', { name: 'Stop dictation' }))

    expect(current().stopped).toBe(true)
    expect(current().aborted).toBe(false)
  })

  it('shows Listening… before any interim words arrive, then the interim transcript', () => {
    render(<TestField />)
    fireEvent.click(screen.getByRole('button', { name: 'Dictate' }))

    expect(screen.getByText('Listening…')).toBeInTheDocument()

    act(() => { current().emitResult('fuel return is the', false) })

    expect(screen.getByText('fuel return is the')).toBeInTheDocument()
    expect(screen.queryByText('Listening…')).not.toBeInTheDocument()
  })

  it('shows nothing from DictationStatus while idle', () => {
    render(<TestField />)
    expect(screen.queryByText('Listening…')).not.toBeInTheDocument()
  })

  it('appends a final phrase to an empty field with no leading space', () => {
    render(<TestField />)
    fireEvent.click(screen.getByRole('button', { name: 'Dictate' }))

    act(() => { current().emitResult('fuel return is the inboard valve', true) })

    expect(screen.getByRole('textbox', { name: 'Field' })).toHaveValue('fuel return is the inboard valve')
  })

  it('appends a second final phrase with a single space, not a replace', () => {
    render(<TestField />)
    fireEvent.click(screen.getByRole('button', { name: 'Dictate' }))

    act(() => { current().emitResult('fuel return is the inboard valve', true) })
    act(() => { current().emitResult('the one with the scratched handle', true) })

    expect(screen.getByRole('textbox', { name: 'Field' }))
      .toHaveValue('fuel return is the inboard valve the one with the scratched handle')
  })

  it('skips an empty (or whitespace-only) final rather than appending nothing useful', () => {
    render(<TestField />)
    fireEvent.click(screen.getByRole('button', { name: 'Dictate' }))

    act(() => { current().emitResult('   ', true) })

    expect(screen.getByRole('textbox', { name: 'Field' })).toHaveValue('')
  })

  it('Escape while listening cancels (aborts) rather than finishing', () => {
    render(<TestField />)
    fireEvent.click(screen.getByRole('button', { name: 'Dictate' }))

    fireEvent.keyDown(screen.getByRole('textbox', { name: 'Field' }), { key: 'Escape' })

    expect(current().aborted).toBe(true)
    expect(current().stopped).toBe(false)
  })

  it('Escape while listening does not propagate past the field (so an ancestor Escape handler is not also triggered)', () => {
    const onAncestorKeyDown = vi.fn()
    render(<div onKeyDown={onAncestorKeyDown}><TestField /></div>)
    fireEvent.click(screen.getByRole('button', { name: 'Dictate' }))

    fireEvent.keyDown(screen.getByRole('textbox', { name: 'Field' }), { key: 'Escape' })

    expect(onAncestorKeyDown).not.toHaveBeenCalled()
  })

  it('Escape does nothing while not listening (an ordinary Escape still reaches an ancestor)', () => {
    const onAncestorKeyDown = vi.fn()
    render(<div onKeyDown={onAncestorKeyDown}><TestField /></div>)

    fireEvent.keyDown(screen.getByRole('textbox', { name: 'Field' }), { key: 'Escape' })

    expect(onAncestorKeyDown).toHaveBeenCalledTimes(1)
  })

  it('shows a recognition error inline as an alert', () => {
    render(<TestField />)
    fireEvent.click(screen.getByRole('button', { name: 'Dictate' }))

    act(() => { current().emitError('not-allowed') })

    expect(screen.getByRole('alert')).toHaveTextContent('Microphone blocked. Allow it for this site in the browser.')
  })

  it('claims the shared voice arbiter while listening and releases it once finished', () => {
    render(<TestField />)
    expect(getVoiceClaimSnapshot()).toBe(false)

    fireEvent.click(screen.getByRole('button', { name: 'Dictate' }))
    expect(getVoiceClaimSnapshot()).toBe(true)

    fireEvent.click(screen.getByRole('button', { name: 'Stop dictation' }))
    act(() => { current().emitEnd() })

    expect(getVoiceClaimSnapshot()).toBe(false)
  })

  // Code review: the claim used to be taken reactively, in an effect keyed
  // off `listening` becoming true - which only runs on the render *after*
  // start() already constructed and started the underlying recognizer, so a
  // running "Hey Mate" listener briefly overlapped with dictation's own
  // session before it got the message to stop. The claim (and any
  // subscriber's synchronous reaction to it - see use-mate-voice.test.ts)
  // has to happen before the recognizer itself is created.
  it('claims the shared voice arbiter before constructing the recognizer, not after', () => {
    render(<TestField />)
    let instanceCountAtClaim = -1
    const unsubscribe = subscribeVoiceClaim(() => { instanceCountAtClaim = instances.length })

    fireEvent.click(screen.getByRole('button', { name: 'Dictate' }))

    expect(instanceCountAtClaim).toBe(0)
    unsubscribe()
  })

  // ADR 0122: push-to-talk is an explicit operator action and wins over an
  // in-progress dictation (hooks/use-mate-voice.ts's pushToTalk calls
  // preemptVoice() before starting its own recognition) - dictation has to
  // actually give up the microphone in response, not just the claim.
  it('stops and releases the claim when preempted by push-to-talk', () => {
    render(<TestField />)
    fireEvent.click(screen.getByRole('button', { name: 'Dictate' }))
    expect(getVoiceClaimSnapshot()).toBe(true)

    act(() => { preemptVoice() })

    expect(current().aborted).toBe(true)
    expect(getVoiceClaimSnapshot()).toBe(false)
  })
})

// ADR 0124: the note editor's mic uses `insert` instead of `setValue` -
// see this file's own UseDictationOptions union and lib/note-editor-
// dictation.ts's insertDictatedText for what the editor itself does with
// each call. This field only has to prove the CONTRACT useDictation gives
// that sink: it receives every final, and only a final, already trimmed
// and never empty - not whether a real Slate editor places the text
// correctly (that's insertDictatedText's own headless test suite, note-
// editor-dictation.test.ts).
function TestFieldWithInsert() {
  const [received, setReceived] = useState<string[]>([])
  const dictation = useDictation({ insert: (text) => setReceived((prev) => [...prev, text]) })
  return (
    <div>
      <ul aria-label="Received">
        {received.map((text, i) => <li key={i}>{text}</li>)}
      </ul>
      <DictateButton dictation={dictation} />
      <DictationStatus dictation={dictation} />
    </div>
  )
}

describe('useDictation insert sink (ADR 0124)', () => {
  beforeEach(() => {
    vi.stubGlobal('SpeechRecognition', FakeSpeechRecognition)
    stubSecureContext(true)
  })

  it('receives a final result, already trimmed', () => {
    render(<TestFieldWithInsert />)
    fireEvent.click(screen.getByRole('button', { name: 'Dictate' }))

    act(() => { current().emitResult('  fuel return is the inboard valve  ', true) })

    expect(screen.getByRole('list', { name: 'Received' })).toHaveTextContent('fuel return is the inboard valve')
  })

  it('never receives an interim result', () => {
    render(<TestFieldWithInsert />)
    fireEvent.click(screen.getByRole('button', { name: 'Dictate' }))

    act(() => { current().emitResult('fuel return is the', false) })

    expect(screen.queryByRole('list', { name: 'Received' })?.textContent).toBe('')
  })

  it('skips an empty (or whitespace-only) final rather than calling insert with nothing useful', () => {
    render(<TestFieldWithInsert />)
    fireEvent.click(screen.getByRole('button', { name: 'Dictate' }))

    act(() => { current().emitResult('   ', true) })

    expect(screen.queryByRole('list', { name: 'Received' })?.textContent).toBe('')
  })

  it('receives each final separately, in order, rather than being joined into one string', () => {
    render(<TestFieldWithInsert />)
    fireEvent.click(screen.getByRole('button', { name: 'Dictate' }))

    act(() => { current().emitResult('fuel return is the inboard valve', true) })
    act(() => { current().emitResult('the one with the scratched handle', true) })

    const items = screen.getAllByRole('listitem')
    expect(items.map((item) => item.textContent)).toEqual([
      'fuel return is the inboard valve',
      'the one with the scratched handle',
    ])
  })
})

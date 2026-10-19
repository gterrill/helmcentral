import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { StrictMode, useState } from 'react'
import { act, fireEvent, render, renderHook, screen, waitFor } from '@testing-library/react'
import { useSetupStatus } from '@/hooks/use-setup-status'

import { SignalKConnectionSection } from '@/components/settings/sections/signalk-connection-section'
import { SettingsFormProvider } from '@/components/settings/settings-form-context'
import { SecretsStatusProvider } from '@/components/settings/secrets-status-context'
import { initialRegularSettingsDraft, type RegularSettingsDraft } from '@/components/settings/settings-draft'

import { AppRouterContext } from '@/hooks/use-app-location'
import type { AppRouter } from '@/lib/app-router'
import { SetupLine } from '@/components/setup-line'
import { SignalKSetupSection } from '@/components/settings/sections/signalk-setup-section'
import { resetQueryStore } from '@/lib/api-query'
import { clearPendingSetupFocus, peekPendingSetupFocus, requestSetupFocus, type SetupStatus } from '@/lib/setup-status'

const step = (id: string, title: string, state: 'done' | 'todo' | 'blocked', detail: string, control: string) => ({
  id, title, state, detail, target: { section: 'signalk', control },
})

const partial: SetupStatus = {
  complete: false,
  steps: [
    step('connection', 'Connect to SignalK', 'done', 'Helmcentral is receiving vessel data.', 'signalk-address'),
    step('engines', 'Engines', 'done', 'Assigned.', 'discovery-engine'),
    step('house_bank', 'House bank', 'todo', 'Choose which of the 2 batteries the boat reports is the house bank.', 'role-house_bank'),
    step('power', 'Charging and power sources', 'todo', 'Still open: Shore charger. Choose one, or This boat has none.', 'role-shore_charger'),
  ],
}

let body: SetupStatus | { error: string }
let ok: boolean
const fetchMock = vi.fn()

beforeEach(() => {
  resetQueryStore()
  body = partial
  ok = true
  fetchMock.mockReset()
  fetchMock.mockImplementation(() => Promise.resolve({ ok, status: ok ? 200 : 500, json: async () => body }))
  vi.stubGlobal('fetch', fetchMock)
  clearPendingSetupFocus()
})

describe('Settings -> SignalK -> Setup', () => {
  it('shows done steps as one muted line and only the next to do with its detail and a button', async () => {
    render(<SignalKSetupSection />)

    expect(await screen.findByText('Choose which of the 2 batteries the boat reports is the house bank.')).toBeInTheDocument()
    // Done steps: title only, no detail.
    expect(screen.getByText('Connect to SignalK')).toBeInTheDocument()
    expect(screen.queryByText('Helmcentral is receiving vessel data.')).toBeNull()
    // The step after the next one stays a single line, without its detail.
    expect(screen.getByText('Charging and power sources')).toBeInTheDocument()
    expect(screen.queryByText(/Still open: Shore charger/)).toBeNull()
    expect(screen.getByRole('button', { name: 'Choose the house bank' })).toBeInTheDocument()
    expect(screen.getAllByRole('button')).toHaveLength(1)
  })

  it('scrolls to and focuses the control the next step lands on', async () => {
    render(
      <>
        <select id="role-house_bank" aria-label="House bank" />
        <SignalKSetupSection />
      </>,
    )
    fireEvent.click(await screen.findByRole('button', { name: 'Choose the house bank' }))
    expect(document.activeElement).toBe(screen.getByLabelText('House bank'))
  })

  it('shows a blocked step as one muted line that says what comes first', async () => {
    body = {
      complete: false,
      steps: [
        step('connection', 'Connect to SignalK', 'todo', 'Helmcentral has no vessel data from SignalK.', 'signalk-address'),
        step('engines', 'Engines', 'blocked', 'Connect to SignalK first.', 'discovery-engine'),
      ],
    }
    render(<SignalKSetupSection />)

    expect(await screen.findByText('Engines · Connect to SignalK first.')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Check the connection' })).toBeInTheDocument()
  })

  it('says so when the status cannot be read', async () => {
    ok = false
    body = { error: 'boom' }
    render(<SignalKSetupSection />)
    expect(await screen.findByRole('alert')).toHaveTextContent('Could not read the setup status')
  })
})

const optionalStep = (state: 'done' | 'todo') => ({
  id: 'passage_limits', title: 'Passage limits', state, detail: 'Still open: ahead, astern.', optional: true,
  target: { section: 'assistant', control: 'passage-limits' },
})

describe('Optional setup steps', () => {
  it('shows an open optional step with its detail and a Set limits button, still labelled Optional', async () => {
    body = { complete: true, steps: [...partial.steps.map((s) => ({ ...s, state: 'done' as const })), optionalStep('todo')] }
    render(<SignalKSetupSection />)
    expect(await screen.findByText('Passage limits')).toBeInTheDocument()
    expect(screen.getByText('Optional')).toBeInTheDocument()
    expect(screen.getByText('Still open: ahead, astern.')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Set limits' })).toBeInTheDocument()
  })

  it('the button opens the Mate settings section and asks for #passage-limits to be focused', async () => {
    body = { complete: true, steps: [...partial.steps.map((s) => ({ ...s, state: 'done' as const })), optionalStep('todo')] }
    const navigate = vi.fn().mockReturnValue(true)
    render(
      <AppRouterContext.Provider value={{ navigate } as unknown as AppRouter}>
        <SignalKSetupSection />
      </AppRouterContext.Provider>,
    )
    fireEvent.click(await screen.findByRole('button', { name: 'Set limits' }))
    expect(navigate).toHaveBeenCalledWith({ panel: 'settings', route: { view: 'section', section: 'assistant' } })
    expect(peekPendingSetupFocus()).toBe('passage-limits')
  })

  it('does not make the optional step the "next" one while a required step is open', async () => {
    body = { complete: false, steps: [...partial.steps, optionalStep('todo')] }
    render(<SignalKSetupSection />)
    expect(await screen.findByRole('button', { name: 'Choose the house bank' })).toBeInTheDocument()
    expect(document.querySelectorAll('[aria-current="step"]')).toHaveLength(1)
  })

  it('keeps optional steps out of the dashboard counter and the "next" step', async () => {
    body = { complete: false, steps: [...partial.steps, optionalStep('todo')] }
    render(<SetupLine enabled onOpen={() => {}} />)
    expect(await screen.findByRole('button', { name: 'Setup: 2 of 4 done · Next: House bank' })).toBeInTheDocument()
  })

  it('does not count a done optional step as progress', async () => {
    body = { complete: false, steps: [...partial.steps, optionalStep('done')] }
    render(<SetupLine enabled onOpen={() => {}} />)
    expect(await screen.findByRole('button', { name: 'Setup: 2 of 4 done · Next: House bank' })).toBeInTheDocument()
  })

  it('draws no setup line when only an optional step is open', async () => {
    body = { complete: true, steps: [...partial.steps.map((s) => ({ ...s, state: 'done' as const })), optionalStep('todo')] }
    const { container } = render(<SetupLine enabled onOpen={() => {}} />)
    await waitFor(() => expect(fetchMock).toHaveBeenCalled())
    await new Promise((r) => setTimeout(r, 20))
    expect(container).toBeEmptyDOMElement()
  })
})

describe('Dashboard setup line', () => {
  it('reads "Setup: 2 of 4 done · Next: House bank" and opens the step', async () => {
    const onOpen = vi.fn()
    render(<SetupLine enabled onOpen={onOpen} />)

    const line = await screen.findByRole('button', { name: 'Setup: 2 of 4 done · Next: House bank' })
    fireEvent.click(line)
    expect(onOpen).toHaveBeenCalledWith(expect.objectContaining({ id: 'house_bank', target: { section: 'signalk', control: 'role-house_bank' } }))
  })

  it('is hidden, and fetches nothing, on a wall display or read-only session', async () => {
    const { container } = render(<SetupLine enabled={false} onOpen={() => {}} />)
    await new Promise((r) => setTimeout(r, 20))
    expect(container).toBeEmptyDOMElement()
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('is hidden once setup is complete, and returns when a step regresses', async () => {
    body = { complete: true, steps: partial.steps.map((s) => ({ ...s, state: 'done' as const })) }
    const { container, rerender } = render(<SetupLine enabled onOpen={() => {}} />)
    await waitFor(() => expect(fetchMock).toHaveBeenCalled())
    await waitFor(() => expect(container).toBeEmptyDOMElement())

    body = partial
    resetQueryStore()
    rerender(<SetupLine enabled={false} onOpen={() => {}} />)
    rerender(<SetupLine enabled onOpen={() => {}} />)
    expect(await screen.findByText('Setup: 2 of 4 done · Next: House bank')).toBeInTheDocument()
  })

  it('lands on the requested control after opening Settings, even under StrictMode\'s double mount', async () => {
    // The dashboard line asks for focus and then navigates. React StrictMode
    // runs the section's mount effect twice; the request must survive the
    // first run or the page stays at the top.
    function Harness() {
      const [draft, setDraft] = useState<RegularSettingsDraft>({ ...initialRegularSettingsDraft })
      return (
        <SettingsFormProvider>
          <SecretsStatusProvider>
            <SignalKConnectionSection draft={draft} onChange={(patch) => setDraft((p) => ({ ...p, ...patch }))} />
          </SecretsStatusProvider>
        </SettingsFormProvider>
      )
    }
    requestSetupFocus('signalk-address')
    render(<StrictMode><Harness /></StrictMode>)

    await waitFor(() => expect(document.activeElement).toBe(screen.getByLabelText('SignalK address')))
    expect(peekPendingSetupFocus()).toBeNull()
  })

  it('ignores a focus request older than five seconds, and clears it', () => {
    vi.useFakeTimers()
    try {
      requestSetupFocus('role-house_bank')
      vi.advanceTimersByTime(6000)
      expect(peekPendingSetupFocus()).toBeNull()
      requestSetupFocus('role-house_bank')
      vi.advanceTimersByTime(4000)
      expect(peekPendingSetupFocus()).toBe('role-house_bank')
    } finally {
      vi.useRealTimers()
    }
  })

  it('acts on a request made while the SignalK section is already mounted', async () => {
    function Harness() {
      const [draft, setDraft] = useState<RegularSettingsDraft>({ ...initialRegularSettingsDraft })
      return (
        <SettingsFormProvider>
          <SecretsStatusProvider>
            <SignalKConnectionSection draft={draft} onChange={(patch) => setDraft((p) => ({ ...p, ...patch }))} />
          </SecretsStatusProvider>
        </SettingsFormProvider>
      )
    }
    render(<Harness />)
    await screen.findByLabelText('SignalK address')
    expect(document.activeElement).not.toBe(screen.getByLabelText('SignalK port'))

    act(() => requestSetupFocus('signalk-port'))

    await waitFor(() => expect(document.activeElement).toBe(screen.getByLabelText('SignalK port')))
    expect(peekPendingSetupFocus()).toBeNull()
  })

  it('keeps a requested focus for Settings to pick up', () => {
    requestSetupFocus('role-house_bank')
    expect(peekPendingSetupFocus()).toBe('role-house_bank')
    expect(peekPendingSetupFocus()).toBe('role-house_bank')
    clearPendingSetupFocus()
    expect(peekPendingSetupFocus()).toBeNull()
  })
})


describe('useSetupStatus polling', () => {
  afterEach(() => vi.useRealTimers())

  const poll = async (complete: boolean) => {
    resetQueryStore()
    body = complete ? { complete: true, steps: partial.steps.map((s) => ({ ...s, state: 'done' as const })) } : partial
    vi.useFakeTimers({ shouldAdvanceTime: true })
    renderHook(() => useSetupStatus())
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1))
    // Let the first answer land so the hook can see whether setup is complete.
    await act(async () => { await vi.advanceTimersByTimeAsync(100) })
    fetchMock.mockClear()
    await act(async () => { await vi.advanceTimersByTimeAsync(180_000) })
    return fetchMock.mock.calls.length
  }

  it('polls every minute while setup is unfinished', async () => {
    const calls = await poll(false)
    expect(calls).toBeGreaterThanOrEqual(2)
    expect(calls).toBeLessThanOrEqual(4)
  })

  it('slows to every 5 minutes once setup is complete', async () => {
    expect(await poll(true)).toBe(0)
  })
})

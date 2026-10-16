import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { StrictMode, useState } from 'react'
import { act, fireEvent, render, renderHook, screen, waitFor } from '@testing-library/react'
import { useSetupStatus } from '@/hooks/use-setup-status'

import { SignalKConnectionSection } from '@/components/settings/sections/signalk-connection-section'
import { SettingsFormProvider } from '@/components/settings/settings-form-context'
import { SecretsStatusProvider } from '@/components/settings/secrets-status-context'
import { initialRegularSettingsDraft, type RegularSettingsDraft } from '@/components/settings/settings-draft'

import { SetupLine } from '@/components/setup-line'
import { SignalKSetupSection } from '@/components/settings/sections/signalk-setup-section'
import { resetQueryStore } from '@/lib/api-query'
import { takePendingSetupFocus, requestSetupFocus, type SetupStatus } from '@/lib/setup-status'

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
  takePendingSetupFocus()
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
    expect(takePendingSetupFocus()).toBeNull()
  })

  it('ignores a focus request older than five seconds, and clears it', () => {
    vi.useFakeTimers()
    try {
      requestSetupFocus('role-house_bank')
      vi.advanceTimersByTime(6000)
      expect(takePendingSetupFocus()).toBeNull()
      requestSetupFocus('role-house_bank')
      vi.advanceTimersByTime(4000)
      expect(takePendingSetupFocus()).toBe('role-house_bank')
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
    expect(takePendingSetupFocus()).toBeNull()
  })

  it('keeps a requested focus for Settings to pick up', () => {
    requestSetupFocus('role-house_bank')
    expect(takePendingSetupFocus()).toBe('role-house_bank')
    expect(takePendingSetupFocus()).toBeNull()
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
    await act(async () => { await vi.advanceTimersByTimeAsync(60_000) })
    return fetchMock.mock.calls.length
  }

  it('polls every 15 seconds while setup is unfinished', async () => {
    expect(await poll(false)).toBeGreaterThanOrEqual(3)
  })

  it('slows to every 5 minutes once setup is complete', async () => {
    expect(await poll(true)).toBe(0)
  })
})

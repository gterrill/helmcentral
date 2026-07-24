import { describe, it, expect, vi, afterEach } from 'vitest'
import { useState } from 'react'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { SignalKConnectionSection } from '@/components/settings/sections/signalk-connection-section'
import { SecretsStatusProvider } from '@/components/settings/secrets-status-context'
import { initialRegularSettingsDraft, type RegularSettingsDraft } from '@/components/settings/settings-draft'

// The button used to be "Connect", and it persisted the address as a side
// effect of checking it — a second write path competing with the pinned "Save
// Settings" button. It is now a pure diagnostic: probe, report, change
// nothing. These tests pin the "changes nothing" half, which is the part that
// is easy to regress by wiring a save back in.

function renderSection(overrides: Partial<RegularSettingsDraft> = {}) {
  const draftStates: RegularSettingsDraft[] = []

  function Harness() {
    const [draft, setDraft] = useState<RegularSettingsDraft>({ ...initialRegularSettingsDraft, ...overrides })
    draftStates.push(draft)
    return (
      <SecretsStatusProvider>
        <SignalKConnectionSection
          draft={draft}
          onChange={(patch) => setDraft((previous) => ({ ...previous, ...patch }))}
        />
      </SecretsStatusProvider>
    )
  }

  render(<Harness />)
  return { latestDraft: () => draftStates[draftStates.length - 1] }
}

function stubFetch(handler: (url: string, init?: RequestInit) => unknown) {
  const mock = vi.fn(async (url: string, init?: RequestInit) => handler(url, init))
  vi.stubGlobal('fetch', mock)
  return mock
}

const secretsResponse = { ok: true, json: async () => ({ SIGNALK_USERNAME: false, SIGNALK_PASSWORD: false }) }

describe('SignalKConnectionSection', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('probes the typed address via the test endpoint and never posts to the settings save endpoint', async () => {
    const fetchMock = stubFetch((url) => {
      if (url.includes('/api/settings/signalk/test')) {
        return { ok: true, json: async () => ({ connected: true, url: 'http://10.0.0.5:3000', vessel_name: 'Pikorua' }) }
      }
      return secretsResponse
    })

    renderSection({ signalkAddress: '10.0.0.5', signalkPort: '3000' })

    fireEvent.click(screen.getByRole('button', { name: /test connection/i }))

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith(
        expect.stringContaining('/api/settings/signalk/test'),
        expect.objectContaining({ method: 'POST' }),
      )
    })

    const probeCall = fetchMock.mock.calls.find(([url]) => String(url).includes('/api/settings/signalk/test'))
    expect(JSON.parse(String((probeCall?.[1] as RequestInit).body))).toEqual({ address: '10.0.0.5', port: 3000 })

    // No save, by any route: not the bulk endpoint, not the old dedicated one.
    const saveCalls = fetchMock.mock.calls.filter(([url], index) => {
      const method = (fetchMock.mock.calls[index][1] as RequestInit | undefined)?.method
      return method === 'POST' && !String(url).includes('/api/settings/signalk/test')
    })
    expect(saveCalls).toEqual([])
  })

  it('reports the vessel that answered, so a wrong-but-reachable server is visible', async () => {
    stubFetch((url) => {
      if (url.includes('/api/settings/signalk/test')) {
        return { ok: true, json: async () => ({ connected: true, vessel_name: 'Someone Elses Boat' }) }
      }
      return secretsResponse
    })

    renderSection({ signalkAddress: '10.0.0.5', signalkPort: '3000' })
    fireEvent.click(screen.getByRole('button', { name: /test connection/i }))

    expect(await screen.findByText(/someone elses boat/i)).toBeInTheDocument()
  })

  it('leaves the draft untouched on a successful probe', async () => {
    stubFetch((url) => {
      if (url.includes('/api/settings/signalk/test')) {
        // A server echoing back a different address must not be able to
        // rewrite what the operator typed, as the old handler's response did.
        return { ok: true, json: async () => ({ connected: true, address: '192.168.9.9', port: 9999, vessel_name: 'Pikorua' }) }
      }
      return secretsResponse
    })

    const { latestDraft } = renderSection({ signalkAddress: '10.0.0.5', signalkPort: '3000' })
    fireEvent.click(screen.getByRole('button', { name: /test connection/i }))

    await screen.findByText(/pikorua/i)
    expect(latestDraft().signalkAddress).toBe('10.0.0.5')
    expect(latestDraft().signalkPort).toBe('3000')
  })

  it('surfaces the backend reason when the probe fails', async () => {
    stubFetch((url) => {
      if (url.includes('/api/settings/signalk/test')) {
        return {
          ok: false,
          json: async () => ({ error: 'unable to connect to SignalK at http://10.0.0.5:3000', field: 'signalk.address' }),
        }
      }
      return secretsResponse
    })

    renderSection({ signalkAddress: '10.0.0.5', signalkPort: '3000' })
    fireEvent.click(screen.getByRole('button', { name: /test connection/i }))

    expect(await screen.findByText(/unable to connect to signalk at http:\/\/10\.0\.0\.5:3000/i)).toBeInTheDocument()
  })
})

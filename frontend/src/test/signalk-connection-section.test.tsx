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

  // "Find Servers" is the explicitly-requested counterpart to the background
  // onboarding prompt (ADR 0029), which stays silent when it finds nothing.
  // Here the operator asked, so every outcome has to be reported — silence
  // would read as a broken button.
  describe('Find Servers', () => {
    const pikorua = {
      address: '192.168.50.240',
      port: 3000,
      url: 'http://192.168.50.240:3000',
      vessel_name: 'Pikorua',
      version: '2.24.0',
    }

    // The operator has an address field right here. If they type one and hit
    // Find Servers, that is the network they mean — not whatever stale value
    // happens to be persisted, which may be loopback or a dead subnet.
    it('searches the network of the address currently typed in the form', async () => {
      const fetchMock = stubFetch((url) => {
        if (url.includes('/api/signalk/discover')) {
          return { ok: true, json: async () => ({ servers: [pikorua], scanned_subnet: '192.168.50.0/24' }) }
        }
        return secretsResponse
      })

      renderSection({ signalkAddress: '192.168.50.99', signalkPort: '3000' })
      fireEvent.click(screen.getByRole('button', { name: /find servers/i }))

      await waitFor(() => {
        const call = fetchMock.mock.calls.find(([url]) => String(url).includes('/api/signalk/discover'))
        expect(call).toBeDefined()
        expect(JSON.parse(String((call![1] as RequestInit).body))).toEqual({ hint: '192.168.50.99' })
      })
    })

    it('lists discovered servers by vessel name', async () => {
      stubFetch((url) => {
        if (url.includes('/api/signalk/discover')) {
          return { ok: true, json: async () => ({ servers: [pikorua], scanned_subnet: '192.168.50.0/24' }) }
        }
        return secretsResponse
      })

      renderSection({ signalkAddress: 'localhost', signalkPort: '3000' })
      fireEvent.click(screen.getByRole('button', { name: /find servers/i }))

      expect(await screen.findByText(/pikorua/i)).toBeInTheDocument()
      expect(screen.getByText(/192\.168\.50\.240:3000/)).toBeInTheDocument()
    })

    it('fills the draft when a server is picked, without saving', async () => {
      const fetchMock = stubFetch((url) => {
        if (url.includes('/api/signalk/discover')) {
          return { ok: true, json: async () => ({ servers: [pikorua], scanned_subnet: '192.168.50.0/24' }) }
        }
        return secretsResponse
      })

      const { latestDraft } = renderSection({ signalkAddress: 'localhost', signalkPort: '3000' })
      fireEvent.click(screen.getByRole('button', { name: /find servers/i }))
      fireEvent.click(await screen.findByRole('button', { name: /pikorua/i }))

      await waitFor(() => {
        expect(latestDraft().signalkAddress).toBe('192.168.50.240')
      })
      expect(latestDraft().signalkPort).toBe('3000')

      // Consistent with the rest of this page: picking populates the form,
      // "Save Settings" persists. Nothing here writes on click.
      const saves = fetchMock.mock.calls.filter(
        ([url, init]) =>
          (init as RequestInit | undefined)?.method === 'POST' && !String(url).includes('/api/signalk/discover'),
      )
      expect(saves).toEqual([])
    })

    it('reports when nothing was found, naming the network it searched', async () => {
      stubFetch((url) => {
        if (url.includes('/api/signalk/discover')) {
          return { ok: true, json: async () => ({ servers: [], scanned_subnet: '192.168.50.0/24' }) }
        }
        return secretsResponse
      })

      renderSection({ signalkAddress: 'localhost', signalkPort: '3000' })
      fireEvent.click(screen.getByRole('button', { name: /find servers/i }))

      expect(await screen.findByText(/no signalk servers found/i)).toBeInTheDocument()
      expect(screen.getByText(/192\.168\.50\.0\/24/)).toBeInTheDocument()
    })

    it('surfaces the backend reason when the network cannot be determined', async () => {
      stubFetch((url) => {
        if (url.includes('/api/signalk/discover')) {
          return { ok: false, json: async () => ({ error: 'cannot determine which network to scan', field: 'signalk.address' }) }
        }
        return secretsResponse
      })

      renderSection({ signalkAddress: 'localhost', signalkPort: '3000' })
      fireEvent.click(screen.getByRole('button', { name: /find servers/i }))

      expect(await screen.findByText(/cannot determine which network to scan/i)).toBeInTheDocument()
    })
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

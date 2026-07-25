import { describe, it, expect, vi, afterEach, beforeEach } from 'vitest'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { SignalKDiscoveryPrompt } from '@/components/signalk-discovery-prompt'

// The prompt is the onboarding path: on a fresh install, or when a known
// server has moved, Helmcentral goes looking rather than leaving the operator
// to find an IP address themselves.
//
// Accepting a result saves immediately — but through useSettingsForm().save(),
// the single write path from ADR 0028. Here the operator's confirmation IS the
// deliberate act, unlike the old Connect button that saved as a side effect of
// a connectivity check. The test below asserts the request goes to
// /api/settings and not to some shortcut.

const settingsResponse = {
  signalk: { address: 'localhost', port: 3000 },
  boat: { vessel_prefix: 'M/V', model: '', house_battery_capacity_ah: 1440 },
  ui: {},
  anchor: {},
  influxdb: {},
  units: 'metric',
}

function stubFetch(overrides: {
  servers?: unknown[]
  scannedSubnet?: string
  discoverStatus?: number
  discoverError?: string
}) {
  const mock = vi.fn(async (url: string, init?: RequestInit) => {
    if (String(url).includes('/api/signalk/discover')) {
      if (overrides.discoverStatus && overrides.discoverStatus >= 400) {
        return { ok: false, json: async () => ({ error: overrides.discoverError, field: 'signalk.address' }) }
      }
      return {
        ok: true,
        json: async () => ({
          servers: overrides.servers ?? [],
          scanned_subnet: overrides.scannedSubnet ?? '192.168.50.0/24',
        }),
      }
    }
    if (String(url).includes('/api/settings') && init?.method === 'POST') {
      return { ok: true, json: async () => settingsResponse }
    }
    return { ok: true, json: async () => settingsResponse }
  })
  vi.stubGlobal('fetch', mock)
  return mock
}

const pikorua = {
  address: '192.168.50.240',
  port: 3000,
  url: 'http://192.168.50.240:3000',
  vessel_name: 'Pikorua',
  version: '2.24.0',
}

describe('SignalKDiscoveryPrompt', () => {
  beforeEach(() => {
    globalThis.localStorage?.clear()
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('offers the discovered vessel by name when SignalK is unconfigured', async () => {
    stubFetch({ servers: [pikorua] })

    render(<SignalKDiscoveryPrompt configuredAddress="" vesselStateSource={null} />)

    expect(await screen.findByText(/pikorua/i)).toBeInTheDocument()
    expect(screen.getByText(/192\.168\.50\.240:3000/)).toBeInTheDocument()
  })

  it('offers to search when a configured server has gone unreachable', async () => {
    stubFetch({ servers: [pikorua] })

    render(<SignalKDiscoveryPrompt configuredAddress="192.168.50.99" vesselStateSource="signalk-unreachable" />)

    expect(await screen.findByText(/pikorua/i)).toBeInTheDocument()
  })

  it('stays silent while SignalK is working normally', async () => {
    const fetchMock = stubFetch({ servers: [pikorua] })

    render(<SignalKDiscoveryPrompt configuredAddress="192.168.50.240" vesselStateSource="signalk" />)

    await waitFor(() => {
      expect(fetchMock.mock.calls.filter(([url]) => String(url).includes('/discover'))).toEqual([])
    })
    expect(screen.queryByText(/pikorua/i)).not.toBeInTheDocument()
  })

  it('saves through the settings endpoint when the operator accepts', async () => {
    const fetchMock = stubFetch({ servers: [pikorua] })

    render(<SignalKDiscoveryPrompt configuredAddress="" vesselStateSource={null} />)

    fireEvent.click(await screen.findByRole('button', { name: /store these settings/i }))

    await waitFor(() => {
      const saves = fetchMock.mock.calls.filter(
        ([url, init]) => String(url).includes('/api/settings') && (init as RequestInit | undefined)?.method === 'POST',
      )
      expect(saves).toHaveLength(1)
      const body = JSON.parse(String((saves[0][1] as RequestInit).body))
      expect(body.signalk).toEqual({ address: '192.168.50.240', port: 3000 })
    })
  })

  it('does not re-prompt for an address the operator already dismissed', async () => {
    const fetchMock = stubFetch({ servers: [pikorua] })

    const { unmount } = render(<SignalKDiscoveryPrompt configuredAddress="192.168.50.99" vesselStateSource="signalk-unreachable" />)
    fireEvent.click(await screen.findByRole('button', { name: /not now/i }))
    unmount()

    fetchMock.mockClear()
    render(<SignalKDiscoveryPrompt configuredAddress="192.168.50.99" vesselStateSource="signalk-unreachable" />)

    await waitFor(() => {
      expect(fetchMock.mock.calls.filter(([url]) => String(url).includes('/discover'))).toEqual([])
    })
  })

  // Dismissing "the server at .99 is gone" must not also suppress the prompt
  // when a *different* address later fails — that would be a different problem
  // going unreported.
  it('does re-prompt when a different address goes unreachable', async () => {
    stubFetch({ servers: [pikorua] })

    const { unmount } = render(<SignalKDiscoveryPrompt configuredAddress="192.168.50.99" vesselStateSource="signalk-unreachable" />)
    fireEvent.click(await screen.findByRole('button', { name: /not now/i }))
    unmount()

    render(<SignalKDiscoveryPrompt configuredAddress="192.168.50.77" vesselStateSource="signalk-unreachable" />)
    expect(await screen.findByText(/pikorua/i)).toBeInTheDocument()
  })

  // The search is a background courtesy the operator never asked for. Putting
  // a modal over the whole dashboard to announce that it found nothing — or
  // that it couldn't run — is worse than staying quiet; Settings → SignalK is
  // still there for manual entry.
  it('stays silent when it finds nothing', async () => {
    const fetchMock = stubFetch({ servers: [], scannedSubnet: '192.168.50.0/24' })

    render(<SignalKDiscoveryPrompt configuredAddress="" vesselStateSource={null} />)

    await waitFor(() => {
      expect(fetchMock.mock.calls.some(([url]) => String(url).includes('/discover'))).toBe(true)
    })
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument()
  })

  it('stays silent when the search itself fails', async () => {
    const fetchMock = stubFetch({ discoverStatus: 400, discoverError: 'cannot determine which network to scan' })

    render(<SignalKDiscoveryPrompt configuredAddress="" vesselStateSource={null} />)

    await waitFor(() => {
      expect(fetchMock.mock.calls.some(([url]) => String(url).includes('/discover'))).toBe(true)
    })
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument()
  })

  // A scan takes seconds; blocking the dashboard while it runs would make the
  // app feel broken on every fresh start.
  it('does not block the app while searching', async () => {
    stubFetch({ servers: [pikorua] })

    render(<SignalKDiscoveryPrompt configuredAddress="" vesselStateSource={null} />)

    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument()
    expect(await screen.findByText(/pikorua/i)).toBeInTheDocument()
  })
})

import { describe, it, expect, vi, afterEach, beforeEach } from 'vitest'
import type { ReactElement } from 'react'
import { act, fireEvent, render, screen } from '@testing-library/react'
import {
  SignalKDiscoveryPrompt,
  UNREACHABLE_GRACE_MS,
  SEARCH_RETRY_MS,
} from '@/components/signalk-discovery-prompt'

// The prompt is the onboarding path: on a fresh install, or when a known
// server has moved, Helmcentral goes looking rather than leaving the operator
// to find an IP address themselves.
//
// Accepting a result saves immediately — but through useSettingsForm().save(),
// the single write path from ADR 0028. Here the operator's confirmation IS the
// deliberate act, unlike the old Connect button that saved as a side effect of
// a connectivity check. The test below asserts the request goes to
// /api/settings and not to some shortcut.
//
// All of it runs on fake timers, because the prompt is mostly about waiting:
// out a restart, out a failed search, out a failed settings read.

let telemetryStatus: 'connected' | 'reconnecting' | 'disconnected' = 'connected'
vi.mock('@/hooks/use-telemetry-stream', () => ({
  useTelemetryStatus: () => telemetryStatus,
}))

interface Saved {
  address: string
  port?: number
}

function stubFetch(overrides: {
  saved?: Saved
  settingsFail?: () => boolean
  servers?: unknown[] | (() => unknown[])
  discoverFail?: () => boolean
}) {
  const settingsResponse = () => ({
    signalk: overrides.saved ?? { address: 'localhost', port: 3000 },
    boat: { vessel_prefix: 'M/V', model: '' },
    ui: {},
    anchor: {},
    influxdb: {},
    units: 'metric',
  })
  const mock = vi.fn(async (url: string, init?: RequestInit) => {
    if (String(url).includes('/api/signalk/discover')) {
      if (overrides.discoverFail?.()) return { ok: false, json: async () => ({ error: 'cannot determine which network to scan' }) }
      const servers = typeof overrides.servers === 'function' ? overrides.servers() : (overrides.servers ?? [])
      return { ok: true, json: async () => ({ servers, scanned_subnet: '192.168.50.0/24' }) }
    }
    if (String(url).includes('/api/settings')) {
      if (init?.method !== 'POST' && overrides.settingsFail?.()) return { ok: false, json: async () => ({}) }
      return { ok: true, json: async () => settingsResponse() }
    }
    return { ok: true, json: async () => ({}) }
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
  configured: false,
}
const neighbour = { ...pikorua, address: '192.168.50.77', vessel_name: 'Neighbour' }

const advance = (ms: number) => act(async () => { await vi.advanceTimersByTimeAsync(ms) })
// Lets settings and a search that has already started settle, without moving the clock on.
const settle = async () => { await advance(0); await advance(0) }
const discoverCalls = (m: ReturnType<typeof stubFetch>) => m.mock.calls.filter(([url]) => String(url).includes('/discover'))
const dismissedKey = () => globalThis.localStorage?.getItem('helmcentral.signalk-discovery-dismissed-for')
const prompt = (source: string | null) => <SignalKDiscoveryPrompt vesselStateSource={source} />
// Renders, then lets the settings read finish before any clock is advanced.
async function mount(ui: ReactElement) {
  const result = render(ui)
  await act(async () => { await vi.advanceTimersByTimeAsync(0) })
  return result
}
const dialog = () => screen.queryByRole('alertdialog')

describe('SignalKDiscoveryPrompt', () => {
  beforeEach(() => {
    globalThis.localStorage?.clear()
    telemetryStatus = 'connected'
    vi.useFakeTimers()
  })

  afterEach(() => {
    vi.useRealTimers()
    vi.unstubAllGlobals()
  })

  describe('when SignalK is unconfigured', () => {
    it('searches at once and offers the discovered vessel by name', async () => {
      const fetchMock = stubFetch({ servers: [pikorua] })
      await mount(prompt(null))
      await settle()

      expect(discoverCalls(fetchMock)).toHaveLength(1)
      expect(screen.getByText(/pikorua/i)).toBeInTheDocument()
      expect(screen.getByText(/192\.168\.50\.240:3000/)).toBeInTheDocument()
    })

    it('saves through the settings endpoint when the operator accepts', async () => {
      const fetchMock = stubFetch({ servers: [pikorua] })
      await mount(prompt(null))
      await settle()

      fireEvent.click(screen.getByRole('button', { name: /store these settings/i }))
      await settle()

      const saves = fetchMock.mock.calls.filter(([url, init]) => String(url).includes('/api/settings') && (init as RequestInit | undefined)?.method === 'POST')
      expect(saves).toHaveLength(1)
      expect(JSON.parse(String((saves[0][1] as RequestInit).body)).signalk).toEqual({ address: '192.168.50.240', port: 3000 })
    })

    it('stays silent when it finds nothing', async () => {
      const fetchMock = stubFetch({ servers: [] })
      await mount(prompt(null))
      await settle()

      expect(discoverCalls(fetchMock)).toHaveLength(1)
      expect(dialog()).not.toBeInTheDocument()
    })
  })

  it('stays silent while SignalK is working normally', async () => {
    const fetchMock = stubFetch({ saved: { address: '192.168.50.240', port: 3000 }, servers: [pikorua] })
    await mount(prompt('signalk'))
    await advance(UNREACHABLE_GRACE_MS * 2)

    expect(discoverCalls(fetchMock)).toEqual([])
    expect(dialog()).not.toBeInTheDocument()
  })

  describe('a configured server that goes unreachable', () => {
    const saved = { address: '192.168.50.99', port: 3000 }

    it('is searched for only after 30 s of continuous unreachable', async () => {
      const fetchMock = stubFetch({ saved, servers: [pikorua] })
      await mount(prompt('signalk-unreachable'))
      await advance(UNREACHABLE_GRACE_MS - 1)
      expect(discoverCalls(fetchMock)).toEqual([])

      await advance(1)
      await settle()
      expect(discoverCalls(fetchMock)).toHaveLength(1)
      expect(screen.getByText(/pikorua/i)).toBeInTheDocument()
    })

    it('searches once, however long it stays unreachable', async () => {
      const fetchMock = stubFetch({ saved, servers: [] })
      const { rerender } = await mount(prompt('signalk-unreachable'))
      await advance(UNREACHABLE_GRACE_MS)
      rerender(prompt('signalk-unreachable'))
      await advance(UNREACHABLE_GRACE_MS * 3)

      expect(discoverCalls(fetchMock)).toHaveLength(1)
    })

    it('never searches when SignalK answers again within the grace period', async () => {
      const fetchMock = stubFetch({ saved, servers: [pikorua] })
      const { rerender } = await mount(prompt('signalk-unreachable'))
      await advance(UNREACHABLE_GRACE_MS - 5_000)
      rerender(prompt('signalk'))
      await advance(UNREACHABLE_GRACE_MS * 2)

      expect(discoverCalls(fetchMock)).toEqual([])
    })

    it('restarts the clock after a good reading', async () => {
      const fetchMock = stubFetch({ saved, servers: [pikorua] })
      const { rerender } = await mount(prompt('signalk-unreachable'))
      await advance(UNREACHABLE_GRACE_MS - 1_000)
      rerender(prompt('signalk'))
      rerender(prompt('signalk-unreachable'))
      await advance(UNREACHABLE_GRACE_MS - 1_000)

      expect(discoverCalls(fetchMock)).toEqual([])
    })

    // The stream keeps its last reading when it drops, so a restart longer
    // than the grace period would otherwise look like 30 s of "unreachable".
    it('does not count time while the vessel-state stream is down', async () => {
      const fetchMock = stubFetch({ saved, servers: [pikorua] })
      const { rerender } = await mount(prompt('signalk-unreachable'))
      await advance(UNREACHABLE_GRACE_MS - 5_000)

      telemetryStatus = 'reconnecting'
      rerender(prompt('signalk-unreachable'))
      await advance(45_000)

      telemetryStatus = 'connected'
      rerender(prompt('signalk'))
      await advance(UNREACHABLE_GRACE_MS * 2)

      expect(discoverCalls(fetchMock)).toEqual([])
    })

    it('starts the clock afresh on reconnect when readings still say unreachable', async () => {
      const fetchMock = stubFetch({ saved, servers: [pikorua] })
      const { rerender } = await mount(prompt('signalk-unreachable'))
      await advance(UNREACHABLE_GRACE_MS - 1_000)
      telemetryStatus = 'disconnected'
      rerender(prompt('signalk-unreachable'))
      await advance(45_000)
      telemetryStatus = 'connected'
      rerender(prompt('signalk-unreachable'))

      await advance(UNREACHABLE_GRACE_MS - 1)
      expect(discoverCalls(fetchMock)).toEqual([])
      await advance(1)
      expect(discoverCalls(fetchMock)).toHaveLength(1)
    })
  })

  describe('the server already configured', () => {
    const saved = { address: '192.168.50.240', port: 3000 }
    const unreachable = async (servers: unknown[], savedSettings: Saved = saved) => {
      const fetchMock = stubFetch({ saved: savedSettings, servers })
      await mount(prompt('signalk-unreachable'))
      await advance(UNREACHABLE_GRACE_MS)
      await settle()
      return fetchMock
    }

    it('is not offered when the search finds only that server, and no dismissal is recorded', async () => {
      const fetchMock = await unreachable([pikorua])
      expect(discoverCalls(fetchMock)).toHaveLength(1)
      expect(dialog()).not.toBeInTheDocument()
      expect(dismissedKey()).toBeNull()
    })

    it('is not offered when the address was saved with stray spaces', async () => {
      await unreachable([pikorua], { address: '  192.168.50.240 ', port: 3000 })
      expect(dialog()).not.toBeInTheDocument()
    })

    it('is not offered when the port was saved empty, meaning 3000', async () => {
      await unreachable([pikorua], { address: '192.168.50.240' })
      expect(dialog()).not.toBeInTheDocument()
    })

    // The sweep returns IPv4 literals, so a saved hostname never equals what it
    // finds. The server says which result is the saved one.
    it('is not offered when the server marks it configured, whatever the saved address says', async () => {
      await unreachable([{ ...pikorua, configured: true }], { address: 'boat.local', port: 3000 })
      expect(dialog()).not.toBeInTheDocument()
      expect(dismissedKey()).toBeNull()
    })

    it('leaves only the other server when two are found', async () => {
      await unreachable([{ ...pikorua, configured: true }, neighbour])
      expect(screen.getByText(/neighbour/i)).toBeInTheDocument()
      expect(screen.queryByText(/pikorua/i)).not.toBeInTheDocument()
      expect(screen.queryByText(/several/i)).not.toBeInTheDocument()
    })

    it('leaves only the other server when the match is by address and port', async () => {
      await unreachable([pikorua, neighbour])
      expect(screen.getByText(/neighbour/i)).toBeInTheDocument()
      expect(screen.queryByText(/pikorua/i)).not.toBeInTheDocument()
    })

    it('still offers the same address on a different port', async () => {
      await unreachable([{ ...pikorua, port: 3001 }])
      expect(screen.getByText(/192\.168\.50\.240:3001/)).toBeInTheDocument()
    })
  })

  describe('dismissal', () => {
    const saved = { address: '192.168.50.99', port: 3000 }

    it('is stored against address and port, and stops a later prompt for the same server', async () => {
      const fetchMock = stubFetch({ saved, servers: [pikorua] })
      const first = await mount(prompt('signalk-unreachable'))
      await advance(UNREACHABLE_GRACE_MS)
      await settle()
      fireEvent.click(screen.getByRole('button', { name: /not now/i }))
      expect(dismissedKey()).toBe('192.168.50.99:3000')
      first.unmount()

      fetchMock.mockClear()
      await mount(prompt('signalk-unreachable'))
      await advance(UNREACHABLE_GRACE_MS * 2)
      expect(discoverCalls(fetchMock)).toEqual([])
    })

    it('honours a dismissal stored by an older version under the address alone, for port 3000', async () => {
      globalThis.localStorage?.setItem('helmcentral.signalk-discovery-dismissed-for', '192.168.50.99')
      const fetchMock = stubFetch({ saved, servers: [pikorua] })
      await mount(prompt('signalk-unreachable'))
      await advance(UNREACHABLE_GRACE_MS * 2)
      expect(discoverCalls(fetchMock)).toEqual([])
    })

    it('does not carry an address-only dismissal over to another port', async () => {
      globalThis.localStorage?.setItem('helmcentral.signalk-discovery-dismissed-for', '192.168.50.99')
      const fetchMock = stubFetch({ saved: { address: '192.168.50.99', port: 3001 }, servers: [pikorua] })
      await mount(prompt('signalk-unreachable'))
      await advance(UNREACHABLE_GRACE_MS)
      expect(discoverCalls(fetchMock)).toHaveLength(1)
    })

    it('does not hold when a different address goes unreachable', async () => {
      globalThis.localStorage?.setItem('helmcentral.signalk-discovery-dismissed-for', '192.168.50.99:3000')
      stubFetch({ saved: { address: '192.168.50.77', port: 3000 }, servers: [pikorua] })
      await mount(prompt('signalk-unreachable'))
      await advance(UNREACHABLE_GRACE_MS)
      await settle()
      expect(screen.getByText(/pikorua/i)).toBeInTheDocument()
    })
  })

  describe('a search that fails', () => {
    it('is tried again after a backoff while the situation persists', async () => {
      let failing = true
      const fetchMock = stubFetch({ servers: [pikorua], discoverFail: () => failing })
      await mount(prompt(null))
      await settle()
      expect(discoverCalls(fetchMock)).toHaveLength(1)
      expect(dialog()).not.toBeInTheDocument()

      failing = false
      await advance(SEARCH_RETRY_MS - 1)
      expect(discoverCalls(fetchMock)).toHaveLength(1)
      await advance(1)
      await settle()
      expect(discoverCalls(fetchMock)).toHaveLength(2)
      expect(screen.getByText(/pikorua/i)).toBeInTheDocument()
    })

    it('is not retried once the situation has gone', async () => {
      const fetchMock = stubFetch({ saved: { address: '192.168.50.99', port: 3000 }, discoverFail: () => true })
      const { rerender } = await mount(prompt('signalk-unreachable'))
      await advance(UNREACHABLE_GRACE_MS)
      await settle()
      expect(discoverCalls(fetchMock)).toHaveLength(1)

      rerender(prompt('signalk'))
      await advance(SEARCH_RETRY_MS * 3)
      expect(discoverCalls(fetchMock)).toHaveLength(1)
    })

    it('is not repeated when it completed and found nothing acceptable', async () => {
      const fetchMock = stubFetch({ servers: [] })
      await mount(prompt(null))
      await settle()
      await advance(SEARCH_RETRY_MS * 3)
      expect(discoverCalls(fetchMock)).toHaveLength(1)
    })

    it('is not repeated when it found only the configured server', async () => {
      const fetchMock = stubFetch({ saved: { address: '192.168.50.240', port: 3000 }, servers: [pikorua] })
      await mount(prompt('signalk-unreachable'))
      await advance(UNREACHABLE_GRACE_MS)
      await settle()
      await advance(SEARCH_RETRY_MS * 3)
      expect(discoverCalls(fetchMock)).toHaveLength(1)
    })
  })

  describe('settings that did not load', () => {
    it('never read as unconfigured, even if the source says unreachable', async () => {
      const fetchMock = stubFetch({ servers: [pikorua], settingsFail: () => true })
      await mount(prompt('signalk-unreachable'))
      await advance(UNREACHABLE_GRACE_MS * 2)
      expect(discoverCalls(fetchMock)).toEqual([])
      expect(dialog()).not.toBeInTheDocument()
    })

    it('are retried, and a search follows only once they have loaded', async () => {
      let failing = true
      const fetchMock = stubFetch({ servers: [pikorua], settingsFail: () => failing })
      await mount(prompt(null))
      await settle()
      expect(discoverCalls(fetchMock)).toEqual([])

      failing = false
      await advance(5_000)
      await settle()
      expect(discoverCalls(fetchMock)).toHaveLength(1)
      expect(screen.getByText(/pikorua/i)).toBeInTheDocument()
    })
  })

  it('stays silent when the search itself fails', async () => {
    const fetchMock = stubFetch({ discoverFail: () => true })
    await mount(prompt(null))
    await settle()
    expect(discoverCalls(fetchMock)).toHaveLength(1)
    expect(dialog()).not.toBeInTheDocument()
  })
})

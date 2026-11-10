import { beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'

import { LearnedNormalStatus } from '@/components/settings/sections/learned-normal-status'
import { resetQueryStore } from '@/lib/api-query'

// ADR 0276: what the learned normal holds, and a switch for each engine reading
// that is the existing ignored-sensor list.

const status = {
  configured: true,
  summary: 'Learned Tue 10 Nov 3.00 AM from main bucket.',
  engines: [
    {
      id: 'port',
      name: 'Port engine',
      bands: 2,
      readings: [
        { key: 'revolutions', path: 'propulsion.port.revolutions', name: 'rpm', on: true, required: true, status: 'used' },
        { key: 'oilPressure', path: 'propulsion.port.oilPressure', name: 'oil pressure', on: true, required: false, status: 'used' },
        { key: 'transmission.oilTemperature', path: 'propulsion.port.transmission.oilTemperature', name: 'gear oil temperature', on: true, required: false, status: 'never changed while running' },
        { key: 'exhaustTemperature', path: 'propulsion.port.exhaustTemperature', name: 'exhaust temperature', on: false, required: false, status: 'switched off' },
        { key: 'fuel.rate', path: 'propulsion.port.fuel.rate', name: 'fuel rate', on: true, required: false, status: 'joins after 60 minutes, 14 so far' },
      ],
    },
    { id: 'starboard', name: 'Starboard engine', bands: 0, problem: 'no steady running minutes in the last 90 days', readings: [] },
  ],
}

function stubApi(body: unknown = status) {
  const calls: Array<{ url: string; method: string }> = []
  const fetchMock = vi.fn(async (url: string, init?: RequestInit) => {
    calls.push({ url, method: init?.method ?? 'GET' })
    if (url.includes('/api/anomaly/normal/status')) return { ok: true, status: 200, json: async () => body }
    if (init?.method === 'DELETE') return { ok: true, status: 204, json: async () => ({}) }
    return { ok: true, status: 200, json: async () => ({ identifiers: ['propulsion.port.oilPressure'], sensors: [] }) }
  })
  vi.stubGlobal('fetch', fetchMock)
  return calls
}

describe('LearnedNormalStatus', () => {
  beforeEach(() => resetQueryStore())

  it('says when and from where the normal was learned, and how many bands each engine has', async () => {
    stubApi()
    render(<LearnedNormalStatus />)
    expect(await screen.findByText(status.summary)).toBeInTheDocument()
    expect(screen.getByText('Port engine')).toBeInTheDocument()
    expect(screen.getByText('Starboard engine')).toBeInTheDocument()
    expect(screen.getByText('no steady running minutes in the last 90 days')).toBeInTheDocument()
  })

  it('gives each reading a switch with the automatic verdict beside it', async () => {
    stubApi()
    render(<LearnedNormalStatus />)
    await screen.findByText(status.summary)

    const oil = screen.getByRole('switch', { name: 'Port engine oil pressure' })
    expect(oil).toBeChecked()
    const oilRow = oil.closest('li') as HTMLElement
    expect(within(oilRow).getByText('used')).toBeInTheDocument()

    expect(screen.getByRole('switch', { name: 'Port engine gear oil temperature' })).toBeChecked()
    expect(screen.getByText('never changed while running')).toBeInTheDocument()
    expect(screen.getByRole('switch', { name: 'Port engine exhaust temperature' })).not.toBeChecked()
    expect(screen.getByText('switched off')).toBeInTheDocument()
    expect(screen.getByText('joins after 60 minutes, 14 so far')).toBeInTheDocument()
  })

  // The name is never cut. The verdict sits under it and wraps instead.
  it('shows each reading name whole, with its verdict on its own line beneath', async () => {
    stubApi()
    render(<LearnedNormalStatus />)
    await screen.findByText(status.summary)
    const row = screen.getByRole('switch', { name: 'Port engine gear oil temperature' }).closest('li') as HTMLElement
    const name = within(row).getByTestId('reading-name')
    const verdict = within(row).getByTestId('reading-status')
    expect(name).toHaveTextContent('gear oil temperature')
    expect(name.className).not.toMatch(/truncate|overflow-hidden|text-ellipsis|line-clamp/)
    expect(verdict).toHaveTextContent('never changed while running')
    expect(verdict.className).toContain('text-muted-foreground')
    expect(verdict.className).not.toMatch(/truncate|line-clamp/)
    expect(name.contains(verdict)).toBe(false)
    expect(name.parentElement).toBe(verdict.parentElement)
    expect(name.compareDocumentPosition(verdict) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
  })

  it('does not repeat the engines\' band counts in the summary line', async () => {
    stubApi()
    render(<LearnedNormalStatus />)
    const summary = await screen.findByText(status.summary)
    expect(summary).not.toHaveTextContent(/bands?/)
    expect(screen.getByText('Port engine').parentElement).toHaveTextContent('2 bands')
  })

  it('cannot switch off a reading the engine cannot be judged without', async () => {
    stubApi()
    render(<LearnedNormalStatus />)
    await screen.findByText(status.summary)
    const rpm = screen.getByRole('switch', { name: 'Port engine rpm' })
    expect(rpm).toHaveAttribute('aria-disabled', 'true')
    expect(rpm).toHaveAttribute('aria-checked', 'true')
    expect(within(rpm.closest('li') as HTMLElement).getByText(/needed to judge the engine/i)).toBeInTheDocument()
  })

  it('lets a needed reading that is switched off be switched back on', async () => {
    const off = {
      ...status,
      engines: status.engines.map((e) => ({
        ...e,
        readings: e.readings.map((r) =>
          r.key === 'revolutions' ? { ...r, on: false, status: 'switched off' } : r,
        ),
      })),
    }
    const calls = stubApi(off)
    render(<LearnedNormalStatus />)
    await screen.findByText(off.summary)
    const rpm = screen.getByRole('switch', { name: 'Port engine rpm' })
    expect(rpm).not.toHaveAttribute('aria-disabled', 'true')
    expect(within(rpm.closest('li') as HTMLElement).getByText(/switched off/i)).toBeInTheDocument()
    fireEvent.click(rpm)
    await waitFor(() =>
      expect(calls.some((c) => c.method === 'DELETE' && c.url.endsWith('/api/alarms/ignored-sensors/propulsion.port.revolutions'))).toBe(true),
    )
  })

  it('switching a reading off adds it to the ignored sensors and reads the status again', async () => {
    const calls = stubApi()
    render(<LearnedNormalStatus />)
    await screen.findByText(status.summary)

    fireEvent.click(screen.getByRole('switch', { name: 'Port engine oil pressure' }))

    await waitFor(() => expect(calls.some((c) => c.method === 'POST' && c.url.endsWith('/api/alarms/ignored-sensors'))).toBe(true))
    const statusReads = () => calls.filter((c) => c.url.includes('/api/anomaly/normal/status')).length
    await waitFor(() => expect(statusReads()).toBeGreaterThanOrEqual(2))
  })

  it('switching a reading back on removes it from the ignored sensors', async () => {
    const calls = stubApi()
    render(<LearnedNormalStatus />)
    await screen.findByText(status.summary)

    fireEvent.click(screen.getByRole('switch', { name: 'Port engine exhaust temperature' }))

    await waitFor(() =>
      expect(calls.some((c) => c.method === 'DELETE' && c.url.endsWith('/api/alarms/ignored-sensors/propulsion.port.exhaustTemperature'))).toBe(true),
    )
  })

  it('says why there is no learned normal instead of showing nothing', async () => {
    stubApi({ configured: false, note: 'No learned normal: InfluxDB is not set up.', engines: [] })
    render(<LearnedNormalStatus />)
    expect(await screen.findByText('No learned normal: InfluxDB is not set up.')).toBeInTheDocument()
    expect(screen.queryByRole('switch')).toBeNull()
  })

  it('shows the latest failed run beside a good model', async () => {
    stubApi({ ...status, note: 'The learned normal was not updated: InfluxDB could not be read: timeout.' })
    render(<LearnedNormalStatus />)
    expect(await screen.findByText(status.summary)).toBeInTheDocument()
    expect(screen.getByText('The learned normal was not updated: InfluxDB could not be read: timeout.')).toBeInTheDocument()
  })

  it('says what is wrong when the status cannot be read', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => ({ ok: false, status: 500, json: async () => ({ error: 'boom' }) })))
    render(<LearnedNormalStatus />)
    expect(await screen.findByRole('alert')).toHaveTextContent('boom')
  })
})

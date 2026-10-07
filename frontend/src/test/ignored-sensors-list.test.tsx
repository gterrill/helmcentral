import { describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'

import { IgnoredSensorsList } from '@/components/settings/sections/ignored-sensors-list'

describe('IgnoredSensorsList', () => {
  it('shows a message when nothing is ignored', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => ({ ok: true, json: async () => ({ identifiers: [] }) })))
    render(<IgnoredSensorsList />)
    expect(await screen.findByText('No sensors are currently ignored.')).toBeInTheDocument()
  })

  it('lists every ignored identifier with a Remove action', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => ({ ok: true, json: async () => ({
      identifiers: ['propulsion.port.exhaustTemperature', 'venus.battery.512'],
      sensors: [
        { identifier: 'propulsion.port.exhaustTemperature', name: 'Port engine', label: 'Port engine exhaust temperature', text: 'Port engine exhaust temperature' },
        { identifier: 'venus.battery.512', name: 'House bank', label: 'House bank', text: 'House bank' },
      ],
    }) })))
    render(<IgnoredSensorsList />)

    expect(await screen.findByText('Port engine exhaust temperature')).toBeInTheDocument()
    expect(screen.getByText('House bank')).toBeInTheDocument()
    expect(screen.queryByText('venus.battery.512')).toBeNull()
    expect(screen.getAllByRole('button', { name: 'Remove' })).toHaveLength(2)
  })

  it('removing one calls DELETE and drops it from the list', async () => {
    const fetchMock = vi.fn(async (_url: string, init?: RequestInit) => {
      if (init?.method === 'DELETE') return { ok: true, json: async () => ({}) }
      return { ok: true, json: async () => ({
        identifiers: ['propulsion.port.exhaustTemperature'],
        sensors: [{ identifier: 'propulsion.port.exhaustTemperature', name: 'Port engine', label: 'Port engine exhaust temperature', text: 'Port engine exhaust temperature' }],
      }) }
    })
    vi.stubGlobal('fetch', fetchMock)
    render(<IgnoredSensorsList />)

    fireEvent.click(await screen.findByRole('button', { name: 'Remove' }))

    await waitFor(() => expect(screen.queryByText('Port engine exhaust temperature')).toBeNull())
    expect(fetchMock).toHaveBeenCalledWith('/api/alarms/ignored-sensors/propulsion.port.exhaustTemperature', { method: 'DELETE' })
    expect(await screen.findByText('No sensors are currently ignored.')).toBeInTheDocument()
  })
})

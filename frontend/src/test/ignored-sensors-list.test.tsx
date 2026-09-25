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
    vi.stubGlobal('fetch', vi.fn(async () => ({ ok: true, json: async () => ({ identifiers: ['propulsion.port.exhaustTemperature', 'venus.battery.512'] }) })))
    render(<IgnoredSensorsList />)

    expect(await screen.findByText('propulsion.port.exhaustTemperature')).toBeInTheDocument()
    expect(screen.getByText('venus.battery.512')).toBeInTheDocument()
    expect(screen.getAllByRole('button', { name: 'Remove' })).toHaveLength(2)
  })

  it('removing one calls DELETE and drops it from the list', async () => {
    const fetchMock = vi.fn(async (_url: string, init?: RequestInit) => {
      if (init?.method === 'DELETE') return { ok: true, json: async () => ({}) }
      return { ok: true, json: async () => ({ identifiers: ['propulsion.port.exhaustTemperature'] }) }
    })
    vi.stubGlobal('fetch', fetchMock)
    render(<IgnoredSensorsList />)

    fireEvent.click(await screen.findByRole('button', { name: 'Remove' }))

    await waitFor(() => expect(screen.queryByText('propulsion.port.exhaustTemperature')).toBeNull())
    expect(fetchMock).toHaveBeenCalledWith('/api/alarms/ignored-sensors/propulsion.port.exhaustTemperature', { method: 'DELETE' })
    expect(await screen.findByText('No sensors are currently ignored.')).toBeInTheDocument()
  })
})

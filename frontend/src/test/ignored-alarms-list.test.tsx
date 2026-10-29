import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { IgnoredAlarmsList } from '@/components/settings/sections/ignored-alarms-list'
import { installFakeTelemetryStream } from './helpers/fake-telemetry-stream'

// ADR 0228: times are the boat's. The runner is on UTC.
installFakeTelemetryStream({ seeds: { 'vessel-state': { timezone: 'UTC', timezone_source: 'fix' } } })


describe('IgnoredAlarmsList', () => {
  it('says where an ignored alarm comes from when none are ignored', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => ({ ok: true, json: async () => ({ alarms: [] }) })))
    render(<IgnoredAlarmsList />)
    expect(await screen.findByText('Alarms you stop showing from the alarms drawer appear here.')).toBeInTheDocument()
  })

  it('lists each alarm by title with its date and a Restore button', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => ({ ok: true, json: async () => ({
      alarms: [{ kind: 'perpendicularPassed', title: 'Waypoint Passed', ignored_at: '2026-10-12T08:00:00Z' }],
    }) })))
    render(<IgnoredAlarmsList />)

    expect(await screen.findByText('Waypoint Passed')).toBeInTheDocument()
    expect(screen.queryByText('perpendicularPassed')).toBeNull()
    expect(screen.getByText(/Ignored .*2026/)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Restore Waypoint Passed' })).toBeInTheDocument()
  })

  it('restoring calls DELETE with the kind and drops the row', async () => {
    const fetchMock = vi.fn(async (_url: string, init?: RequestInit) => {
      if (init?.method === 'DELETE') return { ok: true, json: async () => ({}) }
      return { ok: true, json: async () => ({
        alarms: [{ kind: 'navigation.arrivalCircleEntered', title: 'Arriving at Waypoint', ignored_at: '2026-10-12T08:00:00Z' }],
      }) }
    })
    vi.stubGlobal('fetch', fetchMock)
    render(<IgnoredAlarmsList />)

    fireEvent.click(await screen.findByRole('button', { name: 'Restore Arriving at Waypoint' }))

    await waitFor(() => expect(screen.queryByText('Arriving at Waypoint')).toBeNull())
    expect(fetchMock).toHaveBeenCalledWith('/api/alarms/ignored-alarms/navigation.arrivalCircleEntered', expect.objectContaining({ method: 'DELETE' }))
  })
})

import { beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'

import { ApplyGaugeZones } from '@/components/inventory/apply-gauge-zones'

// "Apply gauge zones" on an engine item: applying port's profile, then
// starboard's, must leave the dashboard page holding both tiles. The bug this
// guards is a PATCH built off a stale copy of the page's tile list, which would
// overwrite port's just-added tile the moment starboard applied.

let dashboardPage: { id: string; widgets: { id: string; x: number; y: number; w: number; h: number; gaugeGroup?: { title: string } }[] }
const profiles = [
  { id: 'engine-profile-1', name: 'Test Engine Profile', kind: 'engine', gauges: [{ path_suffix: 'oilPressure', label: 'Oil', display: 'numeric', quantity: 'pressure', unit: 'psi' }] },
]
const fetchMock = vi.fn()

beforeEach(() => {
  dashboardPage = { id: 'page-1', widgets: [] }
  fetchMock.mockReset().mockImplementation((url: string, init?: RequestInit) => {
    const u = String(url)
    const method = init?.method ?? 'GET'
    if (u.includes('/api/equipment-profiles')) return Promise.resolve({ ok: true, json: async () => ({ profiles, problems: [] }) })
    if (u.endsWith('/api/dashboard-pages')) return Promise.resolve({ ok: true, json: async () => ({ pages: [dashboardPage] }) })
    if (u.match(/\/api\/dashboard-pages\/[^/?]+$/) && method === 'GET') return Promise.resolve({ ok: true, json: async () => dashboardPage })
    if (u.match(/\/api\/dashboard-pages\/[^/?]+$/) && method === 'PATCH') {
      dashboardPage = { ...dashboardPage, ...JSON.parse(String(init?.body)) }
      return Promise.resolve({ ok: true, json: async () => dashboardPage })
    }
    if (u.includes('/api/signalk/paths')) return Promise.resolve({ ok: true, json: async () => ({ paths: [] }) })
    return Promise.resolve({ ok: true, json: async () => ({}) })
  })
  vi.stubGlobal('fetch', fetchMock)
})

describe('Apply gauge zones on an engine item', () => {
  it('applying port then starboard keeps both tiles', async () => {
    const apply = async (source: string) => {
      const { unmount } = render(<ApplyGaugeZones profileId="engine-profile-1" source={source} />)
      fireEvent.click(screen.getByRole('button', { name: 'Apply gauge zones' }))
      const addTile = await screen.findByRole('button', { name: 'Add tile' })
      await waitFor(() => expect(addTile).not.toBeDisabled())
      fireEvent.click(addTile)
      await waitFor(() => expect(screen.queryByRole('button', { name: 'Add tile' })).toBeNull())
      unmount()
    }

    await apply('propulsion.port')
    await waitFor(() => expect(dashboardPage.widgets).toHaveLength(1))
    await apply('propulsion.starboard')
    await waitFor(() => expect(dashboardPage.widgets).toHaveLength(2))

    const titles = dashboardPage.widgets.map((w) => w.gaugeGroup?.title)
    expect(titles).toContain('Port')
    expect(titles).toContain('Starboard')
  })
})

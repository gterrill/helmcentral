import { afterEach, describe, expect, it, vi } from 'vitest'
import { act, render, screen, waitFor } from '@testing-library/react'

import { SaveBarSlot } from '@/components/patterns/save-bar-slot'
import { WatchesRoutePanel } from '@/components/watches/watches-route-panel'
import { resetQueryStore } from '@/lib/api-query'
import { testRouter } from './helpers/router'

const watch = {
  id: 'w1', name: 'Overnight check', kind: 'scheduled', schedule: '0 6 * * *', sections: ['power'],
  thresholds: { alarms_raised: 1, instrument_issues: 1, tank_change_points: 10, pressure_fall_hpa: 3 },
  look_out_for: '', enabled: true, updated_at: '', schedule_sentence: 'Every day at 06:00 boat time', last_run: null, has_finding: false,
}

function stubFetch() {
  const urls: string[] = []
  vi.stubGlobal('fetch', vi.fn(async (url: string) => {
    urls.push(String(url))
    const u = String(url)
    const ok = (data: unknown) => ({ ok: true, status: 200, json: async () => data })
    if (u.includes('/schedule-preview')) return ok({ valid: true, sentence: 'Every day at 06:00 boat time', boat_time_known: true, next: [] })
    if (u.includes('/api/watches/w1')) return ok({ watch, runs: [], has_older: false })
    return ok({ watches: [watch], max: 20, cost_24h: { usd: 0, unknown_runs: 0 }, storage_bytes: 10 })
  }))
  return urls
}

afterEach(() => {
  vi.unstubAllGlobals()
  resetQueryStore()
})

describe('WatchesRoutePanel', () => {
  it('reads the list on the index', async () => {
    const urls = stubFetch()
    const { wrapper } = testRouter('/watches')
    render(<div className="relative"><SaveBarSlot /><WatchesRoutePanel canWrite /></div>, { wrapper })
    expect(await screen.findByText('Overnight check')).toBeInTheDocument()
    expect(urls.some((u) => u.endsWith('/api/watches'))).toBe(true)
  })

  it('does not read or poll the list on a details page', async () => {
    const urls = stubFetch()
    const { wrapper } = testRouter('/watches/w1')
    render(<div className="relative"><SaveBarSlot /><WatchesRoutePanel canWrite /></div>, { wrapper })
    expect(await screen.findByDisplayValue('Overnight check')).toBeInTheDocument()
    await act(async () => { await new Promise((r) => setTimeout(r, 50)) })
    await waitFor(() => expect(urls.some((u) => u.includes('/api/watches/w1'))).toBe(true))
    expect(urls.filter((u) => u.endsWith('/api/watches'))).toEqual([])
  })
})

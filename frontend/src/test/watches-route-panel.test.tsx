import { afterEach, describe, expect, it, vi } from 'vitest'
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'

import { SaveBarSlot } from '@/components/patterns/save-bar-slot'
import { WatchesRoutePanel } from '@/components/watches/watches-route-panel'
import { resetQueryStore } from '@/lib/api-query'
import { testRouter } from './helpers/router'

const watch = {
  id: 'w1', name: 'Overnight check', kind: 'scheduled', schedule: '0 6 * * *', sections: ['power'],
  thresholds: { alarms_raised: 1, instrument_issues: 1, tank_change_points: 10, pressure_fall_hpa: 3 },
  look_out_for: '', enabled: true, notify: [], send: 'findings', updated_at: '', schedule_sentence: 'Every day at 06:00 boat time', last_run: null, has_finding: false,
}

function stubFetch() {
  const urls: string[] = []
  vi.stubGlobal('fetch', vi.fn(async (url: string) => {
    urls.push(String(url))
    const u = String(url)
    const ok = (data: unknown) => ({ ok: true, status: 200, json: async () => data })
    if (u.endsWith('/api/watches/channels')) return ok({ offered: ['ntfy', 'smtp', 'webhook', 'webpush'], enabled: [] })
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

  it('reads the list on a details page too, for the arrows', async () => {
    const urls = stubFetch()
    const { wrapper } = testRouter('/watches/w1')
    render(<div className="relative"><SaveBarSlot /><WatchesRoutePanel canWrite /></div>, { wrapper })
    expect(await screen.findByDisplayValue('Overnight check')).toBeInTheDocument()
    await waitFor(() => expect(urls.some((u) => u.includes('/api/watches/w1'))).toBe(true))
    await waitFor(() => expect(urls.filter((u) => u.endsWith('/api/watches')).length).toBeGreaterThan(0))
  })

  it('does not read the list for a watch that is not saved yet', async () => {
    const urls = stubFetch()
    const { wrapper, router } = testRouter('/watches')
    router.navigate({ panel: 'watches', route: { view: 'new', template: null } })
    render(<div className="relative"><SaveBarSlot /><WatchesRoutePanel canWrite /></div>, { wrapper })
    await screen.findByLabelText('Name')
    await act(async () => { await new Promise((r) => setTimeout(r, 30)) })
    expect(urls.filter((u) => u.endsWith('/api/watches'))).toEqual([])
    expect(screen.queryByRole('button', { name: /^Next/ })).not.toBeInTheDocument()
  })

  describe('the arrows', () => {
    const second = { ...watch, id: 'w2', name: 'Anchor check', schedule_sentence: 'Hourly' }
    const third = { ...watch, id: 'w3', name: 'Zed', schedule_sentence: 'Daily' }

    function stubTwoMore() {
      vi.stubGlobal('fetch', vi.fn(async (url: string) => {
        const u = String(url)
        const ok = (data: unknown) => ({ ok: true, status: 200, json: async () => data })
        if (u.endsWith('/api/watches/channels')) return ok({ offered: [], enabled: [] })
        if (u.includes('/schedule-preview')) return ok({ valid: true, sentence: '', boat_time_known: true, next: [] })
        const one = [watch, second, third].find((w) => u.includes(`/api/watches/${w.id}`))
        if (one) return ok({ watch: one, runs: [], has_older: false })
        return ok({ watches: [watch, second, third], max: 20, cost_24h: { usd: 0, unknown_runs: 0 }, storage_bytes: 10 })
      }))
    }

    it('step through the watches in the index order, by name unless the index was sorted', async () => {
      stubTwoMore()
      const { wrapper, history } = testRouter('/watches/w1')
      render(<div className="relative"><SaveBarSlot /><WatchesRoutePanel canWrite /></div>, { wrapper })
      // Anchor check, Overnight check, Zed.
      fireEvent.click(await screen.findByRole('button', { name: 'Next: Zed' }))
      await waitFor(() => expect(history.path()).toBe('/watches/w3'))
    })

    it('send Back to the index in the same sort', async () => {
      stubTwoMore()
      const { wrapper, history } = testRouter('/watches/w1?sort=-name')
      render(<div className="relative"><SaveBarSlot /><WatchesRoutePanel canWrite /></div>, { wrapper })
      fireEvent.click(await screen.findByRole('link', { name: 'Watches' }))
      await waitFor(() => expect(history.path()).toBe('/watches?sort=-name'))
    })

    it('follow the sort in the address, and keep it when they step', async () => {
      stubTwoMore()
      const { wrapper, history } = testRouter('/watches/w1?sort=-name')
      render(<div className="relative"><SaveBarSlot /><WatchesRoutePanel canWrite /></div>, { wrapper })
      // Zed, Overnight check, Anchor check.
      fireEvent.click(await screen.findByRole('button', { name: 'Next: Anchor check' }))
      await waitFor(() => expect(history.path()).toBe('/watches/w2?sort=-name'))
    })
  })
})

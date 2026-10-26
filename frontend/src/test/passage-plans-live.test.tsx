import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { PassagePlans } from '@/components/passages/passage-plans'
import { usePassageDetail } from '@/hooks/use-passages'
import { decodePassageDetailExtras } from '@/lib/passage-plans'
import { resetQueryStore } from '@/lib/api-query'
import { stubApi } from './helpers/stub-api'
import { testRouter } from './helpers/router'

const toast = vi.hoisted(() => ({ error: vi.fn(), success: vi.fn() }))
vi.mock('sonner', () => ({ toast }))

const stats = {
  distance_nm: 1, underway_seconds: 60, mean_sog_kts: null, max_sog_kts: null, fuel_burned_l: null, tws_mean_kts: null,
  max_gust_kts: null, soc_start_pct: null, soc_end_pct: null, soc_min_pct: null,
}
const detail = (over: Record<string, unknown> = {}) => ({
  passage: {
    id: 'p1', start: '2026-10-23T00:00:00Z', end: '2026-10-23T03:00:00Z', from_name: '', to_name: '', title: '', notes: '', hidden: false,
    edited: false, cruise_id: null, created_at: '', start_lon: 149, stats, track: [], track_times: [], replay_track: [], replay_times: [],
    rating: '',
    planned_with_mate: [{
      decision: { id: 7, state: 'stayed', row_date: null, inferred: false, passage_id: 'p1' },
      legs: [], row_dates: ['2026-10-23', '2026-10-24'], row_verdict: null, timezone: 'Australia/Brisbane', conversation_id: 'c-1', chat_exists: true,
    }],
    ...over,
  },
})

function Page() {
  const { passage, error } = usePassageDetail('p1')
  if (error) return <p>query failed: {error}</p>
  if (!passage) return <p>loading</p>
  return (
    <div>
      <span data-testid="stats">{passage.stats.distance_nm} NM</span>
      <PassagePlans passageId={passage.id} plans={passage.plannedWithMate} error={passage.plannedWithMateError} canWrite />
    </div>
  )
}

describe('the plan line on a live passage query', () => {
  beforeEach(() => { resetQueryStore(); vi.spyOn(console, 'error').mockImplementation(() => {}) })

  it('still loads the passage when its plans cannot be read', async () => {
    stubApi({ 'GET /api/passages/p1': detail({ planned_with_mate: [{ decision: null }] }) })
    const { wrapper } = testRouter('/passages/p1')
    render(<Page />, { wrapper })
    expect(await screen.findByTestId('stats')).toHaveTextContent('1 NM')
    expect(screen.getByRole('alert')).toHaveTextContent("Couldn't read the Mate plans for this passage.")
  })

  it('shows a corrected choice at once, before the passage is read again', async () => {
    let reads = 0
    stubApi({
      'GET /api/passages/p1': () => (++reads === 1 ? detail() : new Promise(() => {})),
      'PUT /api/passage-decisions/7': () => ({ decision: { id: 7, state: 'went', row_date: '2026-10-24', inferred: false, passage_id: 'p1' } }),
    })
    const { wrapper } = testRouter('/passages/p1')
    render(<Page />, { wrapper })
    const trigger = await screen.findByRole('combobox')
    expect(trigger).toHaveTextContent('Stayed')
    fireEvent.click(trigger)
    const option = await screen.findByRole('option', { name: 'Went · Sat 24 Oct' })
    fireEvent.pointerDown(option)
    fireEvent.pointerUp(option)
    fireEvent.click(option)
    await waitFor(() => expect(screen.getByRole('combobox')).toHaveTextContent('Went · Sat 24 Oct'))
    expect(reads).toBeGreaterThanOrEqual(1)
  })

  it('saves a choice when the passage detail is not cached, and reads the passage again', async () => {
    const mock = stubApi({
      'PUT /api/passage-decisions/7': () => ({ decision: { id: 7, state: 'went', row_date: '2026-10-24', inferred: false, passage_id: 'p1' } }),
    })
    const { wrapper } = testRouter('/passages/p1')
    // The plans are shown from props, with nothing in the cache under the passage's key.
    render(<PassagePlans passageId="p1" plans={decodePassageDetailExtras(detail().passage).plannedWithMate} error={null} canWrite />, { wrapper })
    const trigger = await screen.findByRole('combobox')
    fireEvent.click(trigger)
    const option = await screen.findByRole('option', { name: 'Went · Sat 24 Oct' })
    fireEvent.pointerDown(option)
    fireEvent.pointerUp(option)
    fireEvent.click(option)
    await waitFor(() => expect(mock.mock.calls.some(([, i]) => i?.method === 'PUT')).toBe(true))
    await new Promise((r) => setTimeout(r, 20))
    expect(toast.error).not.toHaveBeenCalled()
  })
})

import { fireEvent, render, screen } from '@testing-library/react'
import { useEffect } from 'react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { PassageDetailsPage } from '@/components/passages/passage-details-page'
import type { PassageView } from '@/lib/passages'
import { testRouter } from './helpers/router'

const seek = vi.hoisted(() => vi.fn())
const hooks = vi.hoisted(() => ({ passage: null as unknown }))

vi.mock('@/components/passages/replay-panel', () => ({
  ReplayPanel: ({ seekRef }: { seekRef?: { current: ((at: number) => void) | null } }) => {
    useEffect(() => { if (seekRef) seekRef.current = seek })
    return <div data-testid="replay" />
  },
}))
vi.mock('@/components/passages/passage-export-menu', () => ({ PassageExportMenu: () => null }))
vi.mock('@/hooks/use-passages', () => ({
  usePassageDetail: () => ({ passage: hooks.passage, loading: false, error: null }),
  useCruiseDetails: vi.fn(),
  usePassageDetails: vi.fn(),
}))
vi.mock('@/lib/passages-api', () => ({ passagesApi: {} }))
vi.mock('sonner', () => ({ toast: { error: vi.fn(), success: vi.fn() } }))

const passage: PassageView = {
  id: 'p1', start: '2026-11-16T19:05:00Z', end: '2026-11-17T01:44:00Z', from_name: 'Rivergate Marina', to_name: 'The Boatworks',
  title: '', notes: '', hidden: false, edited: false, cruise_id: null, created_at: '', timezone: 'Australia/Brisbane',
  stats: {
    distance_nm: 10, underway_seconds: 7200, mean_sog_kts: 5, max_sog_kts: 7, max_sog_at: 1_700_000_100, fuel_burned_l: 44, tws_mean_kts: 11.5,
    max_gust_kts: 24.4, max_gust_at: 1_700_000_200, soc_start_pct: 90, soc_end_pct: 70, soc_min_pct: 68,
  },
  track: [], track_times: [], replay_track: [], replay_times: [],
  rating: '', ratingError: null, plannedWithMate: [], plannedWithMateError: null,
}

function page() {
  const { wrapper } = testRouter('/passages/p1')
  render(
    <PassageDetailsPage
      units="metric" mode={{ kind: 'passage', id: 'p1' }} canWrite isDarkTheme={false} list={[]}
      filter={{ kind: 'all' }} cruises={[]} onBack={vi.fn()} onOpenPassage={vi.fn()} onOpenCruise={vi.fn()}
    />,
    { wrapper },
  )
}

beforeEach(() => { vi.clearAllMocks(); hooks.passage = passage })

describe('passage page', () => {
  it('moves the replay when a maximum is chosen', () => {
    page()
    fireEvent.click(screen.getByRole('button', { name: 'Show max gust 24.4 kn on the replay' }))
    expect(seek).toHaveBeenCalledWith(1_700_000_200)
  })

  it('moves the replay from the motion line too', () => {
    hooks.passage = { ...passage, stats: { ...passage.stats, max_roll_deg: 14, max_roll_at: 1_700_000_300 } }
    page()
    fireEvent.click(screen.getByRole('button', { name: 'Show max roll 14° on the replay' }))
    expect(seek).toHaveBeenCalledWith(1_700_000_300)
  })

  it('keeps the route in the second line when the heading is a title of the operator\'s own', () => {
    hooks.passage = { ...passage, title: 'Delivery run' }
    page()
    expect(screen.getByText('Rivergate Marina → The Boatworks · Tue 17 Nov, 5.05 AM to 11.44 AM')).toBeInTheDocument()
  })

  it('names the route once, then gives the day and the time span', () => {
    page()
    expect(screen.getAllByText('Rivergate Marina → The Boatworks')).toHaveLength(1)
    expect(screen.getByText('Tue 17 Nov, 5.05 AM to 11.44 AM')).toBeInTheDocument()
  })
})

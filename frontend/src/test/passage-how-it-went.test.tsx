import { fireEvent, render, screen } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { PassageEditPanel } from '@/components/passages/passage-edit-panel'
import type { PassageView } from '@/lib/passages'
import { testRouter } from './helpers/router'

const api = vi.hoisted(() => ({ patch: vi.fn() }))
vi.mock('@/lib/passages-api', () => ({ passagesApi: api }))
vi.mock('sonner', () => ({ toast: { error: vi.fn(), success: vi.fn() } }))

const passage: PassageView = {
  id: 'p1', start: '2026-03-13T23:30:00Z', end: '2026-03-14T02:00:00Z', from_name: 'Airlie', to_name: 'Hamilton',
  title: '', notes: '', hidden: false, edited: false, cruise_id: null, created_at: '', timezone: 'Australia/Brisbane',
  stats: {
    distance_nm: 1, underway_seconds: 60, mean_sog_kts: null, max_sog_kts: null, fuel_burned_l: null, tws_mean_kts: null,
    max_gust_kts: null, soc_start_pct: null, soc_end_pct: null, soc_min_pct: null,
    max_roll_deg: 14.2, max_roll_at: 1_700_000_300,
    max_pitch_deg: 6.1, max_pitch_at: 1_700_000_400,
    max_heave_m: 0.84, max_heave_at: 1_700_000_500,
  },
  track: [], track_times: [], replay_track: [], replay_times: [],
  rating: '', ratingError: null, plannedWithMate: [], plannedWithMateError: null,
}

function panel(over: Partial<PassageView> = {}, onSeek: (at: number) => void = vi.fn()) {
  const { wrapper } = testRouter('/passages/p1')
  render(
    <PassageEditPanel
      passage={{ ...passage, ...over }} cruises={[]} olderNeighbour={null} newerNeighbour={null}
      cursorRef={{ current: null }} canWrite onMerged={vi.fn()} onHidden={vi.fn()} onSeek={onSeek}
    />,
    { wrapper },
  )
}

beforeEach(() => { vi.clearAllMocks() })

describe('How it went motion line', () => {
  it('shows the largest roll, pitch and heave', () => {
    panel()
    expect(screen.getByTestId('passage-motion')).toHaveTextContent('Max roll 14°, pitch 6°, heave 0.8 m')
  })

  it('moves the replay to each one', () => {
    const onSeek = vi.fn()
    panel({}, onSeek)
    fireEvent.click(screen.getByRole('button', { name: 'Show max roll 14° on the replay' }))
    fireEvent.click(screen.getByRole('button', { name: 'Show max pitch 6° on the replay' }))
    fireEvent.click(screen.getByRole('button', { name: 'Show max heave 0.8 m on the replay' }))
    expect(onSeek.mock.calls).toEqual([[1_700_000_300], [1_700_000_400], [1_700_000_500]])
  })

  it('says motion was not recorded when no point has a reading', () => {
    panel({ stats: { ...passage.stats, max_roll_deg: null, max_pitch_deg: null, max_heave_m: null, max_roll_at: null, max_pitch_at: null, max_heave_at: null } })
    expect(screen.getByText('Motion not recorded for this passage')).toBeInTheDocument()
    expect(screen.queryByTestId('passage-motion')).not.toBeInTheDocument()
  })

  it('says the same for a passage from a server that sends no motion at all', () => {
    const old = { ...passage.stats }
    for (const key of ['max_roll_deg', 'max_roll_at', 'max_pitch_deg', 'max_pitch_at', 'max_heave_m', 'max_heave_at'] as const) delete old[key]
    panel({ stats: old })
    expect(screen.getByText('Motion not recorded for this passage')).toBeInTheDocument()
  })

  it('lists only the figures that were recorded', () => {
    panel({ stats: { ...passage.stats, max_pitch_deg: null, max_pitch_at: null } })
    expect(screen.getByTestId('passage-motion')).toHaveTextContent('Max roll 14°, heave 0.8 m')
  })

  it('sits above the score chips', () => {
    panel()
    const motion = screen.getByTestId('passage-motion')
    const chip = screen.getByRole('button', { name: 'Comfortable' })
    expect(motion.compareDocumentPosition(chip) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
  })
})

describe('How it went chips', () => {
  it('fills the chosen chip and marks it pressed', () => {
    panel({ rating: 'lively' })
    const chosen = screen.getByRole('button', { name: 'Lively' })
    expect(chosen).toHaveAttribute('aria-pressed', 'true')
    expect(chosen.className).toContain('bg-primary')
    const other = screen.getByRole('button', { name: 'Comfortable' })
    expect(other).toHaveAttribute('aria-pressed', 'false')
    expect(other.className).not.toContain('bg-primary ')
  })
})

import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { PassagePlans } from '@/components/passages/passage-plans'
import { PassageEditPanel } from '@/components/passages/passage-edit-panel'
import { decodePassageDetailExtras, type PassagePlan } from '@/lib/passage-plans'
import type { PassageView } from '@/lib/passages'
import { testRouter } from './helpers/router'

const api = vi.hoisted(() => ({ patch: vi.fn() }))
vi.mock('@/lib/passages-api', () => ({ passagesApi: api, PASSAGES_KEY: ['passages'] }))

const toastError = vi.hoisted(() => vi.fn())
vi.mock('sonner', () => ({ toast: { error: toastError, success: vi.fn() } }))

function plan(over: Partial<PassagePlan> = {}): PassagePlan {
  return {
    decision: { id: 7, state: 'went', rowDate: '2026-10-23', inferred: true, passageId: 'p1' },
    rowDates: ['2026-10-23', '2026-10-24'],
    rowVerdict: 'go',
    conversationId: 'c-1',
    chatExists: true,
    ...over,
  }
}

describe('PassagePlans', () => {
  beforeEach(() => { vi.clearAllMocks() })

  it('says what Mate planned, the day, the verdict and that it was inferred', () => {
    const { wrapper } = testRouter('/passages/p1')
    render(<PassagePlans passageId="p1" plans={[plan()]} error={null} canWrite={false} />, { wrapper })
    expect(screen.getByText(/Planned with Mate/)).toBeInTheDocument()
    expect(screen.getByText(/^Planned with Mate · Fri 23 Oct/)).toBeInTheDocument()
    expect(screen.getByText('GO')).toBeInTheDocument()
    expect(screen.getByText('Went · Fri 23 Oct (inferred)')).toBeInTheDocument()
  })

  it('offers Open chat only when the chat still exists', () => {
    const { wrapper, router } = testRouter('/passages/p1')
    const { rerender } = render(<PassagePlans passageId="p1" plans={[plan()]} error={null} canWrite={false} />, { wrapper })
    fireEvent.click(screen.getByRole('button', { name: 'Open chat' }))
    expect(router.getSnapshot().location).toEqual({ panel: 'assistant', conversationId: 'c-1' })
    rerender(<PassagePlans passageId="p1" plans={[plan({ chatExists: false })]} error={null} canWrite={false} />)
    expect(screen.queryByRole('button', { name: 'Open chat' })).not.toBeInTheDocument()
  })

  it('shows the error when the plans could not be read', () => {
    const { wrapper } = testRouter('/passages/p1')
    render(<PassagePlans passageId="p1" plans={null} error="Couldn't read the Mate plans for this passage." canWrite={false} />, { wrapper })
    expect(screen.getByRole('alert')).toHaveTextContent("Couldn't read the Mate plans for this passage.")
  })

  it('shows nothing when the passage was never planned', () => {
    const { wrapper } = testRouter('/passages/p1')
    const { container } = render(<PassagePlans passageId="p1" plans={[]} error={null} canWrite={true} />, { wrapper })
    expect(container).toBeEmptyDOMElement()
  })
})

describe('decodePassageDetailExtras', () => {
  const raw = (over: Record<string, unknown> = {}) => ({
    rating: '',
    planned_with_mate: [{
      decision: { id: 7, state: 'went', row_date: '2026-10-23', inferred: true, passage_id: 'p1' },
      legs: [{ name: 'Airlie', stay_nights_after: 2 }], row_dates: ['2026-10-23', '2026-10-24'], row_verdict: 'go',
      timezone: 'Australia/Brisbane', conversation_id: 'c-1', chat_exists: true,
    }],
    ...over,
  })
  beforeEach(() => { vi.spyOn(console, 'error').mockImplementation(() => {}) })

  it('reads the plans and the rating', () => {
    const ok = decodePassageDetailExtras(raw({ rating: 'lively' }))
    expect(ok.rating).toBe('lively')
    expect(ok.plannedWithMate?.[0]).toMatchObject({ rowVerdict: 'go', chatExists: true, conversationId: 'c-1', rowDates: ['2026-10-23', '2026-10-24'] })
  })

  it('turns a server error into a plain sentence and keeps the detail in the console', () => {
    const failed = decodePassageDetailExtras(raw({ planned_with_mate: null, planned_with_mate_error: 'assistant store is not running' }))
    expect(failed.plannedWithMate).toBeNull()
    expect(failed.plannedWithMateError).toBe("Couldn't read the Mate plans for this passage.")
    expect(console.error).toHaveBeenCalledWith(expect.stringContaining('assistant store is not running'))
  })

  it('turns an unreadable plan into the same sentence without throwing', () => {
    const failed = decodePassageDetailExtras(raw({ planned_with_mate: [{ decision: null }] }))
    expect(failed.plannedWithMate).toBeNull()
    expect(failed.plannedWithMateError).toBe("Couldn't read the Mate plans for this passage.")
    expect(failed.rating).toBe('')
  })

  it('gives an unknown rating its own error and leaves the plans alone', () => {
    const r = decodePassageDetailExtras(raw({ rating: 'great' }))
    expect(r.rating).toBe('')
    expect(r.ratingError).toBe("Couldn't read how this passage went.")
    expect(r.plannedWithMate).toHaveLength(1)
  })
})

const passage: PassageView = {
  id: 'p1', start: '2026-03-13T23:30:00Z', end: '2026-03-14T02:00:00Z', from_name: 'Airlie', to_name: 'Hamilton',
  title: '', notes: '', hidden: false, edited: false, cruise_id: null, created_at: '', timezone: 'Australia/Brisbane',
  stats: {
    distance_nm: 1, underway_seconds: 60, mean_sog_kts: null, max_sog_kts: null, fuel_burned_l: null, tws_mean_kts: null,
    max_gust_kts: null, soc_start_pct: null, soc_end_pct: null, soc_min_pct: null,
  },
  track: [], track_times: [], replay_track: [], replay_times: [],
  rating: '', ratingError: null, plannedWithMate: [], plannedWithMateError: null,
}

function panel(over: Partial<PassageView> = {}, canWrite = true) {
  const { wrapper, router } = testRouter('/passages/p1')
  render(
    <PassageEditPanel
      passage={{ ...passage, ...over }} cruises={[]} olderNeighbour={null} newerNeighbour={null}
      cursorRef={{ current: null }} canWrite={canWrite} onMerged={vi.fn()} onHidden={vi.fn()}
    />,
    { wrapper },
  )
  return router
}

describe('How it went', () => {
  beforeEach(() => { vi.clearAllMocks() })

  it('saves a rating as soon as it is chosen', async () => {
    api.patch.mockResolvedValue({})
    panel()
    expect(screen.getByText('How it went')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Lively' }))
    await waitFor(() => expect(api.patch).toHaveBeenCalledWith('p1', { rating: 'lively' }))
  })

  it('clears the rating when the chosen one is clicked again', async () => {
    api.patch.mockResolvedValue({})
    panel({ rating: 'comfortable' })
    expect(screen.getByRole('button', { name: 'Comfortable' })).toHaveAttribute('aria-pressed', 'true')
    fireEvent.click(screen.getByRole('button', { name: 'Comfortable' }))
    await waitFor(() => expect(api.patch).toHaveBeenCalledWith('p1', { rating: '' }))
  })

  it('shows the server\'s words in a toast and changes nothing on a refusal', async () => {
    api.patch.mockRejectedValue(new Error('rating must be one of comfortable, lively, shouldnt_have_gone'))
    panel()
    fireEvent.click(screen.getByRole('button', { name: "Shouldn't have gone" }))
    await waitFor(() => expect(toastError).toHaveBeenCalledWith('Could not save rating', {
      description: 'rating must be one of comfortable, lively, shouldnt_have_gone',
    }))
    expect(screen.getByRole('button', { name: "Shouldn't have gone" })).toHaveAttribute('aria-pressed', 'false')
  })

  it('is not part of the unsaved state', async () => {
    api.patch.mockResolvedValue({})
    const router = panel()
    await act(async () => { fireEvent.click(screen.getByRole('button', { name: 'Lively' })) })
    expect(router.hasGuards()).toBe(false)
    expect(screen.getByRole('button', { name: 'Save' })).toBeDisabled()
  })

  it('ignores a second press while a save is in flight and locks the buttons', async () => {
    let finish: (v: unknown) => void = () => {}
    api.patch.mockReturnValue(new Promise((r) => { finish = r }))
    panel()
    fireEvent.click(screen.getByRole('button', { name: 'Lively' }))
    await waitFor(() => expect(screen.getByRole('button', { name: 'Comfortable' })).toBeDisabled())
    fireEvent.click(screen.getByRole('button', { name: 'Comfortable' }))
    expect(api.patch).toHaveBeenCalledTimes(1)
    await act(async () => { finish({}) })
    await waitFor(() => expect(screen.getByRole('button', { name: 'Comfortable' })).toBeEnabled())
  })

  it('says so near the rating when the stored rating could not be read', () => {
    panel({ ratingError: "Couldn't read how this passage went." })
    expect(screen.getByRole('alert')).toHaveTextContent("Couldn't read how this passage went.")
  })

  it('is read-only without write access', () => {
    panel({}, false)
    expect(screen.getByRole('button', { name: 'Lively' })).toBeDisabled()
  })
})

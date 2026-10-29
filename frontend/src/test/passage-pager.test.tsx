import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { PassageDetailsPage } from '@/components/passages/passage-details-page'
import type { PassageSummary } from '@/lib/passages'
import { testRouter } from './helpers/router'

vi.mock('@/hooks/use-passages', () => ({
  usePassageDetail: () => ({ passage: null, loading: true, error: null }),
  useCruiseDetails: () => ({ data: null, loading: true, error: null }),
  usePassageDetails: () => ({ passages: [], error: null }),
}))
vi.mock('@/hooks/use-passage-legs', () => ({ usePassageLegs: () => ({ legs: [] }) }))

function p(id: string, title: string, start: string, cruise: string | null = null): PassageSummary {
  return { id, title, start, end: start, from_name: '', to_name: '', cruise_id: cruise, timezone: 'Australia/Brisbane' } as PassageSummary
}

const LIST = [
  p('d', 'Fourth', '2026-04-04T00:00:00Z', 'c1'),
  p('c', 'Third', '2026-03-03T00:00:00Z'),
  p('b', 'Second', '2026-02-02T00:00:00Z', 'c1'),
  p('a', 'First', '2026-01-01T00:00:00Z'),
]

function show(id: string, filter: Parameters<typeof PassageDetailsPage>[0]['filter']) {
  const onOpenPassage = vi.fn()
  const { wrapper } = testRouter(`/passages/${id}`)
  render(
    <PassageDetailsPage
      units="metric" mode={{ kind: 'passage', id }} canWrite isDarkTheme={false} list={LIST} filter={filter} cruises={[]}
      onBack={vi.fn()} onOpenPassage={onOpenPassage} onOpenCruise={vi.fn()}
    />,
    { wrapper },
  )
  return onOpenPassage
}

describe('passage pager', () => {
  it('opens the later and earlier passage, named with their dates', () => {
    const open = show('b', { kind: 'all' })
    fireEvent.click(screen.getByRole('button', { name: 'Next: Third · 3 Mar' }))
    expect(open).toHaveBeenLastCalledWith('c')
    fireEvent.click(screen.getByRole('button', { name: 'Previous: First · 1 Jan' }))
    expect(open).toHaveBeenLastCalledWith('a')
  })

  it('stays inside the cruise it was opened from', () => {
    const open = show('b', { kind: 'cruise', id: 'c1' })
    expect(screen.getByRole('button', { name: 'Previous' })).toBeDisabled()
    fireEvent.click(screen.getByRole('button', { name: 'Next: Fourth · 4 Apr' }))
    expect(open).toHaveBeenLastCalledWith('d')
  })
})

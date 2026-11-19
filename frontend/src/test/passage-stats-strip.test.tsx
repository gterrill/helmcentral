import { fireEvent, render, screen, within } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { PassageStatsStrip } from '@/components/passages/passage-stats'
import type { PassageStats } from '@/lib/passages'

const stats: PassageStats = {
  distance_nm: 10,
  underway_seconds: 7200,
  mean_sog_kts: 5.2,
  max_sog_kts: 7.1,
  max_sog_at: 1_700_000_100,
  fuel_burned_l: 44,
  tws_mean_kts: 11.5,
  max_gust_kts: 24.4,
  max_gust_at: 1_700_000_200,
  soc_start_pct: 90,
  soc_end_pct: 70,
  soc_min_pct: 68,
}

describe('PassageStatsStrip maxima', () => {
  it('moves the replay to the gust', () => {
    const onSeek = vi.fn()
    render(<PassageStatsStrip stats={stats} units="metric" onSeek={onSeek} />)
    fireEvent.click(screen.getByRole('button', { name: 'Show max gust 24.4 kn on the replay' }))
    expect(onSeek).toHaveBeenCalledWith(1_700_000_200)
  })

  it('moves the replay to the top speed', () => {
    const onSeek = vi.fn()
    render(<PassageStatsStrip stats={stats} units="metric" onSeek={onSeek} />)
    fireEvent.click(screen.getByRole('button', { name: 'Show max speed 7.1 kn on the replay' }))
    expect(onSeek).toHaveBeenCalledWith(1_700_000_100)
  })

  it('is plain text when the time of the maximum is not known', () => {
    render(<PassageStatsStrip stats={{ ...stats, max_gust_at: null }} units="metric" onSeek={vi.fn()} />)
    expect(screen.queryByRole('button', { name: /max gust/ })).not.toBeInTheDocument()
    expect(screen.getByText('Gust 24.4 kn')).toBeInTheDocument()
  })

  it('is plain text with no replay to move', () => {
    render(<PassageStatsStrip stats={stats} units="metric" />)
    expect(screen.queryByRole('button')).not.toBeInTheDocument()
  })
})

describe('PassageStatsStrip tiles', () => {
  it('says that the wind figure is the mean', () => {
    render(<PassageStatsStrip stats={stats} units="metric" />)
    const tile = screen.getByText('Mean wind').parentElement!
    expect(within(tile).getByText('11.5 kn')).toBeInTheDocument()
  })

  it('writes distance with a lower-case unit, like kn', () => {
    render(<PassageStatsStrip stats={stats} units="metric" />)
    expect(screen.getByText('10.0 nm')).toBeInTheDocument()
  })

  it('shows fuel per mile under the total', () => {
    render(<PassageStatsStrip stats={stats} units="metric" />)
    expect(screen.getByText('4.4 L/nm')).toBeInTheDocument()
  })

  it('shows fuel per mile in gallons when the units are imperial', () => {
    render(<PassageStatsStrip stats={stats} units="imperial" />)
    expect(screen.getByText(/gal\/nm$/)).toBeInTheDocument()
  })

  it('leaves the rate out when fuel or distance is missing', () => {
    const { rerender } = render(<PassageStatsStrip stats={{ ...stats, fuel_burned_l: null }} units="metric" />)
    expect(screen.queryByText(/\/nm/)).not.toBeInTheDocument()
    rerender(<PassageStatsStrip stats={{ ...stats, distance_nm: 0 }} units="metric" />)
    expect(screen.queryByText(/\/nm/)).not.toBeInTheDocument()
  })

  it('drops the battery tile when the bank barely moved', () => {
    render(<PassageStatsStrip stats={{ ...stats, soc_start_pct: 90, soc_min_pct: 86, soc_end_pct: 88 }} units="metric" />)
    expect(screen.queryByText('House battery')).not.toBeInTheDocument()
    expect(screen.getByTestId('passage-stats')).toHaveAttribute('data-tiles', '5')
  })

  it('keeps the battery tile when the bank charged by five points or more', () => {
    render(<PassageStatsStrip stats={{ ...stats, soc_start_pct: 60, soc_min_pct: 60, soc_end_pct: 100 }} units="metric" />)
    expect(screen.getByText('House battery')).toBeInTheDocument()
  })

  it('keeps the battery tile when the low is five points or more under the start', () => {
    render(<PassageStatsStrip stats={{ ...stats, soc_start_pct: 90, soc_min_pct: 85 }} units="metric" />)
    expect(screen.getByText('House battery')).toBeInTheDocument()
    expect(screen.getByTestId('passage-stats')).toHaveAttribute('data-tiles', '6')
  })

  it('keeps the battery tile, showing --, when there is no reading to judge by', () => {
    render(<PassageStatsStrip stats={{ ...stats, soc_start_pct: null, soc_min_pct: null, soc_end_pct: null }} units="metric" />)
    expect(screen.getByText('House battery')).toBeInTheDocument()
  })
})

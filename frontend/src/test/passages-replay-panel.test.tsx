import { act, fireEvent, render, screen } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { ReplayPanel, type ReplayCursor } from '@/components/passages/replay-panel'
import type { PassageDetail } from '@/lib/passages'

let introDone: (() => void) | null = null
let lastBoat: { lat: number; lon: number } | null = null
vi.mock('@/components/passages/passage-map', () => ({
  passageColour: () => 'x',
  PassageMap: (p: { onIntroDone: () => void; boat: { lat: number; lon: number } | null }) => {
    introDone = p.onIntroDone
    lastBoat = p.boat
    return <div data-testid="map" />
  },
  InsetMap: () => <div data-testid="inset" />,
}))

const start = Date.UTC(2026, 2, 13, 23, 0) / 1000
const passage: PassageDetail = {
  id: 'p1', start: new Date(start * 1000).toISOString(), end: new Date((start + 1000) * 1000).toISOString(),
  from_name: '', to_name: '', title: '', notes: '', hidden: false, edited: false, cruise_id: null, created_at: '',
  stats: {
    distance_nm: 0, underway_seconds: 0, mean_sog_kts: null, max_sog_kts: null, fuel_burned_l: null, tws_mean_kts: null,
    max_gust_kts: null, soc_start_pct: null, soc_end_pct: null, soc_min_pct: null,
  },
  start_lon: 149,
  track: [[[0, 149], [0, 150]]],
  track_times: [[start, start + 1000]],
}

beforeEach(() => { localStorage.clear(); lastBoat = null })

describe('ReplayPanel', () => {
  it('scrubbing moves the boat and the boat-local readout, and reports the cursor', () => {
    const cursorRef = { current: null as ReplayCursor | null }
    render(<ReplayPanel passages={[passage]} isDarkTheme={false} cursorRef={cursorRef} />)
    expect(screen.getByTestId('replay-readout')).toHaveTextContent('14 Mar 09:00 UTC+10')
    fireEvent.change(screen.getByLabelText('Replay position'), { target: { value: '500' } })
    expect(lastBoat).toEqual({ lat: 0, lon: 149.5 })
    expect(cursorRef.current).toEqual({ id: 'p1', at: start + 500 })
  })

  it('shows the overview map only after the zoom-in, when switched on, and remembers the choice', () => {
    render(<ReplayPanel passages={[passage]} isDarkTheme={false} />)
    fireEvent.click(screen.getByRole('button', { name: /Overview map/ }))
    expect(localStorage.getItem('passages.insetMap')).toBe('1')
    expect(screen.queryByTestId('inset')).not.toBeInTheDocument()
    act(() => introDone?.())
    expect(screen.getByTestId('inset')).toBeInTheDocument()
  })

  it('says so when no track was kept rather than drawing nothing', () => {
    render(<ReplayPanel passages={[{ ...passage, track: [], track_times: [] }]} isDarkTheme={false} />)
    expect(screen.getByTestId('passage-no-track')).toBeInTheDocument()
  })
})

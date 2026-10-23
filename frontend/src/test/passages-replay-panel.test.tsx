import { fireEvent, render, screen } from '@testing-library/react'
import { forwardRef, useImperativeHandle } from 'react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { REPLAY_SPEEDS } from '@/hooks/use-replay'

import { ReplayPanel, type ReplayCursor } from '@/components/passages/replay-panel'
import type { PassageDetail } from '@/lib/passages'

vi.mock('@/hooks/use-boat-hull', () => ({ useBoatHull: () => null }))

const flyIn = vi.fn(() => Promise.resolve())
let lastBoat: { lat: number; lon: number } | null = null
vi.mock('@/components/passages/passage-map', () => ({
  passageColour: () => 'x',
  PassageMap: forwardRef(function PassageMapMock(p: { boat: { lat: number; lon: number } | null; overlay?: React.ReactNode }, ref) {
    useImperativeHandle(ref, () => ({ flyIn }))
    lastBoat = p.boat
    return <div data-testid="map">{p.overlay}</div>
  }),
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
  // A stop in the middle: the boat is at 149.2 from a quarter to three quarters of the way.
  replay_track: [[[0, 149], [0, 149.2], [0, 149.2], [0, 150]]],
  replay_times: [[start, start + 250, start + 750, start + 1000]],
}

beforeEach(() => { localStorage.clear(); lastBoat = null; flyIn.mockClear() })

describe('ReplayPanel', () => {
  it('scrubbing moves the boat and the boat-local readout, and reports the cursor', () => {
    const cursorRef = { current: null as ReplayCursor | null }
    render(<ReplayPanel passages={[passage]} isDarkTheme={false} cursorRef={cursorRef} />)
    expect(screen.getByTestId('replay-readout')).toHaveTextContent('14 Mar 09:00 UTC+10')
    fireEvent.change(screen.getByLabelText('Replay position'), { target: { value: '500' } })
    // The straight display line would put her at 149.5; the replay track shows her stopped.
    expect(lastBoat).toEqual({ lat: 0, lon: 149.2 })
    expect(cursorRef.current).toEqual({ id: 'p1', at: start + 500 })
  })

  it('frames the track on load without the zoom-in', () => {
    render(<ReplayPanel passages={[passage]} isDarkTheme={false} />)
    expect(flyIn).not.toHaveBeenCalled()
  })

  it('zooms in when Play is pressed at the start, then plays and shows the overview map', async () => {
    render(<ReplayPanel passages={[passage]} isDarkTheme={false} />)
    fireEvent.click(screen.getByRole('button', { name: /Overview map/ }))
    expect(localStorage.getItem('passages.insetMap')).toBe('1')
    fireEvent.click(screen.getByRole('button', { name: 'Play replay' }))
    expect(flyIn).toHaveBeenCalledTimes(1)
    expect(await screen.findByRole('button', { name: 'Pause replay' })).toBeInTheDocument()
    expect(screen.getByTestId('inset')).toBeInTheDocument()
  })

  it('does not zoom when resuming after a pause or after scrubbing mid-replay', async () => {
    render(<ReplayPanel passages={[passage]} isDarkTheme={false} />)
    fireEvent.change(screen.getByLabelText('Replay position'), { target: { value: '500' } })
    fireEvent.click(screen.getByRole('button', { name: 'Play replay' }))
    expect(await screen.findByRole('button', { name: 'Pause replay' })).toBeInTheDocument()
    expect(flyIn).not.toHaveBeenCalled()
  })

  it('plays the whole track in 30, 15 or 8 seconds', () => {
    expect(REPLAY_SPEEDS.map((s) => [s.label, s.seconds])).toEqual([['Slow', 30], ['Normal', 15], ['Fast', 8]])
  })

  it('says so when no track was kept rather than drawing nothing', () => {
    render(<ReplayPanel passages={[{ ...passage, track: [], track_times: [], replay_track: [], replay_times: [] }]} isDarkTheme={false} />)
    expect(screen.getByTestId('passage-no-track')).toBeInTheDocument()
  })
})

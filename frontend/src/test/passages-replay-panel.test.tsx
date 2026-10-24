import { fireEvent, render, screen } from '@testing-library/react'
import { forwardRef, useImperativeHandle } from 'react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { REPLAY_SPEEDS } from '@/hooks/use-replay'

import { ReplayPanel, type ReplayCursor } from '@/components/passages/replay-panel'
import type { PassageDetail } from '@/lib/passages'
import { saveInsetPreference } from '@/lib/passage-inset-preference'
import { stubApi, type StubRequest } from './helpers/stub-api'
import { waitFor } from '@testing-library/react'

vi.mock('@/hooks/use-boat-hull', () => ({ useBoatHull: () => null }))

const flyIn = vi.fn(() => Promise.resolve())
let lastBoat: { lat: number; lon: number } | null = null
vi.mock('@/components/passages/passage-map', () => ({
  passageColour: () => 'x',
  PassageMap: forwardRef(function PassageMapMock(p: { boat: { lat: number; lon: number } | null; inset?: React.ReactNode; insetCorner?: string }, ref) {
    useImperativeHandle(ref, () => ({ flyIn }))
    lastBoat = p.boat
    return <div data-testid="map" data-inset-corner={p.insetCorner}>{p.inset}</div>
  }),
  InsetMap: ({ zoomOffset }: { zoomOffset?: number }) => <div data-testid="inset" data-zoom-offset={zoomOffset} />,
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

const toast = vi.hoisted(() => ({ error: vi.fn() }))
vi.mock('sonner', () => ({ toast }))

let saved = { corner: 'top-right', zoom_offset: 0 }
let puts: unknown[] = []
const insetRoutes = () => ({
  'GET /api/passages/inset-view': () => ({ inset_view: { at_ms: start * 1000, ...saved } }),
  'PUT /api/passages/inset-view': (req: StubRequest) => {
    puts.push(req.body)
    const b = req.body as { corner: string; zoom_offset: number }
    saved = { corner: b.corner, zoom_offset: b.zoom_offset }
    return { inset_view: { at_ms: start * 1000, ...saved } }
  },
})

beforeEach(() => { saveInsetPreference(false); saved = { corner: 'top-right', zoom_offset: 0 }; puts = []; toast.error.mockClear(); stubApi({ 'GET /api/passages/camera-shots': { shots: [] }, 'GET /api/passages/signs': { signs: [] }, ...insetRoutes() }); localStorage.clear(); lastBoat = null; flyIn.mockClear() })

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

  it('plays the whole track in 60, 30, 15 or 8 seconds', () => {
    expect(REPLAY_SPEEDS.map((s) => [s.label, s.seconds])).toEqual([['60 s', 60], ['Slow', 30], ['Normal', 15], ['Fast', 8]])
  })

  it('says so when no track was kept rather than drawing nothing', () => {
    render(<ReplayPanel passages={[{ ...passage, track: [], track_times: [], replay_track: [], replay_times: [] }]} isDarkTheme={false} />)
    expect(screen.getByTestId('passage-no-track')).toBeInTheDocument()
  })

  describe('the overview map corner and zoom step', () => {
    const turnOn = () => fireEvent.click(screen.getByRole('button', { name: /Overview map/ }))

    it('draws the overview in the corner saved for the passage, at its zoom step', async () => {
      saved = { corner: 'bottom-left', zoom_offset: -2 }
      render(<ReplayPanel passages={[passage]} isDarkTheme={false} />)
      turnOn()
      await waitFor(() => expect(screen.getByTestId('map')).toHaveAttribute('data-inset-corner', 'bottom-left'))
      expect(screen.getByTestId('inset')).toHaveAttribute('data-zoom-offset', '-2')
    })

    it('asks for the setting by the passage start, in milliseconds', async () => {
      const mock = stubApi({ 'GET /api/passages/camera-shots': { shots: [] }, 'GET /api/passages/signs': { signs: [] }, ...insetRoutes() })
      render(<ReplayPanel passages={[passage]} isDarkTheme={false} />)
      await waitFor(() => expect(mock.mock.calls.some(([u]) => String(u) === `/api/passages/inset-view?at=${start * 1000}`)).toBe(true))
    })

    it('saves a new corner for the passage and moves the overview at once', async () => {
      render(<ReplayPanel passages={[passage]} isDarkTheme={false} />)
      turnOn()
      fireEvent.click(await screen.findByRole('button', { name: 'Place overview map' }))
      fireEvent.click(screen.getByRole('button', { name: 'Bottom right' }))
      expect(screen.getByTestId('map')).toHaveAttribute('data-inset-corner', 'bottom-right')
      await waitFor(() => expect(puts).toEqual([{ at_ms: start * 1000, corner: 'bottom-right', zoom_offset: 0 }]))
    })

    it('saves each zoom step, building on the one before it', async () => {
      render(<ReplayPanel passages={[passage]} isDarkTheme={false} />)
      turnOn()
      fireEvent.click(await screen.findByRole('button', { name: 'Place overview map' }))
      fireEvent.click(screen.getByRole('button', { name: 'Closer' }))
      fireEvent.click(screen.getByRole('button', { name: 'Closer' }))
      expect(screen.getByTestId('inset')).toHaveAttribute('data-zoom-offset', '2')
      await waitFor(() => expect(puts).toHaveLength(2))
      expect(puts).toEqual([
        { at_ms: start * 1000, corner: 'top-right', zoom_offset: 1 },
        { at_ms: start * 1000, corner: 'top-right', zoom_offset: 2 },
      ])
    })

    it('says so and goes back to the saved placement when the server refuses', async () => {
      stubApi({
        'GET /api/passages/camera-shots': { shots: [] }, 'GET /api/passages/signs': { signs: [] },
        ...insetRoutes(),
        'PUT /api/passages/inset-view': () => new Response(JSON.stringify({ error: 'zoom_offset must be between -3 and 3' }), { status: 400, headers: { 'Content-Type': 'application/json' } }),
      })
      render(<ReplayPanel passages={[passage]} isDarkTheme={false} />)
      turnOn()
      fireEvent.click(await screen.findByRole('button', { name: 'Place overview map' }))
      fireEvent.click(screen.getByRole('button', { name: 'Top left' }))
      await waitFor(() => expect(toast.error).toHaveBeenCalledWith('Could not save the overview map', { description: 'zoom_offset must be between -3 and 3' }))
      await waitFor(() => expect(screen.getByTestId('map')).toHaveAttribute('data-inset-corner', 'top-right'))
    })

    it('shows no overview, and says why, when the saved placement cannot be read', async () => {
      stubApi({
        'GET /api/passages/camera-shots': { shots: [] }, 'GET /api/passages/signs': { signs: [] },
        'GET /api/passages/inset-view': () => new Response(JSON.stringify({ error: 'boom' }), { status: 500, headers: { 'Content-Type': 'application/json' } }),
      })
      render(<ReplayPanel passages={[passage]} isDarkTheme={false} />)
      turnOn()
      await waitFor(() => expect(toast.error).toHaveBeenCalledWith('Could not read where the overview map goes', expect.anything()))
      expect(screen.queryByTestId('inset')).toBeNull()
    })
  })
})

import { act, fireEvent, render, screen } from '@testing-library/react'
import { forwardRef, useImperativeHandle } from 'react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { REPLAY_SPEEDS } from '@/hooks/use-replay'

import { ReplayPanel, type ReplayCursor } from '@/components/passages/replay-panel'
import type { PassageDetail } from '@/lib/passages'
import { saveInsetPreference } from '@/lib/passage-inset-preference'
import { stubApi, type StubRequest } from './helpers/stub-api'
import { waitFor } from '@testing-library/react'

vi.mock('@/hooks/use-boat-hull', () => ({ useBoatHull: () => null }))

const getFraming = vi.fn(() => ({ overview: { center: [149.5, 0] as [number, number], zoom: 9, pitch: 0, bearing: 0 }, bearing: 0 }))
vi.mock('@/lib/replay-intro', async (importOriginal) => ({ ...(await importOriginal<typeof import('@/lib/replay-intro')>()), INTRO_MS: 50 }))
let lastBoat: { lat: number; lon: number } | null = null
const jumpTo = vi.fn()
const setProjection = vi.fn()
let interrupt: (() => void) | undefined
let introActive = false
let lastLight: { key: string } | null = null
vi.mock('@/components/passages/passage-map', () => ({
  passageColour: () => 'x',
  PassageMap: forwardRef(function PassageMapMock(p: { boat: { lat: number; lon: number } | null; inset?: React.ReactNode; insetCorner?: string; introActive?: boolean; onIntroInterrupt?: () => void; light?: { key: string } | null }, ref) {
    lastLight = p.light ?? null
    useImperativeHandle(ref, () => ({ getFraming, jumpTo, setProjection }))
    interrupt = p.onIntroInterrupt
    introActive = !!p.introActive
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
  timezone: 'Australia/Brisbane',
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
  'GET /api/passages/inset-view': () => ({ inset_view: { from_ms: start * 1000, to_ms: (start + 1000) * 1000, ...saved } }),
  'PUT /api/passages/inset-view': (req: StubRequest) => {
    puts.push(req.body)
    const b = req.body as { corner: string; zoom_offset: number }
    saved = { corner: b.corner, zoom_offset: b.zoom_offset }
    return { inset_view: { from_ms: start * 1000, to_ms: (start + 1000) * 1000, ...saved } }
  },
})

beforeEach(() => { saveInsetPreference(false); saved = { corner: 'top-right', zoom_offset: 0 }; puts = []; toast.error.mockClear(); stubApi({ 'GET /api/passages/camera-shots': { shots: [] }, 'GET /api/passages/signs': { signs: [] }, ...insetRoutes() }); localStorage.clear(); lastBoat = null; getFraming.mockClear(); jumpTo.mockClear(); setProjection.mockClear() })

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

  it('hands the map the light for the time of day at the boat, and changes it as the slider moves', () => {
    render(<ReplayPanel passages={[passage]} isDarkTheme={false} />)
    // 23:00 UTC on 13 March is 09:00 the next morning in the boat's zone: the sun is up.
    const morning = lastLight
    expect(morning).not.toBeNull()
    fireEvent.change(screen.getByLabelText('Replay position'), { target: { value: '500' } })
    expect(lastLight).not.toBeNull()
  })

  it('frames the track on load without the zoom-in', () => {
    render(<ReplayPanel passages={[passage]} isDarkTheme={false} />)
    expect(getFraming).not.toHaveBeenCalled()
  })

  it('zooms in when Play is pressed at the start, then plays and shows the overview map', async () => {
    render(<ReplayPanel passages={[passage]} isDarkTheme={false} />)
    fireEvent.click(screen.getByRole('switch', { name: 'Overview map' }))
    expect(localStorage.getItem('passages.insetMap')).toBe('1')
    fireEvent.click(screen.getByRole('button', { name: 'Play replay' }))
    expect(getFraming).toHaveBeenCalledTimes(1)
    expect(await screen.findByRole('button', { name: 'Pause replay' })).toBeInTheDocument()
    expect(screen.getByTestId('inset')).toBeInTheDocument()
  })

  it('places the opening move on the map a frame at a time, and renders the panel only at its start and end', async () => {
    render(<ReplayPanel passages={[passage]} isDarkTheme={false} />)
    fireEvent.click(screen.getByRole('button', { name: 'Play replay' }))
    expect(introActive).toBe(true)
    expect(jumpTo).toHaveBeenCalled()
    expect(await screen.findByRole('button', { name: 'Pause replay' })).toBeInTheDocument()
    expect(introActive).toBe(false)
    // The last camera of the move is the framing it lands on.
    expect(jumpTo).toHaveBeenLastCalledWith({ center: [149.5, 0], zoom: 9, pitch: 0, bearing: 0 })
  })

  it('starts the opening move on the globe and ends on the flat map, setting the projection only when it changes', async () => {
    render(<ReplayPanel passages={[passage]} isDarkTheme={false} />)
    fireEvent.click(screen.getByRole('button', { name: 'Play replay' }))
    expect(setProjection.mock.calls[0][0].type).toEqual(expect.arrayContaining(['vertical-perspective']))
    expect(setProjection.mock.calls[0][1]).toBe(5.5)
    await waitFor(() => expect(introActive).toBe(false))
    expect(setProjection).toHaveBeenLastCalledWith({ type: 'mercator' }, null)
    expect(setProjection.mock.calls.length).toBeLessThanOrEqual(2)
  })

  it('two gestures on the map before the panel redraws end the move once and leave the replay playing', async () => {
    render(<ReplayPanel passages={[passage]} isDarkTheme={false} />)
    fireEvent.click(screen.getByRole('button', { name: 'Play replay' }))
    act(() => { interrupt?.(); interrupt?.() })
    expect(await screen.findByRole('button', { name: 'Pause replay' })).toBeInTheDocument()
    expect(introActive).toBe(false)
  })

  it('does not start playing at the landing if the operator moved the replay during the move', async () => {
    render(<ReplayPanel passages={[passage]} isDarkTheme={false} />)
    fireEvent.click(screen.getByRole('button', { name: 'Play replay' }))
    act(() => { fireEvent.change(screen.getByLabelText('Replay position'), { target: { value: '500' } }) })
    await waitFor(() => expect(introActive).toBe(false))
    expect(screen.getByRole('button', { name: 'Play replay' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Pause replay' })).toBeNull()
  })

  it('does not zoom when resuming after a pause or after scrubbing mid-replay', async () => {
    render(<ReplayPanel passages={[passage]} isDarkTheme={false} />)
    fireEvent.change(screen.getByLabelText('Replay position'), { target: { value: '500' } })
    fireEvent.click(screen.getByRole('button', { name: 'Play replay' }))
    expect(await screen.findByRole('button', { name: 'Pause replay' })).toBeInTheDocument()
    expect(getFraming).not.toHaveBeenCalled()
  })

  it('plays the whole track in 60, 30, 15 or 8 seconds', () => {
    expect(REPLAY_SPEEDS.map((s) => [s.label, s.seconds])).toEqual([['60 s', 60], ['30 s', 30], ['15 s', 15], ['8 s', 8]])
  })

  it('says so when no track was kept rather than drawing nothing', () => {
    render(<ReplayPanel passages={[{ ...passage, track: [], track_times: [], replay_track: [], replay_times: [] }]} isDarkTheme={false} />)
    expect(screen.getByTestId('passage-no-track')).toBeInTheDocument()
  })

  describe('the overview map corner and zoom step', () => {
    // The button waits, disabled, until the saved placement has been read.
    const openPlacement = async () => {
      await waitFor(() => expect(screen.getByRole('button', { name: 'Place overview map' })).toBeEnabled())
      fireEvent.click(screen.getByRole('button', { name: 'Place overview map' }))
    }
    const turnOn = () => fireEvent.click(screen.getByRole('switch', { name: 'Overview map' }))

    it('draws the overview in the corner saved for the passage, at its zoom step', async () => {
      saved = { corner: 'bottom-left', zoom_offset: -2 }
      render(<ReplayPanel passages={[passage]} isDarkTheme={false} />)
      turnOn()
      await waitFor(() => expect(screen.getByTestId('map')).toHaveAttribute('data-inset-corner', 'bottom-left'))
      expect(screen.getByTestId('inset')).toHaveAttribute('data-zoom-offset', '-2')
    })

    it('asks for the setting by the replay range, first start to last end, in milliseconds', async () => {
      const mock = stubApi({ 'GET /api/passages/camera-shots': { shots: [] }, 'GET /api/passages/signs': { signs: [] }, ...insetRoutes() })
      render(<ReplayPanel passages={[passage]} isDarkTheme={false} />)
      await waitFor(() => expect(mock.mock.calls.some(([u]) => String(u) === `/api/passages/inset-view?from=${start * 1000}&to=${(start + 1000) * 1000}`)).toBe(true))
    })

    it('saves a new corner for the passage and moves the overview at once', async () => {
      render(<ReplayPanel passages={[passage]} isDarkTheme={false} />)
      turnOn()
      await openPlacement()
      fireEvent.click(screen.getByRole('button', { name: 'Bottom right' }))
      expect(screen.getByTestId('map')).toHaveAttribute('data-inset-corner', 'bottom-right')
      await waitFor(() => expect(puts).toEqual([{ from_ms: start * 1000, to_ms: (start + 1000) * 1000, corner: 'bottom-right', zoom_offset: 0 }]))
    })

    it('saves each zoom step, building on the one before it', async () => {
      render(<ReplayPanel passages={[passage]} isDarkTheme={false} />)
      turnOn()
      await openPlacement()
      fireEvent.click(screen.getByRole('button', { name: 'Closer' }))
      fireEvent.click(screen.getByRole('button', { name: 'Closer' }))
      expect(screen.getByTestId('inset')).toHaveAttribute('data-zoom-offset', '2')
      await waitFor(() => expect(puts).toHaveLength(2))
      expect(puts).toEqual([
        { from_ms: start * 1000, to_ms: (start + 1000) * 1000, corner: 'top-right', zoom_offset: 1 },
        { from_ms: start * 1000, to_ms: (start + 1000) * 1000, corner: 'top-right', zoom_offset: 2 },
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
      await openPlacement()
      fireEvent.click(screen.getByRole('button', { name: 'Top left' }))
      await waitFor(() => expect(toast.error).toHaveBeenCalledWith('Could not save the overview map', { description: 'zoom_offset must be between -3 and 3' }))
      await waitFor(() => expect(screen.getByTestId('map')).toHaveAttribute('data-inset-corner', 'top-right'))
    })

    it('shows no overview, says why, and offers a retry that brings the placement back', async () => {
      let failing = true
      stubApi({
        'GET /api/passages/camera-shots': { shots: [] }, 'GET /api/passages/signs': { signs: [] },
        ...insetRoutes(),
        'GET /api/passages/inset-view': () => failing
          ? new Response(JSON.stringify({ error: 'boom' }), { status: 500, headers: { 'Content-Type': 'application/json' } })
          : { inset_view: { from_ms: start * 1000, to_ms: (start + 1000) * 1000, corner: 'bottom-left', zoom_offset: 1 } },
      })
      render(<ReplayPanel passages={[passage]} isDarkTheme={false} />)
      turnOn()
      await waitFor(() => expect(toast.error).toHaveBeenCalledWith('Could not read where the overview map goes', expect.anything()))
      expect(screen.queryByTestId('inset')).toBeNull()
      failing = false
      fireEvent.click(screen.getByRole('button', { name: 'Retry reading the overview map placement' }))
      await waitFor(() => expect(screen.getByTestId('map')).toHaveAttribute('data-inset-corner', 'bottom-left'))
      expect(screen.getByTestId('inset')).toHaveAttribute('data-zoom-offset', '1')
    })

    it('sends saves one at a time, so the last click is what the server and the screen end with', async () => {
      const releases: (() => void)[] = []
      const started: unknown[] = []
      stubApi({
        'GET /api/passages/camera-shots': { shots: [] }, 'GET /api/passages/signs': { signs: [] },
        ...insetRoutes(),
        'PUT /api/passages/inset-view': (req: StubRequest) => new Promise((resolve) => {
          started.push(req.body)
          const b = req.body as { corner: string; zoom_offset: number }
          releases.push(() => { saved = { corner: b.corner, zoom_offset: b.zoom_offset }; resolve({ inset_view: { from_ms: start * 1000, to_ms: (start + 1000) * 1000, ...saved } }) })
        }),
      })
      render(<ReplayPanel passages={[passage]} isDarkTheme={false} />)
      turnOn()
      await openPlacement()
      fireEvent.click(screen.getByRole('button', { name: 'Closer' }))
      fireEvent.click(screen.getByRole('button', { name: 'Closer' }))
      await waitFor(() => expect(started).toHaveLength(1))
      // The second save waits for the first to land.
      await new Promise((r) => setTimeout(r, 30))
      expect(started).toHaveLength(1)
      releases[0]()
      await waitFor(() => expect(started).toHaveLength(2))
      expect(started[1]).toMatchObject({ zoom_offset: 2 })
      releases[1]()
      await waitFor(() => expect(screen.getByTestId('inset')).toHaveAttribute('data-zoom-offset', '2'))
      expect(saved.zoom_offset).toBe(2)
    })
  })
})

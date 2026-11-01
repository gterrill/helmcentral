import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { forwardRef, useImperativeHandle } from 'react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { ReplayPanel } from '@/components/passages/replay-panel'
import type { CameraDriver } from '@/components/passages/passage-map'
import type { PassageDetail } from '@/lib/passages'
import { resetQueryStore } from '@/lib/api-query'
import { stubApi, type StubRequest } from './helpers/stub-api'

vi.mock('@/hooks/use-boat-hull', () => ({ useBoatHull: () => null }))

let driver: CameraDriver | null = null
let onFlat: ((flat: boolean) => void) | undefined
const getView = vi.fn(() => ({ zoom: 13.456, pitch: 42 as number | null, bearing: 135 }))
vi.mock('@/components/passages/passage-map', () => ({
  passageColour: () => 'x',
  PassageMap: forwardRef(function PassageMapMock(p: { cameraDriver?: CameraDriver | null; onFlatChange?: (flat: boolean) => void }, ref) {
    useImperativeHandle(ref, () => ({ getFraming: () => null, jumpTo: () => {}, setProjection: () => {}, getView }))
    driver = p.cameraDriver ?? null
    onFlat = p.onFlatChange
    return <div data-testid="map" />
  }),
  InsetMap: () => <div />,
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
  // Due east along the equator, 0.001 degrees a minute or so.
  track: [[[0, 149], [0, 150]]],
  track_times: [[start, start + 1000]],
  replay_track: [[[0, 149], [0, 149.5], [0, 150]]],
  replay_times: [[start, start + 500, start + 1000]],
}

const shot = (id: string, offset: number, extra: Record<string, unknown> = {}) => ({
  id, t_ms: (start + offset) * 1000, mode: 'follow', zoom: 14, pitch: 50, bearing_offset_deg: 0, orbit_deg_per_s: 0, pace: 1, ...extra,
})

let shots: ReturnType<typeof shot>[] = []
let writes: { method: string; url: string; body: unknown }[] = []

function stub() {
  stubApi({
    'GET /api/passages/signs': { signs: [] },
    'GET /api/passages/inset-view': { inset_view: { from_ms: 0, to_ms: 0, corner: 'top-right', zoom_offset: 0 } },
    'GET /api/passages/camera-shots': () => ({ shots }),
    'POST /api/passages/camera-shots': (req: StubRequest) => {
      writes.push({ method: 'POST', url: req.url, body: req.body })
      const created = { ...(req.body as object), id: 'new' } as ReturnType<typeof shot>
      shots = [...shots, created]
      return { shot: created }
    },
    'PATCH /api/passages/camera-shots/s1': (req: StubRequest) => {
      writes.push({ method: 'PATCH', url: req.url, body: req.body })
      shots = shots.map((s) => (s.id === 's1' ? { ...s, ...(req.body as object) } : s))
      return { shot: shots[0] }
    },
    'DELETE /api/passages/camera-shots/s1': (req: StubRequest) => {
      writes.push({ method: 'DELETE', url: req.url, body: undefined })
      shots = shots.filter((s) => s.id !== 's1')
      return { deleted: true }
    },
  })
}

beforeEach(() => {
  resetQueryStore()
  localStorage.clear()
  driver = null
  shots = []
  writes = []
  getView.mockClear()
  stub()
})

describe('ReplayPanel camera shots', () => {
  it('has no camera driver and no marks without shots: today\'s behaviour', async () => {
    render(<ReplayPanel passages={[passage]} isDarkTheme={false} />)
    await screen.findByRole('button', { name: 'Camera' })
    expect(driver).toBeNull()
    expect(screen.queryByRole('button', { name: /^Camera shot/ })).toBeNull()
  })

  it('draws a mark on the scrubber for each shot inside the replay and none for one outside', async () => {
    shots = [shot('s1', 250), shot('out', -86_400)]
    render(<ReplayPanel passages={[passage]} isDarkTheme={false} />)
    const mark = await screen.findByRole('button', { name: /Camera shot 1, Follow, at/ })
    expect(mark).toHaveStyle({ left: '25%' })
    expect(screen.getAllByRole('button', { name: /^Camera shot/ })).toHaveLength(1)
    expect(driver).not.toBeNull()
  })

  it('a mark seeks to its shot and opens the editor on it', async () => {
    shots = [shot('s1', 250, { mode: 'orbit', orbit_deg_per_s: 6 })]
    render(<ReplayPanel passages={[passage]} isDarkTheme={false} />)
    fireEvent.click(await screen.findByRole('button', { name: /Camera shot 1, Orbit, at/ }))
    expect((screen.getByLabelText('Replay position') as HTMLInputElement).value).toBe('250')
    const editor = screen.getByTestId('camera-editor')
    expect(within(editor).getByRole('button', { name: 'Orbit' })).toHaveAttribute('aria-pressed', 'true')
    expect(within(editor).getByLabelText('Orbit °/s')).toHaveValue(6)
    expect(within(editor).queryByLabelText('Angle')).toBeNull()
  })

  it('adds a Follow shot at the cursor with the current map zoom and pitch', async () => {
    render(<ReplayPanel passages={[passage]} isDarkTheme={false} />)
    await screen.findByRole('button', { name: 'Camera' })
    fireEvent.change(screen.getByLabelText('Replay position'), { target: { value: '400' } })
    fireEvent.click(screen.getByRole('button', { name: 'Camera' }))
    fireEvent.click(screen.getByRole('button', { name: 'Add shot here' }))
    await waitFor(() => expect(writes).toHaveLength(1))
    expect(writes[0].body).toEqual({
      t_ms: (start + 400) * 1000, mode: 'follow', zoom: 13.46, pitch: 42, bearing_offset_deg: 0, orbit_deg_per_s: 0, pace: 1,
    })
    // The new shot is picked, ready to edit.
    expect(await screen.findByRole('button', { name: 'Follow' })).toHaveAttribute('aria-pressed', 'true')
  })

  it('edits mode, pace, numbers and deletes, each sent to the server', async () => {
    shots = [shot('s1', 250)]
    render(<ReplayPanel passages={[passage]} isDarkTheme={false} />)
    fireEvent.click(await screen.findByRole('button', { name: /Camera shot 1, Follow, at/ }))
    fireEvent.click(screen.getByRole('button', { name: 'Pace half' }))
    await waitFor(() => expect(writes).toHaveLength(1))
    expect(writes[0].body).toEqual({ pace: 0.5 })
    const angle = screen.getByLabelText('Angle')
    fireEvent.change(angle, { target: { value: '90' } })
    fireEvent.blur(angle)
    await waitFor(() => expect(writes).toHaveLength(2))
    expect(writes[1].body).toEqual({ bearing_offset_deg: 90 })
    const zoom = screen.getByLabelText('Zoom')
    fireEvent.change(zoom, { target: { value: '40' } })
    fireEvent.keyDown(zoom, { key: 'Enter' })
    await waitFor(() => expect(writes).toHaveLength(3))
    expect(writes[2].body).toEqual({ zoom: 22 })
    fireEvent.click(screen.getByRole('button', { name: 'Delete shot' }))
    await waitFor(() => expect(writes[3]?.method).toBe('DELETE'))
    await waitFor(() => expect(screen.queryByRole('button', { name: /^Camera shot/ })).toBeNull())
  })

  it('Use current view copies zoom, pitch and the angle off the bow', async () => {
    shots = [shot('s1', 250)]
    render(<ReplayPanel passages={[passage]} isDarkTheme={false} />)
    fireEvent.click(await screen.findByRole('button', { name: /Camera shot 1, Follow, at/ }))
    fireEvent.click(screen.getByRole('button', { name: 'Use current view' }))
    await waitFor(() => expect(writes).toHaveLength(1))
    // The boat heads east (090); the view is turned to 135: 45 degrees off the stern.
    expect(writes[0].body).toEqual({ zoom: 13.46, pitch: 42, bearing_offset_deg: 45 })
  })

  it('shows the server\'s reason when a shot is refused', async () => {
    stubApi({
      'GET /api/passages/inset-view': { inset_view: { from_ms: 0, to_ms: 0, corner: 'top-right', zoom_offset: 0 } },
      'GET /api/passages/camera-shots': () => ({ shots }),
      'POST /api/passages/camera-shots': () => new Response(JSON.stringify({ error: 'zoom must be between 0 and 22' }), { status: 400, headers: { 'Content-Type': 'application/json' } }),
    })
    render(<ReplayPanel passages={[passage]} isDarkTheme={false} />)
    fireEvent.click(await screen.findByRole('button', { name: 'Camera' }))
    fireEvent.click(screen.getByRole('button', { name: 'Add shot here' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('zoom must be between 0 and 22')
  })

  it('a slowed shot stretches the replay and keeps the scrubber in timeline units', async () => {
    shots = [shot('s1', 0, { pace: 0.5 })]
    render(<ReplayPanel passages={[passage]} isDarkTheme={false} />)
    await screen.findByRole('button', { name: /Camera shot 1, Follow, at/ })
    expect(screen.getByLabelText('Replay position')).toHaveAttribute('max', '1000')
  })

  it('asks for a camera that follows the boat', async () => {
    shots = [shot('s1', 0)]
    render(<ReplayPanel passages={[passage]} isDarkTheme={false} />)
    await screen.findByRole('button', { name: /Camera shot 1, Follow, at/ })
    fireEvent.change(screen.getByLabelText('Replay position'), { target: { value: '500' } })
    const overview = { center: [149.5, 0] as [number, number], zoom: 8, pitch: 50, bearing: 0 }
    const result = driver!.resolve({ flat: false, overview, current: overview })!
    expect(result.shotKey).toBe('s1')
    expect(result.camera.center).toEqual([149.5, 0])
    expect(result.camera.bearing).toBeCloseTo(90, 0)
    expect(driver!.clock).toBe(500)
  })
})

describe('ReplayPanel camera under reduced motion', () => {
  it('tells the map to cut rather than move', async () => {
    vi.stubGlobal('matchMedia', (query: string) => ({
      matches: query === '(prefers-reduced-motion: reduce)',
      addEventListener: () => {},
      removeEventListener: () => {},
    }))
    try {
      shots = [shot('s1', 0)]
      render(<ReplayPanel passages={[passage]} isDarkTheme={false} />)
      await screen.findByRole('button', { name: /Camera shot 1, Follow, at/ })
      expect(driver!.reduced).toBe(true)
    } finally {
      vi.unstubAllGlobals()
    }
  })
})

describe('ReplayPanel camera robustness', () => {
  const second: PassageDetail = { ...passage, id: 'p2', start: new Date((start + 1000) * 1000).toISOString(), end: new Date((start + 1400) * 1000).toISOString(), track: [], track_times: [], replay_track: [], replay_times: [] }

  it('a shot at the end of one passage does not break when the next has no track', async () => {
    shots = [shot('s1', 1000)]
    render(<ReplayPanel passages={[passage, second]} isDarkTheme={false} />)
    await screen.findByRole('button', { name: /Camera shot 1, Follow, at/ })
    fireEvent.change(screen.getByLabelText('Replay position'), { target: { value: '1000' } })
    const overview = { center: [149.5, 0] as [number, number], zoom: 8, pitch: 50, bearing: 0 }
    expect(() => driver!.resolve({ flat: false, overview, current: overview })).not.toThrow()
    fireEvent.change(screen.getByLabelText('Replay position'), { target: { value: '1300' } })
    expect(() => driver!.resolve({ flat: false, overview, current: overview })).not.toThrow()
  })

  it('tells the editor the map is flat when the map says so, not from the 3D switch', async () => {
    shots = [shot('s1', 250)]
    render(<ReplayPanel passages={[passage]} isDarkTheme={false} />)
    fireEvent.click(await screen.findByRole('button', { name: 'Camera' }))
    expect(screen.queryByText(/Flat map/)).toBeNull()
    act(() => onFlat!(true))
    expect(screen.getByText(/Flat map/)).toBeInTheDocument()
  })

  it('clears a shown error when another shot is picked, and locks the fields during a write', async () => {
    shots = [shot('s1', 250), shot('s2', 600)]
    let release: () => void = () => {}
    stubApi({
      'GET /api/passages/inset-view': { inset_view: { from_ms: 0, to_ms: 0, corner: 'top-right', zoom_offset: 0 } },
      'GET /api/passages/camera-shots': () => ({ shots }),
      'PATCH /api/passages/camera-shots/s1': () => new Promise((resolve) => { release = () => resolve({ shot: shots[0] }) }),
      'PATCH /api/passages/camera-shots/s2': () => new Response(JSON.stringify({ error: 'zoom must be between 0 and 22' }), { status: 400, headers: { 'Content-Type': 'application/json' } }),
    })
    render(<ReplayPanel passages={[passage]} isDarkTheme={false} />)
    const marks = await screen.findAllByRole('button', { name: /^Camera shot/ })
    fireEvent.click(marks[0])
    fireEvent.click(screen.getByRole('button', { name: 'Pace half' }))
    await waitFor(() => expect(screen.getByLabelText('Zoom')).toBeDisabled())
    release()
    await waitFor(() => expect(screen.getByLabelText('Zoom')).not.toBeDisabled())
    fireEvent.click(screen.getAllByRole('button', { name: /^Camera shot/ })[1])
    fireEvent.click(screen.getByRole('button', { name: 'Pace half' }))
    expect(await screen.findByRole('alert')).toBeInTheDocument()
    fireEvent.click(screen.getAllByRole('button', { name: /^Camera shot/ })[0])
    expect(screen.queryByRole('alert')).toBeNull()
  })

  it('names the shot being edited, with its number, start and end', async () => {
    shots = [shot('s1', 250), shot('s2', 600)]
    render(<ReplayPanel passages={[passage]} isDarkTheme={false} />)
    const marks = await screen.findAllByRole('button', { name: /^Camera shot/ })
    fireEvent.click(screen.getByRole('button', { name: 'Camera' }))
    const editor = screen.getByTestId('camera-editor')
    expect(within(editor).queryByTestId('camera-shot-heading')).toBeNull()
    expect(within(editor).getByText(/Pick a mark on the scrubber/)).toBeInTheDocument()
    fireEvent.click(marks[0])
    expect(within(editor).getByTestId('camera-shot-heading')).toHaveTextContent('Shot 1 of 2 · from 09:04 to 09:10')
    fireEvent.click(screen.getAllByRole('button', { name: /^Camera shot/ })[1])
    expect(within(editor).getByTestId('camera-shot-heading')).toHaveTextContent('Shot 2 of 2 · from 09:10 to end')
  })

  it('puts dates on the titles and heading once the shots span days', async () => {
    shots = [shot('s1', 250), shot('s2', 600, { t_ms: (start + 86_400) * 1000 })]
    const day2 = { ...passage, id: 'p2', start: new Date((start + 86_000) * 1000).toISOString(), end: new Date((start + 87_000) * 1000).toISOString(), track_times: [[start + 86_000, start + 87_000]], replay_times: [[start + 86_000, start + 86_500, start + 87_000]] }
    render(<ReplayPanel passages={[passage, day2]} isDarkTheme={false} />)
    const marks = await screen.findAllByRole('button', { name: /^Camera shot/ })
    expect(marks[0].getAttribute('title')).toMatch(/^Shot 1 · \d+ \w{3} 09:04$/)
    fireEvent.click(screen.getByRole('button', { name: 'Camera' }))
    fireEvent.click(marks[0])
    // The first shot ends with its own passage, not at the next day's shot.
    expect(screen.getByTestId('camera-shot-heading').textContent).toMatch(/^Shot 1 of 2 · from \d+ \w{3} 09:04 to \d+ \w{3} 09:16$/)
  })

  it('titles each mark with its number and time and rings the selected one', async () => {
    shots = [shot('s1', 250), shot('s2', 600)]
    render(<ReplayPanel passages={[passage]} isDarkTheme={false} />)
    const marks = await screen.findAllByRole('button', { name: /^Camera shot/ })
    expect(marks[0]).toHaveAttribute('title', 'Shot 1 · 09:04')
    expect(marks[1]).toHaveAttribute('title', 'Shot 2 · 09:10')
    expect(marks[1].className).not.toContain('ring-2')
    fireEvent.click(marks[1])
    const picked = screen.getAllByRole('button', { name: /^Camera shot/ })
    expect(picked[1]).toHaveAttribute('aria-pressed', 'true')
    expect(picked[1].className).toContain('ring-2')
    expect(picked[0].className).not.toContain('ring-2')
  })
})

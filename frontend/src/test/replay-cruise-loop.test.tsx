import { act, fireEvent, render, screen } from '@testing-library/react'
import { Profiler, StrictMode } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import type { PassageDetail } from '@/lib/passages'
import { saveInsetPreference } from '@/lib/passage-inset-preference'
import { ReplayPanel } from '@/test/replay-panel-harness'
import { stubApi } from './helpers/stub-api'

// The real replay panel on the real passage map, with MapLibre faked by a map
// that behaves as one does where it matters here: placing the camera is
// synchronous, and fires the zoom events that come of it.

vi.mock('maplibre-gl', () => ({ default: {} }))
vi.mock('@/lib/webgl', () => ({ hasWebGL2: () => true }))
vi.mock('@/hooks/use-boat-hull', () => ({ useBoatHull: () => null }))
vi.mock('@/components/map-place-labels', () => ({ MapPlaceLabels: () => null, warnIfBaseVectorSourceMissing: () => {} }))

const m = vi.hoisted(() => ({
  handlers: new Map<string, Set<(e?: unknown) => void>>(),
  current: { center: { lng: 149, lat: 0 }, zoom: 9, pitch: 0, bearing: 0 },
  jumps: 0,
  props: {} as Record<string, unknown>,
}))
const emit = (name: string) => m.handlers.get(name)?.forEach((f) => f({}))

vi.mock('react-map-gl/maplibre', async () => {
  const React = await import('react')
  const map = {
    getSource: () => ({}),
    easeTo: vi.fn(),
    fitBounds: vi.fn(),
    once: vi.fn(),
    on: (name: string, cb: (e?: unknown) => void) => { (m.handlers.get(name) ?? m.handlers.set(name, new Set()).get(name)!).add(cb) },
    off: (name: string, cb: (e?: unknown) => void) => { m.handlers.get(name)?.delete(cb) },
    jumpTo: (c: { center: [number, number]; zoom: number; pitch: number; bearing: number }) => {
      m.jumps++
      const zoomed = c.zoom !== m.current.zoom
      m.current = { center: { lng: c.center[0], lat: c.center[1] }, zoom: c.zoom, pitch: c.pitch, bearing: c.bearing }
      emit('movestart')
      emit('move')
      if (zoomed) emit('zoomend')
      emit('moveend')
    },
    project: () => ({ x: 400, y: 200 }),
    getZoom: () => m.current.zoom,
    getPitch: () => m.current.pitch,
    getBearing: () => m.current.bearing,
    getCenter: () => m.current.center,
    getCanvasContainer: () => document.createElement('div'),
    getContainer: () => ({ clientWidth: 800, clientHeight: 400 }),
    // The fit depends on the bounds: a passage is closer in than the whole cruise.
    cameraForBounds: (b: [[number, number], [number, number]]) => ({ center: { lng: (b[0][0] + b[1][0]) / 2, lat: (b[0][1] + b[1][1]) / 2 }, zoom: 11 - Math.log2(1 + (b[1][0] - b[0][0]) * 20) }),
    getLayer: () => undefined,
    // Changing the style tells everyone who listens, as MapLibre does.
    setSky: vi.fn(() => emit('styledata')),
    setLight: vi.fn(() => emit('styledata')),
    setPaintProperty: vi.fn(() => emit('styledata')),
    getPaintProperty: () => undefined,
    setTerrain: vi.fn(),
    setProjection: vi.fn(),
    getStyle: () => ({ layers: [] }),
    moveLayer: vi.fn(),
    getLayersOrder: () => [],
  }
  return {
    Map: React.forwardRef((props: Record<string, unknown> & { children?: React.ReactNode }, ref: React.Ref<unknown>) => {
      m.props = props
      React.useImperativeHandle(ref, () => ({ getMap: () => map, getCanvas: () => ({}) }))
      return <div>{props.children}</div>
    }),
    Marker: () => null,
    Source: ({ children }: { children?: React.ReactNode }) => <div>{children}</div>,
    // react-map-gl's Layer draws again on every change to the style.
    Layer: function Layer() {
      const [, force] = React.useState(0)
      React.useEffect(() => {
        const again = () => force((v) => v + 1)
        map.on('styledata', again)
        return () => map.off('styledata', again)
      }, [])
      return null
    },
  }
})

const day = 86400
const t0 = Date.UTC(2026, 2, 13, 23, 0) / 1000
const passage = (id: string, n: number, lon: number): PassageDetail => {
  const start = t0 + n * 2 * day
  return {
    id, start: new Date(start * 1000).toISOString(), end: new Date((start + 1000) * 1000).toISOString(),
    from_name: 'A', to_name: 'B', title: '', notes: '', hidden: false, edited: false, cruise_id: null, created_at: '',
    stats: {
      distance_nm: 0, underway_seconds: 0, mean_sog_kts: null, max_sog_kts: null, fuel_burned_l: null, tws_mean_kts: null,
      max_gust_kts: null, soc_start_pct: null, soc_end_pct: null, soc_min_pct: null,
    },
    timezone: 'Australia/Brisbane',
    track: [[[0, lon], [0, lon + 0.5]]], track_times: [[start, start + 1000]],
    replay_track: [[[0, lon], [0, lon + 0.25], [0, lon + 0.5]]], replay_times: [[start, start + 500, start + 1000]],
  }
}
const passages = [passage('p1', 0, 149), passage('p2', 1, 149.8), passage('p3', 2, 150.6)]

// A frame at a time, by hand.
let frames: ((now: number) => void)[] = []
let now = 0
const errors: string[] = []

beforeEach(() => {
  m.handlers.clear()
  m.jumps = 0
  m.current = { center: { lng: 149, lat: 0 }, zoom: 9, pitch: 0, bearing: 0 }
  frames = []
  now = 1000
  errors.length = 0
  saveInsetPreference(false)
  localStorage.clear()
  stubApi({ 'GET /api/passages/camera-shots': { shots: [] }, 'GET /api/passages/signs': { signs: [] }, 'GET /api/passages/inset-view': { inset_view: null } })
  vi.stubGlobal('requestAnimationFrame', (cb: (n: number) => void) => { frames.push(cb); return frames.length })
  vi.stubGlobal('cancelAnimationFrame', () => {})
  vi.spyOn(performance, 'now').mockImplementation(() => now)
  // A map with a width: a label with no room is not drawn.
  vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockReturnValue({ width: 800, height: 400, x: 0, y: 0, top: 0, left: 0, right: 800, bottom: 400, toJSON: () => ({}) })
  vi.spyOn(console, 'error').mockImplementation((...args: unknown[]) => { errors.push(args.map(String).join(' ')) })
})
afterEach(() => { vi.unstubAllGlobals(); vi.restoreAllMocks() })

/** Runs `seconds` of watching, a frame every 50 ms. */
const seen = { stay: 0, passage: 0, frames: 0 }
function watch(seconds: number) {
  for (let t = 0; t < seconds; t += 0.05) {
    seen.frames++
    if (screen.queryByTestId('stay-label')) seen.stay++
    if (screen.queryByTestId('passage-label')) seen.passage++
    now += 50
    const due = frames
    frames = []
    act(() => { due.forEach((f) => f(now)) })
  }
}

describe('a replay of three passages through its stays', () => {
  it('moves the camera through them without a render loop', () => {
    let commits = 0
    render(<StrictMode><Profiler id="p" onRender={() => { commits++ }}><ReplayPanel passages={passages} isDarkTheme={false} /></Profiler></StrictMode>)
    act(() => { (m.props.onLoad as () => void)() })
    // Start just short of the first stay, so the replay does not open on the fly-in, and play on through both stays.
    fireEvent.change(screen.getByLabelText('Replay position'), { target: { value: '900' } })
    fireEvent.click(screen.getByRole('button', { name: /play/i }))
    const before = m.jumps
    commits = 0
    watch(20)
    // Both stays were played, with their labels up, and the panel drew about once a frame.
    expect(seen.stay).toBeGreaterThan(40)
    expect(commits).toBeLessThan(seen.frames * 1.5)
    expect(errors.filter((e) => /Maximum update depth/.test(e))).toEqual([])
    expect(errors).toEqual([])
    // The camera moved while the stays played.
    expect(m.jumps).toBeGreaterThan(before)
  })

  it('and when the zoom events come from the camera it placed', () => {
    render(<ReplayPanel passages={passages} isDarkTheme={false} />)
    act(() => { (m.props.onLoad as () => void)() })
    fireEvent.change(screen.getByLabelText('Replay position'), { target: { value: '950' } })
    fireEvent.click(screen.getByRole('button', { name: /play/i }))
    watch(20)
    expect(errors.filter((e) => /Maximum update depth/.test(e))).toEqual([])
  })
})

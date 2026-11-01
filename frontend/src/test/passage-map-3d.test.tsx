import { act, fireEvent, render, screen } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { createRef } from 'react'

import { PassageMap, type PassageMapHandle } from '@/components/passages/passage-map'
import { load3dPreference, save3dPreference } from '@/lib/passage-3d-preference'
import { saveImageryPreference } from '@/lib/passage-imagery-preference'
import { introProjectionAt } from '@/lib/replay-intro'
import { drawnPieces, type PassageLeg } from '@/lib/passage-legs'
import type { TimedSegment } from '@/lib/passages'
import { SPEED_RAMP_LIGHT, speedFrameAt, speedTrack } from '@/lib/replay-speed'
import { lightFor } from '@/lib/replay-light'

vi.mock('maplibre-gl', () => ({ default: {} }))
vi.mock('@/lib/webgl', () => ({ hasWebGL2: () => true }))

const easeTo = vi.fn()
const fitBounds = vi.fn()
const jumpTo = vi.fn()
const setSky = vi.fn()
const setLight = vi.fn()
const setPaintProperty = vi.fn()
const setProjection = vi.fn()
let onceHandlers: (() => void)[] = []
const surface = document.createElement('div')
const mapHandlers: Record<string, ((e: unknown) => void)[]> = {}
const moveEnd = () => { const h = onceHandlers; onceHandlers = []; act(() => h.forEach((f) => f())) }
let mapProps: Record<string, unknown> = {}
let sources: Record<string, Record<string, unknown>> = {}
const lastData: Record<string, unknown> = {}
let layerIds: string[] = []
const layerPaint: Record<string, Record<string, unknown> | undefined> = {}

vi.mock('react-map-gl/maplibre', async () => {
  const React = await import('react')
  return {
    Map: React.forwardRef((props: Record<string, unknown> & { children?: React.ReactNode }, ref: React.Ref<unknown>) => {
      mapProps = props
      React.useImperativeHandle(ref, () => ({
        getMap: () => ({
          getSource: () => ({}),
          easeTo,
          getZoom: () => 10,
          getBearing: () => 0,
          fitBounds,
          jumpTo,
          once: (_: string, cb: () => void) => { onceHandlers.push(cb) },
          on: (name: string, cb: (e: unknown) => void) => { (mapHandlers[name] ??= []).push(cb) },
          off: (name: string, cb: (e: unknown) => void) => { mapHandlers[name] = (mapHandlers[name] ?? []).filter((f) => f !== cb) },
          setSky,
          setLight,
          setPaintProperty,
          getPaintProperty: () => undefined,
          setProjection,
          getLayer: (id: string) => (id === 'terrain-hillshade' || id === 'world-imagery-layer' ? {} : undefined),
          getCanvasContainer: () => surface,
          cameraForBounds: () => ({ center: { lng: 148.05, lat: -20.05 }, zoom: 9 }),
          getContainer: () => ({ clientWidth: 800, clientHeight: 400 }),
        }),
        getCanvas: () => ({}),
      }))
      return <div data-testid="map-root">{props.children}</div>
    }),
    Marker: ({ children }: { children?: React.ReactNode }) => <div>{children}</div>,
    Source: ({ id, children, ...rest }: { id: string; children?: React.ReactNode; data?: unknown }) => {
      sources[id] = rest
      lastData[id] = rest.data
      return <div data-testid={`source-${id}`}>{children}</div>
    },
    Layer: ({ id, beforeId, paint }: { id: string; beforeId?: string; paint?: Record<string, unknown> }) => {
      layerIds.push(id)
      layerPaint[id] = paint
      return <div data-testid={`layer-${id}`} data-before-id={beforeId} />
    },
  }
})

vi.mock('@/components/map-place-labels', () => ({
  MapPlaceLabels: () => null,
  warnIfBaseVectorSourceMissing: () => {},
}))

// The speed frame the replay hands the map: made from the same lib functions the controller uses.
function speedFor(ps: { id: string; segments: TimedSegment[] }[], sailedTo: Map<string, { at: number; end?: { lat: number; lon: number } }>, legs?: readonly PassageLeg[]) {
  const pieces = drawnPieces(ps, legs)
  return speedFrameAt(pieces, speedTrack(pieces)!, sailedTo, SPEED_RAMP_LIGHT, null, { zoom: 12 })
}

const passages = [{ id: 'p1', segments: [[{ t: 0, lat: -20, lon: 148 }, { t: 60, lat: -20.1, lon: 148.1 }]] }]

function renderMap() {
  return render(<PassageMap passages={passages} playedIds={new Set()} boat={null} isDarkTheme={false} />)
}

beforeEach(() => {
  save3dPreference(true)
  saveImageryPreference(true)
  localStorage.clear()
  easeTo.mockClear()
  fitBounds.mockClear()
  jumpTo.mockClear()
  setSky.mockClear()
  setLight.mockClear()
  setPaintProperty.mockClear()
  setProjection.mockClear()
  onceHandlers = []
  mapProps = {}
  sources = {}
  layerIds = []
})

describe('3D preference', () => {
  it('is on by default', () => {
    expect(load3dPreference()).toBe(true)
  })

  it('is remembered under its own key, apart from the satellite choice', () => {
    save3dPreference(false)
    expect(localStorage.getItem('passages.replay3d')).toBe('0')
    expect(localStorage.getItem('passages.satelliteImagery')).toBeNull()
  })
})

describe('PassageMap 3D', () => {
  it('starts in 3D: terrain at 1.5x, hillshade, pitch and rotate enabled', () => {
    renderMap()
    expect(screen.getByRole('button', { name: 'Toggle 3D' }).getAttribute('aria-pressed')).toBe('true')
    expect(mapProps.terrain).toEqual({ source: 'terrain-dem', exaggeration: 1.5 })
    expect(mapProps.dragRotate).toBe(true)
    expect(mapProps.touchPitch).toBe(true)
    expect(mapProps.maxPitch).toBe(70)
    expect(sources['terrain-dem']).toMatchObject({ type: 'raster-dem', encoding: 'terrarium', tileSize: 256, maxzoom: 15, tiles: ['/api/terrain/{z}/{x}/{y}'] })
    expect(screen.getByTestId('layer-terrain-hillshade').dataset.beforeId).toBe('openseamap-layer')
    expect((mapProps.initialViewState as { pitch: number }).pitch).toBe(50)
  })

  it('turning 3D off levels the camera first, then takes the gestures and terrain away', () => {
    renderMap()
    act(() => { (mapProps.onLoad as () => void)() })
    fireEvent.click(screen.getByRole('button', { name: 'Toggle 3D' }))
    expect(localStorage.getItem('passages.replay3d')).toBe('0')
    expect(easeTo).toHaveBeenLastCalledWith(expect.objectContaining({ pitch: 0, bearing: 0 }))
    // Still tiltable while the ease runs, so the pitch does not snap.
    expect(mapProps.maxPitch).toBe(70)
    expect(mapProps.dragRotate).toBe(true)
    expect(mapProps.terrain).toBeNull()
    expect(screen.queryByTestId('source-terrain-dem')).toBeNull()
    expect(screen.queryByTestId('layer-terrain-hillshade')).toBeNull()
    moveEnd()
    expect(mapProps.maxPitch).toBe(0)
    expect(mapProps.dragRotate).toBe(false)
    expect(mapProps.touchPitch).toBe(false)
  })

  it('is flat from the start when 3D is off', () => {
    localStorage.setItem('passages.replay3d', '0')
    renderMap()
    expect(mapProps.terrain).toBeNull()
    expect(mapProps.maxPitch).toBe(0)
    expect(mapProps.dragRotate).toBe(false)
    expect((mapProps.initialViewState as { pitch: number }).pitch).toBe(0)
  })

  it('tilts the camera when 3D comes on', () => {
    localStorage.setItem('passages.replay3d', '0')
    renderMap()
    act(() => { (mapProps.onLoad as () => void)() })
    fireEvent.click(screen.getByRole('button', { name: 'Toggle 3D' }))
    expect(easeTo).toHaveBeenLastCalledWith(expect.objectContaining({ pitch: 50 }))
    expect(mapProps.dragRotate).toBe(true)
  })

  it('frames the whole track at the 3D pitch, backing the zoom off for the tilt', () => {
    renderMap()
    act(() => { (mapProps.onLoad as () => void)() })
    expect(fitBounds).not.toHaveBeenCalled()
    const cam = jumpTo.mock.calls.at(-1)?.[0]
    expect(cam.pitch).toBe(50)
    expect(cam.bearing).toBe(0)
    expect(cam.zoom).toBeLessThanOrEqual(9)
  })

  it('offers the same pitched, fitted camera as the framing the opening move lands on', () => {
    const ref = createRef<PassageMapHandle>()
    render(<PassageMap ref={ref} passages={passages} playedIds={new Set()} boat={null} isDarkTheme={false} />)
    const framing = ref.current?.getFraming()
    expect(framing?.overview).toMatchObject({ pitch: 50, bearing: 0 })
    expect(framing!.overview.zoom).toBeLessThanOrEqual(9)
    expect(framing?.bearing).toBe(0)
    expect(easeTo).not.toHaveBeenCalled()
    expect(fitBounds).not.toHaveBeenCalled()
  })

  it('places a camera it is handed, for the opening move', () => {
    const ref = createRef<PassageMapHandle>()
    render(<PassageMap ref={ref} passages={passages} playedIds={new Set()} boat={null} isDarkTheme={false} />)
    act(() => { (mapProps.onLoad as () => void)() })
    const view = { center: [149, -20] as [number, number], zoom: 3, pitch: 10, bearing: 0 }
    ref.current?.jumpTo(view)
    expect(jumpTo).toHaveBeenLastCalledWith(view)
  })

  describe('while the opening move runs', () => {
    const mountIntro = () => {
      const interrupted = vi.fn()
      render(<PassageMap passages={passages} playedIds={new Set()} boat={null} isDarkTheme={false} introActive onIntroInterrupt={interrupted} />)
      act(() => { (mapProps.onLoad as () => void)() })
      return interrupted
    }

    it('a drag that starts a move of its own ends it, the camera\'s own jumps do not', () => {
      const interrupted = mountIntro()
      act(() => { mapHandlers.movestart?.forEach((f) => f({})) })
      expect(interrupted).not.toHaveBeenCalled()
      act(() => { mapHandlers.movestart?.forEach((f) => f({ originalEvent: {} })) })
      expect(interrupted).toHaveBeenCalledTimes(1)
    })

    it.each(['pointerdown', 'touchstart', 'wheel'])('a %s on the map ends it, even if no move of its own starts', (type) => {
      const interrupted = mountIntro()
      act(() => { surface.dispatchEvent(new Event(type)) })
      expect(interrupted).toHaveBeenCalledTimes(1)
    })

    it('does not listen once it is over', () => {
      const interrupted = vi.fn()
      const { rerender } = render(<PassageMap passages={passages} playedIds={new Set()} boat={null} isDarkTheme={false} introActive onIntroInterrupt={interrupted} />)
      act(() => { (mapProps.onLoad as () => void)() })
      rerender(<PassageMap passages={passages} playedIds={new Set()} boat={null} isDarkTheme={false} introActive={false} onIntroInterrupt={interrupted} />)
      act(() => { surface.dispatchEvent(new Event('pointerdown')) })
      expect(interrupted).not.toHaveBeenCalled()
    })
  })

  it('frames flat exactly as before with 3D off', () => {
    localStorage.setItem('passages.replay3d', '0')
    renderMap()
    act(() => { (mapProps.onLoad as () => void)() })
    // MapLibre's own fit, level and facing north, through the same padding and zoom ceiling.
    expect(jumpTo).toHaveBeenLastCalledWith({ center: [148.05, -20.05], zoom: 9, pitch: 0, bearing: 0 })
    expect(fitBounds).not.toHaveBeenCalled()
  })

  it('says so and goes flat when the terrain cannot be loaded', () => {
    renderMap()
    act(() => { (mapProps.onLoad as () => void)() })
    expect(screen.queryByText('Terrain unavailable')).toBeNull()
    act(() => { (mapProps.onError as (e: unknown) => void)({ sourceId: 'terrain-dem', error: new Error('502') }) })
    expect(screen.getByRole('status').textContent).toContain('Terrain unavailable')
    expect(mapProps.terrain).toBeNull()
    expect(screen.queryByTestId('layer-terrain-hillshade')).toBeNull()
    expect(easeTo).toHaveBeenLastCalledWith(expect.objectContaining({ pitch: 0, bearing: 0 }))
    moveEnd()
    expect(mapProps.maxPitch).toBe(0)
    expect(mapProps.dragRotate).toBe(false)
    expect(mapProps.touchPitch).toBe(false)
  })

  it('tries the terrain again after a minute and clears the notice when its tiles load', () => {
    vi.useFakeTimers()
    try {
      renderMap()
      act(() => { (mapProps.onLoad as () => void)() })
      act(() => { (mapProps.onError as (e: unknown) => void)({ sourceId: 'terrain-dem', error: new Error('502') }) })
      moveEnd()
      act(() => { vi.advanceTimersByTime(59_000) })
      expect(mapProps.terrain).toBeNull()
      act(() => { vi.advanceTimersByTime(1_000) })
      expect(mapProps.terrain).toEqual({ source: 'terrain-dem', exaggeration: 1.5 })
      expect(screen.getByRole('status').textContent).toContain('Terrain unavailable')
      expect(mapProps.dragRotate).toBe(false)
      act(() => { (mapProps.onSourceData as (e: unknown) => void)({ sourceId: 'terrain-dem', tile: {} }) })
      expect(screen.queryByRole('status')).toBeNull()
      expect(mapProps.dragRotate).toBe(true)
      expect(easeTo).toHaveBeenLastCalledWith(expect.objectContaining({ pitch: 50 }))
    } finally {
      vi.useRealTimers()
    }
  })

  it('goes back to failed, with the notice, when the retry fails too', () => {
    vi.useFakeTimers()
    try {
      renderMap()
      act(() => { (mapProps.onError as (e: unknown) => void)({ sourceId: 'terrain-dem', error: new Error('502') }) })
      act(() => { vi.advanceTimersByTime(60_000) })
      act(() => { (mapProps.onError as (e: unknown) => void)({ sourceId: 'terrain-dem', error: new Error('502') }) })
      expect(mapProps.terrain).toBeNull()
      expect(screen.getByRole('status')).toBeInTheDocument()
      act(() => { vi.advanceTimersByTime(60_000) })
      expect(mapProps.terrain).toBeTruthy()
    } finally {
      vi.useRealTimers()
    }
  })

  it('ignores errors from other sources', () => {
    renderMap()
    act(() => { (mapProps.onError as (e: unknown) => void)({ sourceId: 'openseamap', error: new Error('404') }) })
    expect(screen.queryByText('Terrain unavailable')).toBeNull()
    expect(mapProps.terrain).toBeTruthy()
  })

  it('tries terrain again when 3D is switched off and on', () => {
    renderMap()
    act(() => { (mapProps.onError as (e: unknown) => void)({ sourceId: 'terrain-dem', error: new Error('502') }) })
    fireEvent.click(screen.getByRole('button', { name: 'Toggle 3D' }))
    fireEvent.click(screen.getByRole('button', { name: 'Toggle 3D' }))
    expect(screen.queryByText('Terrain unavailable')).toBeNull()
    expect(mapProps.terrain).toBeTruthy()
  })
})

describe('PassageMap progressive track', () => {
  it('shows tracks as before while not replaying', () => {
    renderMap()
    expect(screen.queryByTestId('layer-passage-speed-line-p1-0')).toBeNull()
    expect(screen.getByTestId('layer-passage-line-p1')).toBeInTheDocument()
  })

  it('draws the sailed part on top of the dim full line while replaying, with its own casing under imagery', () => {
    const sailedTo = new Map([['p1', { at: 30 }]])
    render(
      <PassageMap passages={passages} playedIds={new Set(['p1'])} boat={null} isDarkTheme={false} sailedTo={sailedTo} speed={speedFor(passages, sailedTo)} />,
    )
    expect(screen.getByTestId('source-passage-speed-p1-0')).toBeInTheDocument()
    expect(layerIds.indexOf('passage-speed-line-p1-0')).toBeGreaterThan(layerIds.indexOf('passage-line-p1'))
    expect(screen.getByTestId('layer-passage-speed-casing-p1-0').dataset.beforeId).toBe('passage-speed-line-p1-0')
  })

  it('hands the map the same sailed line again when only another passage has moved', () => {
    const two = [...passages, { id: 'p2', segments: [[{ t: 100, lat: -21, lon: 149 }, { t: 160, lat: -21.1, lon: 149.1 }]] }]
    const sailedAt = (p2: number) => new Map([['p1', { at: 30 }], ['p2', { at: p2 }]])
    const first = speedFor(two, sailedAt(110))
    const { rerender } = render(<PassageMap passages={two} playedIds={new Set()} boat={null} isDarkTheme={false} sailedTo={sailedAt(110)} speed={first} />)
    const p1 = lastData['passage-speed-p1-0']
    const p2 = lastData['passage-speed-p2-0']
    const full1 = lastData['passage-p1']
    const second = speedFrameAt(drawnPieces(two), speedTrack(drawnPieces(two))!, sailedAt(120), SPEED_RAMP_LIGHT, first, { zoom: 12 })
    rerender(<PassageMap passages={two} playedIds={new Set()} boat={null} isDarkTheme={false} sailedTo={sailedAt(120)} speed={second} />)
    expect(lastData['passage-speed-p1-0']).toBe(p1)
    expect(lastData['passage-p1']).toBe(full1)
    expect(lastData['passage-speed-p2-0']).not.toBe(p2)
  })

  it('ends the sailed line on the boat marker position', () => {
    const sailedTo = new Map([['p1', { at: 30, end: { lat: -20.05, lon: 148.04 } }]])
    render(
      <PassageMap passages={passages} playedIds={new Set()} boat={{ lat: -20.05, lon: 148.04 }} isDarkTheme={false}
        sailedTo={sailedTo} speed={speedFor(passages, sailedTo)} />,
    )
    const geo = lastData['passage-speed-p1-0'] as GeoJSON.Feature<GeoJSON.LineString>
    expect(geo.geometry.coordinates.at(-1)).toEqual([148.04, -20.05])
  })

  it('colours the sailed line by speed with a gradient along its own length, and draws a wake ending on the boat', () => {
    const sailedTo = new Map([['p1', { at: 30, end: { lat: -20.05, lon: 148.04 } }]])
    render(
      <PassageMap passages={passages} playedIds={new Set()} boat={{ lat: -20.05, lon: 148.04 }} isDarkTheme={false}
        sailedTo={sailedTo} speed={speedFor(passages, sailedTo)} />,
    )
    expect(sources['passage-speed-p1-0']).toMatchObject({ lineMetrics: true })
    const wake = lastData['replay-wake'] as GeoJSON.Feature<GeoJSON.LineString>
    expect(wake.geometry.coordinates.at(-1)).toEqual([148.04, -20.05])
    expect(screen.getByTestId('layer-replay-wake-line')).toBeInTheDocument()
  })

  it('draws the pieces it is given, rather than working them out again', () => {
    const pieces = [{ key: 'given', passageId: 'p1', segments: passages[0].segments, colourIndex: 0, from: -Infinity, to: Infinity }]
    render(<PassageMap passages={passages} pieces={pieces} playedIds={new Set()} boat={null} isDarkTheme={false} />)
    expect(screen.getByTestId('layer-passage-line-given')).toBeInTheDocument()
    expect(screen.queryByTestId('layer-passage-line-p1')).toBeNull()
  })

  it('keeps the plain sailed line for a track cut into too many segments to colour', () => {
    const many = [{ id: 'p1', segments: Array.from({ length: 13 }, (_, i) => [{ t: i * 100, lat: -20, lon: 148 + i * 0.01 }, { t: i * 100 + 60, lat: -20.01, lon: 148.005 + i * 0.01 }]) }]
    render(<PassageMap passages={many} playedIds={new Set()} boat={null} isDarkTheme={false} sailedTo={new Map([['p1', { at: 700 }]])} />)
    expect(screen.getByTestId('source-passage-sailed-p1')).toBeInTheDocument()
    expect(screen.queryByTestId('source-passage-speed-p1-0')).toBeNull()
  })
})

describe('PassageMap zoom and fit buttons', () => {
  it('zooms a step over 250 ms and fits the track', () => {
    render(<PassageMap passages={passages} playedIds={new Set()} boat={null} isDarkTheme={false} />)
    act(() => { (mapProps.onLoad as () => void)() })
    easeTo.mockClear()
    fireEvent.click(screen.getByRole('button', { name: 'Zoom in' }))
    expect(easeTo).toHaveBeenLastCalledWith({ zoom: 11, duration: 250 })
    fireEvent.click(screen.getByRole('button', { name: 'Zoom out' }))
    expect(easeTo).toHaveBeenLastCalledWith({ zoom: 9, duration: 250 })
    easeTo.mockClear()
    fireEvent.click(screen.getByRole('button', { name: 'Fit track' }))
    expect(easeTo.mock.calls.length + fitBounds.mock.calls.length).toBeGreaterThan(0)
  })

  it('greys all three while the replay plays', () => {
    render(<PassageMap passages={passages} playedIds={new Set()} boat={null} isDarkTheme={false} controlsDisabled />)
    for (const name of ['Zoom in', 'Zoom out', 'Fit track']) {
      expect(screen.getByRole('button', { name })).toBeDisabled()
    }
  })
})

describe('PassageMap light', () => {
  const dusk = lightFor({ altitudeDeg: -3, azimuthDeg: 250 })
  const mount = (light: typeof dusk | null) => (
    <PassageMap passages={passages} playedIds={new Set()} boat={null} isDarkTheme={false} light={light} />
  )

  it('paints the sky, the map light and the relief shading for the sun', () => {
    render(mount(dusk))
    act(() => { (mapProps.onLoad as () => void)() })
    expect(setSky).toHaveBeenLastCalledWith(dusk.sky)
    expect(setLight).toHaveBeenLastCalledWith(dusk.light)
    expect(setPaintProperty).toHaveBeenCalledWith('terrain-hillshade', 'hillshade-illumination-direction', dusk.hillshade.direction)
    expect(setPaintProperty).toHaveBeenCalledWith('terrain-hillshade', 'hillshade-exaggeration', dusk.hillshade.exaggeration)
  })

  it('leaves the sky to the app theme while there is no light', () => {
    render(mount(null))
    act(() => { (mapProps.onLoad as () => void)() })
    expect(setSky).toHaveBeenCalled()
    expect(setLight).not.toHaveBeenCalled()
  })

  it('still sets the light on a flat map, where there is no sky', () => {
    localStorage.setItem('passages.replay3d', '0')
    render(mount(dusk))
    act(() => { (mapProps.onLoad as () => void)() })
    expect(setSky).toHaveBeenLastCalledWith(undefined)
    expect(setLight).toHaveBeenLastCalledWith(dusk.light)
    // No relief to shade, but the satellite picture is still dimmed.
    expect(setPaintProperty).not.toHaveBeenCalledWith('terrain-hillshade', expect.anything(), expect.anything())
    expect(setPaintProperty).toHaveBeenCalledWith('world-imagery-layer', 'raster-brightness-max', dusk.raster['raster-brightness-max'])
  })

  it('lays the time of day over the base map: above the chart and the relief, below the names, the track and the wake', () => {
    const sailedTo = new Map([['p1', { at: 30, end: { lat: -20.05, lon: 148.04 } }]])
    render(
      <PassageMap passages={passages} playedIds={new Set()} boat={{ lat: -20.05, lon: 148.04 }} isDarkTheme={false} light={dusk}
        sailedTo={sailedTo} speed={speedFor(passages, sailedTo)} />,
    )
    const order = (id: string) => layerIds.indexOf(id)
    expect(order('replay-tint')).toBeGreaterThan(order('openseamap-layer'))
    expect(order('replay-tint')).toBeGreaterThan(order('terrain-hillshade'))
    expect(order('replay-tint')).toBeLessThan(order('passage-line-p1'))
    expect(order('replay-tint')).toBeLessThan(order('passage-speed-line-p1-0'))
    expect(order('replay-tint')).toBeLessThan(order('replay-wake-line'))
    expect(layerPaint['replay-tint']).toEqual({ 'background-color': dusk.tint.colour, 'background-opacity': dusk.tint.opacity * 0.6 })
  })

  it('has the tint clear by day, and clear while there is no light', () => {
    const { rerender } = render(mount(lightFor({ altitudeDeg: 40, azimuthDeg: 180 })))
    expect(layerPaint['replay-tint']?.['background-opacity']).toBe(0)
    rerender(mount(null))
    expect(layerPaint['replay-tint']?.['background-opacity']).toBe(0)
  })

  it('dims the satellite picture from the sun, and only when there is one', () => {
    const { rerender } = render(mount(dusk))
    act(() => { (mapProps.onLoad as () => void)() })
    expect(setPaintProperty).toHaveBeenCalledWith('world-imagery-layer', 'raster-brightness-max', dusk.raster['raster-brightness-max'])
    expect(setPaintProperty).toHaveBeenCalledWith('world-imagery-layer', 'raster-saturation', dusk.raster['raster-saturation'])
    setPaintProperty.mockClear()
    saveImageryPreference(false)
    rerender(<PassageMap passages={passages} playedIds={new Set()} boat={null} isDarkTheme={false} light={dusk} />)
    // The dimming is taken off with the imagery; nothing paints it again.
    expect(setPaintProperty).not.toHaveBeenCalledWith('world-imagery-layer', 'raster-brightness-max', dusk.raster['raster-brightness-max'])
  })

  it('draws the wake wider than the sailed line, as foam fading to nothing at its tail', () => {
    const sailedTo = new Map([['p1', { at: 30, end: { lat: -20.05, lon: 148.04 } }]])
    render(
      <PassageMap passages={passages} playedIds={new Set()} boat={{ lat: -20.05, lon: 148.04 }} isDarkTheme={false}
        sailedTo={sailedTo} speed={speedFor(passages, sailedTo)} />,
    )
    const wake = layerPaint['replay-wake-line']!
    const line = layerPaint['passage-speed-line-p1-0']!
    expect(wake['line-width'] as number).toBeGreaterThan(line['line-width'] as number)
    const gradient = wake['line-gradient'] as unknown[]
    expect(String(gradient[4])).toMatch(/^rgba\(\d+, \d+, \d+, 0\)$/)
    expect(String(gradient[6])).toMatch(/, 0\.95\)$/)
  })

  it('does not touch the map again while the light is the same', () => {
    const { rerender } = render(mount(dusk))
    act(() => { (mapProps.onLoad as () => void)() })
    const calls = setLight.mock.calls.length
    rerender(mount(dusk))
    rerender(mount(dusk))
    expect(setLight.mock.calls.length).toBe(calls)
    rerender(mount(lightFor({ altitudeDeg: 30, azimuthDeg: 180 })))
    // The old light is put back to the map's own, then the new one is set.
    expect(setLight.mock.calls.length).toBe(calls + 2)
  })
})

describe('PassageMap projection', () => {
  const { projection: blend, flatAtZoom } = introProjectionAt({ center: [149, -20], zoom: 9, pitch: 50, bearing: 0 }, 0)

  it('takes the projection it is given', () => {
    const ref = createRef<PassageMapHandle>()
    render(<PassageMap ref={ref} passages={passages} playedIds={new Set()} boat={null} isDarkTheme={false} />)
    ref.current?.setProjection(blend, flatAtZoom)
    expect(setProjection).toHaveBeenLastCalledWith(blend)
  })

  it('goes straight to the flat map when the move ends before it has landed, at any zoom', () => {
    const ref = createRef<PassageMapHandle>()
    const { rerender } = render(<PassageMap ref={ref} passages={passages} playedIds={new Set()} boat={null} isDarkTheme={false} introActive />)
    act(() => { (mapProps.onLoad as () => void)() })
    ref.current?.setProjection(blend, flatAtZoom)
    setProjection.mockClear()
    // Mid-move the map is left as it is, whatever it does.
    act(() => { mapHandlers.zoomend?.forEach((f) => f({})) })
    expect(setProjection).not.toHaveBeenCalled()
    // A hand took the map: the move is over, and the map is the flat one.
    rerender(<PassageMap ref={ref} passages={passages} playedIds={new Set()} boat={null} isDarkTheme={false} introActive={false} />)
    expect(setProjection).toHaveBeenCalledTimes(1)
    expect(setProjection).toHaveBeenCalledWith({ type: 'mercator' })
  })

  it('leaves a landed map alone: it is flat already', () => {
    const ref = createRef<PassageMapHandle>()
    const { rerender } = render(<PassageMap ref={ref} passages={passages} playedIds={new Set()} boat={null} isDarkTheme={false} introActive />)
    act(() => { (mapProps.onLoad as () => void)() })
    ref.current?.setProjection({ type: 'mercator' }, null)
    setProjection.mockClear()
    rerender(<PassageMap ref={ref} passages={passages} playedIds={new Set()} boat={null} isDarkTheme={false} introActive={false} />)
    expect(setProjection).not.toHaveBeenCalled()
  })
})

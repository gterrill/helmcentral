import { act, fireEvent, render, screen } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { createRef } from 'react'

import { PassageMap, type PassageMapHandle } from '@/components/passages/passage-map'
import { load3dPreference, save3dPreference } from '@/lib/passage-3d-preference'
import { saveImageryPreference } from '@/lib/passage-imagery-preference'

vi.mock('maplibre-gl', () => ({ default: {} }))
vi.mock('@/lib/webgl', () => ({ hasWebGL2: () => true }))

const easeTo = vi.fn()
const fitBounds = vi.fn()
const jumpTo = vi.fn()
let onceHandlers: (() => void)[] = []
const moveEnd = () => { const h = onceHandlers; onceHandlers = []; act(() => h.forEach((f) => f())) }
let mapProps: Record<string, unknown> = {}
let sources: Record<string, Record<string, unknown>> = {}
const lastData: Record<string, unknown> = {}
let layerIds: string[] = []

vi.mock('react-map-gl/maplibre', async () => {
  const React = await import('react')
  return {
    Map: React.forwardRef((props: Record<string, unknown> & { children?: React.ReactNode }, ref: React.Ref<unknown>) => {
      mapProps = props
      React.useImperativeHandle(ref, () => ({
        getMap: () => ({
          getSource: () => ({}),
          easeTo,
          fitBounds,
          jumpTo,
          once: (_: string, cb: () => void) => { onceHandlers.push(cb) },
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
    Layer: ({ id, beforeId }: { id: string; beforeId?: string }) => {
      layerIds.push(id)
      return <div data-testid={`layer-${id}`} data-before-id={beforeId} />
    },
  }
})

vi.mock('@/components/map-place-labels', () => ({
  MapPlaceLabels: () => null,
  warnIfBaseVectorSourceMissing: () => {},
}))

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

  it('flies in to the same pitched, fitted camera', () => {
    const ref = createRef<PassageMapHandle>()
    render(<PassageMap ref={ref} passages={passages} playedIds={new Set()} boat={null} isDarkTheme={false} />)
    act(() => { void ref.current?.flyIn() })
    const cam = easeTo.mock.calls.at(-1)?.[0]
    expect(cam).toMatchObject({ pitch: 50, bearing: 0, duration: 3200 })
    expect(cam.zoom).toBeLessThanOrEqual(9)
    expect(fitBounds).not.toHaveBeenCalled()
  })

  it('frames flat exactly as before with 3D off', () => {
    localStorage.setItem('passages.replay3d', '0')
    renderMap()
    act(() => { (mapProps.onLoad as () => void)() })
    expect(jumpTo).not.toHaveBeenCalled()
    expect(fitBounds).toHaveBeenLastCalledWith(expect.anything(), expect.not.objectContaining({ pitch: expect.anything() }))
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
    expect(screen.queryByTestId('layer-passage-sailed-line-p1')).toBeNull()
    expect(screen.getByTestId('layer-passage-line-p1')).toBeInTheDocument()
  })

  it('draws the sailed part on top of the dim full line while replaying, with its own casing under imagery', () => {
    render(
      <PassageMap passages={passages} playedIds={new Set(['p1'])} boat={null} isDarkTheme={false} sailedTo={new Map([['p1', { at: 30 }]])} />,
    )
    expect(screen.getByTestId('source-passage-sailed-p1')).toBeInTheDocument()
    expect(layerIds.indexOf('passage-sailed-line-p1')).toBeGreaterThan(layerIds.indexOf('passage-line-p1'))
    expect(screen.getByTestId('layer-passage-sailed-casing-p1').dataset.beforeId).toBe('passage-sailed-line-p1')
  })

  it('hands the map the same sailed line again when only another passage has moved', () => {
    const two = [...passages, { id: 'p2', segments: [[{ t: 100, lat: -21, lon: 149 }, { t: 160, lat: -21.1, lon: 149.1 }]] }]
    const sailedAt = (p2: number) => new Map([['p1', { at: 30 }], ['p2', { at: p2 }]])
    const { rerender } = render(<PassageMap passages={two} playedIds={new Set()} boat={null} isDarkTheme={false} sailedTo={sailedAt(110)} />)
    const p1 = lastData['passage-sailed-p1']
    const p2 = lastData['passage-sailed-p2']
    const full1 = lastData['passage-p1']
    rerender(<PassageMap passages={two} playedIds={new Set()} boat={null} isDarkTheme={false} sailedTo={sailedAt(120)} />)
    expect(lastData['passage-sailed-p1']).toBe(p1)
    expect(lastData['passage-p1']).toBe(full1)
    expect(lastData['passage-sailed-p2']).not.toBe(p2)
  })

  it('ends the sailed line on the boat marker position', () => {
    render(
      <PassageMap passages={passages} playedIds={new Set()} boat={{ lat: -20.05, lon: 148.04 }} isDarkTheme={false}
        sailedTo={new Map([['p1', { at: 30, end: { lat: -20.05, lon: 148.04 } }]])} />,
    )
    const geo = lastData['passage-sailed-p1'] as GeoJSON.Feature<GeoJSON.MultiLineString>
    expect(geo.geometry.coordinates[0].at(-1)).toEqual([148.04, -20.05])
  })
})

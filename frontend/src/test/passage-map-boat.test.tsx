import { act, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { PassageMap } from '@/components/passages/passage-map'
import { save3dPreference } from '@/lib/passage-3d-preference'
import { saveImageryPreference } from '@/lib/passage-imagery-preference'

vi.mock('maplibre-gl', () => ({ default: {} }))
vi.mock('@/lib/webgl', () => ({ hasWebGL2: () => true }))

const { addLayer, removeLayer, triggerRepaint, setPose, createBoatLayer, mapOn } = vi.hoisted(() => {
  const setPose = vi.fn()
  return {
    addLayer: vi.fn(),
    removeLayer: vi.fn(),
    triggerRepaint: vi.fn(),
    mapOn: vi.fn(),
    setPose,
    createBoatLayer: vi.fn((hull: unknown) => ({ id: 'passage-boat-model', type: 'custom', hull, setPose })),
  }
})
let present = new Set<string>()
let mapProps: Record<string, unknown> = {}

vi.mock('react-map-gl/maplibre', async () => {
  const React = await import('react')
  const map = {
    getSource: () => ({}),
    easeTo: vi.fn(),
    fitBounds: vi.fn(),
    jumpTo: vi.fn(),
    once: vi.fn(),
    on: mapOn,
    off: vi.fn(),
    cameraForBounds: () => ({ center: { lng: 148.05, lat: -20.05 }, zoom: 9 }),
    getContainer: () => ({ clientWidth: 800, clientHeight: 400 }),
    getLayer: (id: string) => (present.has(id) ? {} : undefined),
    addLayer: (l: { id: string }) => { addLayer(l); present.add(l.id) },
    removeLayer: (id: string) => { present.delete(id); removeLayer(id) },
    triggerRepaint,
  }
  return {
    Map: React.forwardRef((props: Record<string, unknown> & { children?: React.ReactNode }, ref: React.Ref<unknown>) => {
      mapProps = props
      React.useImperativeHandle(ref, () => ({ getMap: () => map, getCanvas: () => ({}) }))
      return <div>{props.children}</div>
    }),
    Marker: ({ children }: { children?: React.ReactNode }) => <div>{children}</div>,
    Source: ({ children }: { children?: React.ReactNode }) => <div>{children}</div>,
    Layer: () => null,
  }
})
vi.mock('@/components/map-place-labels', () => ({ MapPlaceLabels: () => null, warnIfBaseVectorSourceMissing: () => {} }))

vi.mock('@/lib/boat-layer', () => ({ createBoatLayer: (h: unknown) => createBoatLayer(h) }))

const passages = [{ id: 'p1', segments: [[{ t: 0, lat: -20, lon: 148 }, { t: 60, lat: -20.1, lon: 148.1 }]] }]
const hull = { type: 'sail_cat' as const, loaM: 14, beamM: 7 }
const boat = { lat: -20.05, lon: 148.05 }

function renderMap(props: Partial<React.ComponentProps<typeof PassageMap>> = {}) {
  const view = render(<PassageMap passages={passages} playedIds={new Set()} boat={boat} boatHeading={90} hull={hull} isDarkTheme={false} {...props} />)
  act(() => { (mapProps.onLoad as () => void)() })
  return view
}

beforeEach(() => {
  localStorage.clear()
  save3dPreference(true)
  saveImageryPreference(true)
  addLayer.mockClear(); removeLayer.mockClear(); setPose.mockClear(); mapOn.mockClear(); createBoatLayer.mockClear(); triggerRepaint.mockClear()
  present = new Set()
})

describe('PassageMap boat model', () => {
  it('shows the dot until the model has loaded, then the model replaces it', async () => {
    renderMap()
    await waitFor(() => expect(addLayer).toHaveBeenCalledTimes(1))
    expect(createBoatLayer).toHaveBeenCalledWith({ type: 'sail_cat', loaM: 14, beamM: 7 })
    await waitFor(() => expect(screen.queryByTestId('passage-boat-marker')).toBeNull())
  })

  it('hands the model each position and heading', async () => {
    const { rerender } = renderMap()
    await waitFor(() => expect(setPose).toHaveBeenCalledWith({ lat: -20.05, lon: 148.05, headingDeg: 90 }))
    rerender(<PassageMap passages={passages} playedIds={new Set()} boat={{ lat: -20.06, lon: 148.06 }} boatHeading={95} hull={hull} isDarkTheme={false} />)
    await waitFor(() => expect(setPose).toHaveBeenLastCalledWith({ lat: -20.06, lon: 148.06, headingDeg: 95 }))
  })

  it('keeps the dot and loads nothing when no hull type is set', () => {
    renderMap({ hull: null })
    expect(screen.getByTestId('passage-boat-marker')).toBeInTheDocument()
    expect(createBoatLayer).not.toHaveBeenCalled()
  })

  it('keeps the dot and loads nothing with 3D off', () => {
    save3dPreference(false)
    renderMap()
    expect(screen.getByTestId('passage-boat-marker')).toBeInTheDocument()
    expect(createBoatLayer).not.toHaveBeenCalled()
  })

  it('goes back to the dot and removes the layer when the terrain fails', async () => {
    renderMap()
    await waitFor(() => expect(addLayer).toHaveBeenCalled())
    act(() => { (mapProps.onError as (e: unknown) => void)({ sourceId: 'terrain-dem', error: new Error('502') }) })
    expect(removeLayer).toHaveBeenCalledWith('passage-boat-model')
    expect(screen.getByTestId('passage-boat-marker')).toBeInTheDocument()
  })

  it('removes the layer when the map goes away', async () => {
    const { unmount } = renderMap()
    await waitFor(() => expect(addLayer).toHaveBeenCalled())
    unmount()
    expect(removeLayer).toHaveBeenCalledWith('passage-boat-model')
  })

  it('takes the model off the map when the boat has no position', async () => {
    const { rerender } = renderMap()
    await waitFor(() => expect(setPose).toHaveBeenCalled())
    rerender(<PassageMap passages={passages} playedIds={new Set()} boat={null} boatHeading={null} hull={hull} isDarkTheme={false} />)
    await waitFor(() => expect(setPose).toHaveBeenLastCalledWith(null))
    expect(screen.queryByTestId('passage-boat-marker')).toBeNull()
  })

  it('puts the layer back after a style change', async () => {
    renderMap()
    await waitFor(() => expect(addLayer).toHaveBeenCalledTimes(1))
    present.clear() // a new style drops custom layers
    const handler = mapOn.mock.calls.find((c) => c[0] === 'styledata')?.[1] as () => void
    act(() => handler())
    expect(addLayer).toHaveBeenCalledTimes(2)
  })

  it('says so and shows the dot when the layer cannot be put back', async () => {
    const error = vi.spyOn(console, 'error').mockImplementation(() => {})
    renderMap()
    await waitFor(() => expect(screen.queryByTestId('passage-boat-marker')).toBeNull())
    present.clear()
    addLayer.mockImplementationOnce(() => { throw new Error('style not ready') })
    const handler = mapOn.mock.calls.find((c) => c[0] === 'styledata')?.[1] as () => void
    act(() => handler())
    expect(error).toHaveBeenCalledWith('Boat model could not be added to the map', expect.any(Error))
    expect(screen.getByTestId('passage-boat-marker')).toBeInTheDocument()
    error.mockRestore()
  })
})

import { act, render } from '@testing-library/react'
import { createRef } from 'react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { PassageMap, type CameraDriver, type PassageMapHandle } from '@/components/passages/passage-map'
import { save3dPreference } from '@/lib/passage-3d-preference'
import { saveImageryPreference } from '@/lib/passage-imagery-preference'
import type { Camera } from '@/lib/replay-camera'
import { SKY_DAY } from '@/lib/terrain'

vi.mock('maplibre-gl', () => ({ default: {} }))
vi.mock('@/lib/webgl', () => ({ hasWebGL2: () => true }))

const m = vi.hoisted(() => ({
  jumpTo: vi.fn(),
  handlers: {} as Record<string, (e: unknown) => void>,
  projected: { x: 400, y: 200 },
  props: {} as Record<string, unknown>,
  surface: null as unknown as HTMLElement,
  setSky: vi.fn(),
  current: { center: { lng: 0, lat: 0 }, zoom: 3, pitch: 0, bearing: 0 },
}))

vi.mock('react-map-gl/maplibre', async () => {
  const React = await import('react')
  const map = {
    getSource: () => ({}),
    easeTo: vi.fn(),
    fitBounds: vi.fn(),
    jumpTo: m.jumpTo,
    once: vi.fn(),
    on: (name: string, cb: (e: unknown) => void) => { m.handlers[name] = cb },
    off: (name: string) => { delete m.handlers[name] },
    project: () => m.projected,
    getZoom: () => m.current.zoom,
    getPitch: () => m.current.pitch,
    getBearing: () => m.current.bearing,
    getCenter: () => m.current.center,
    getCanvasContainer: () => m.surface,
    setSky: m.setSky,
    cameraForBounds: () => ({ center: { lng: 148.05, lat: -20.05 }, zoom: 9 }),
    getContainer: () => ({ clientWidth: 800, clientHeight: 400 }),
    getLayer: () => undefined,
  }
  return {
    Map: React.forwardRef((props: Record<string, unknown> & { children?: React.ReactNode }, ref: React.Ref<unknown>) => {
      m.props = props
      React.useImperativeHandle(ref, () => ({ getMap: () => map, getCanvas: () => ({}) }))
      return <div>{props.children}</div>
    }),
    Marker: () => null,
    Source: ({ children }: { children?: React.ReactNode }) => <div>{children}</div>,
    Layer: () => null,
  }
})
vi.mock('@/components/map-place-labels', () => ({ MapPlaceLabels: () => null, warnIfBaseVectorSourceMissing: () => {} }))

const passages = [{ id: 'p1', segments: [[{ t: 0, lat: -20, lon: 148 }, { t: 60, lat: -20.1, lon: 148.1 }]] }]
const boat = { lat: -20.05, lon: 148.05 }
const cam = (zoom: number): Camera => ({ center: [148.05, -20.05], zoom, pitch: 55, bearing: 10 })

function driver(over: Partial<CameraDriver> & { zoom?: number; shotKey?: string } = {}): CameraDriver {
  const { zoom = 14, shotKey = 'a', ...rest } = over
  return { clock: 1, reduced: false, resume: 0, resolve: () => ({ camera: cam(zoom), shotKey, mode: 'follow' }), ...rest }
}

function setup(d: CameraDriver | null, extra: { ref?: React.Ref<PassageMapHandle> } = {}) {
  const el = (cd: CameraDriver | null) => (
    <PassageMap ref={extra.ref} passages={passages} playedIds={new Set()} boat={boat} cameraDriver={cd} isDarkTheme={false} />
  )
  const view = render(el(d))
  act(() => { (m.props.onLoad as () => void)() })
  // Loading frames the whole track itself; the camera shots come after.
  m.jumpTo.mockClear()
  return { rerender: (cd: CameraDriver | null) => view.rerender(el(cd)) }
}

beforeEach(() => {
  localStorage.clear()
  m.surface = document.createElement('div')
  m.setSky.mockClear()
  m.current = { center: { lng: 0, lat: 0 }, zoom: 12.5, pitch: 48, bearing: 33 }
  save3dPreference(true)
  saveImageryPreference(false)
  m.jumpTo.mockClear()
  m.handlers = {}
  m.projected = { x: 400, y: 200 }
})

describe('PassageMap camera shots', () => {
  it('places the camera each time the replay clock moves', () => {
    const { rerender } = setup(driver({ clock: 1, zoom: 14 }))
    m.jumpTo.mockClear()
    rerender(driver({ clock: 2, zoom: 15 }))
    expect(m.jumpTo).toHaveBeenLastCalledWith(cam(15))
    rerender(driver({ clock: 3, zoom: 16 }))
    expect(m.jumpTo).toHaveBeenLastCalledWith(cam(16))
  })

  it('does not move the camera when only the shots change under a paused replay', () => {
    const { rerender } = setup(driver({ clock: 1, zoom: 14 }))
    m.jumpTo.mockClear()
    rerender(driver({ clock: 1, zoom: 18 }))
    expect(m.jumpTo).not.toHaveBeenCalled()
  })

  it('leaves the camera alone with no driver', () => {
    setup(null)
    expect(m.jumpTo).not.toHaveBeenCalled()
  })

  const fire = (type: string, init: { x?: number; y?: number; id?: number } = {}) => {
    const e = new MouseEvent(type, { clientX: init.x ?? 0, clientY: init.y ?? 0, bubbles: true })
    Object.defineProperty(e, 'pointerId', { value: init.id ?? 1 })
    act(() => { m.surface.dispatchEvent(e) })
  }
  const drag = (to = 40) => { fire('pointerdown', { x: 10, y: 10 }); fire('pointermove', { x: 10 + to, y: 10 }); fire('pointerup', { x: 10 + to, y: 10 }) }

  it('yields to the operator dragging the map until the next shot starts', () => {
    const { rerender } = setup(driver({ clock: 1 }))
    rerender(driver({ clock: 1.5 }))
    drag()
    m.jumpTo.mockClear()
    rerender(driver({ clock: 2, zoom: 15 }))
    expect(m.jumpTo).not.toHaveBeenCalled()
    rerender(driver({ clock: 3, zoom: 16, shotKey: 'b' }))
    expect(m.jumpTo).toHaveBeenCalledWith(cam(16))
  })

  it('yields until Play or a scrub hands the camera back', () => {
    const { rerender } = setup(driver({ clock: 1 }))
    rerender(driver({ clock: 1.5 }))
    drag()
    m.jumpTo.mockClear()
    rerender(driver({ clock: 2, zoom: 15 }))
    expect(m.jumpTo).not.toHaveBeenCalled()
    rerender(driver({ clock: 3, zoom: 17, resume: 1 }))
    expect(m.jumpTo).toHaveBeenCalledWith(cam(17))
  })

  it('a tap or click does not take the camera', () => {
    const { rerender } = setup(driver({ clock: 1 }))
    rerender(driver({ clock: 1.5 }))
    fire('pointerdown', { x: 10, y: 10 })
    fire('pointermove', { x: 12, y: 11 })
    fire('pointerup', { x: 12, y: 11 })
    m.jumpTo.mockClear()
    rerender(driver({ clock: 2, zoom: 15 }))
    expect(m.jumpTo).toHaveBeenCalledWith(cam(15))
  })

  it('the wheel, a second finger, or a person-made movestart take the camera', () => {
    const cases: [string, () => void][] = [
      ['wheel', () => fire('wheel')],
      ['pinch', () => { fire('pointerdown', { id: 1 }); fire('pointerdown', { id: 2, x: 50 }) }],
      ['movestart', () => act(() => { m.handlers.movestart({ originalEvent: new Event('x') }) })],
    ]
    for (const [name, trigger] of cases) {
      const { rerender } = setup(driver({ clock: 1, shotKey: name }))
      rerender(driver({ clock: 1.5, shotKey: name }))
      trigger()
      m.jumpTo.mockClear()
      rerender(driver({ clock: 2, shotKey: name, zoom: 15 }))
      expect(m.jumpTo, name).not.toHaveBeenCalled()
    }
  })

  it('does not mistake its own camera moves for the operator', () => {
    const { rerender } = setup(driver({ clock: 1 }))
    act(() => { m.handlers.movestart({}) })
    m.jumpTo.mockClear()
    rerender(driver({ clock: 2, zoom: 15 }))
    expect(m.jumpTo).toHaveBeenCalledTimes(1)
  })

  it('hands the driver the map\'s camera as it stands', () => {
    const resolve = vi.fn<CameraDriver['resolve']>(() => ({ camera: cam(14), shotKey: 'a', mode: 'follow' }))
    const { rerender } = setup(driver({ resolve }))
    rerender(driver({ resolve, clock: 2 }))
    expect(resolve).toHaveBeenLastCalledWith(expect.objectContaining({ current: { center: [0, 0], zoom: 12.5, pitch: 48, bearing: 33 } }))
  })

  it('does not place a camera that is already there', () => {
    const { rerender } = setup(driver({ clock: 1 }))
    m.current = { center: { lng: 148.05, lat: -20.05 }, zoom: 14, pitch: 55, bearing: 10 }
    rerender(driver({ clock: 2, zoom: 14 }))
    expect(m.jumpTo).not.toHaveBeenCalled()
  })

  it('asks for a flat camera when 3D is off', () => {
    save3dPreference(false)
    const resolve = vi.fn<CameraDriver['resolve']>(() => ({ camera: cam(14), shotKey: 'a', mode: 'follow' }))
    const { rerender } = setup(driver({ resolve }))
    rerender(driver({ resolve, clock: 2 }))
    expect(resolve).toHaveBeenCalledWith(expect.objectContaining({ flat: true }))
  })

  describe('under reduced motion', () => {
    it('cuts at the start of a shot and then holds still', () => {
      const { rerender } = setup(driver({ clock: 1, reduced: true, shotKey: 'a' }))
      rerender(driver({ clock: 2, reduced: true, shotKey: 'a', zoom: 15 }))
      expect(m.jumpTo).toHaveBeenCalledTimes(1)
      m.jumpTo.mockClear()
      rerender(driver({ clock: 3, reduced: true, shotKey: 'a', zoom: 16 }))
      expect(m.jumpTo).not.toHaveBeenCalled()
      rerender(driver({ clock: 4, reduced: true, shotKey: 'b', zoom: 17 }))
      expect(m.jumpTo).toHaveBeenCalledTimes(1)
    })

    it('cuts again when the boat leaves the central part of the view', () => {
      const { rerender } = setup(driver({ clock: 1, reduced: true }))
      rerender(driver({ clock: 1.5, reduced: true }))
      m.jumpTo.mockClear()
      m.projected = { x: 100, y: 200 } // 800 x 400 view: central 60% is x 160..640
      rerender(driver({ clock: 2, reduced: true, zoom: 15 }))
      expect(m.jumpTo).toHaveBeenCalledWith(cam(15))
      m.jumpTo.mockClear()
      m.projected = { x: 400, y: 200 }
      rerender(driver({ clock: 3, reduced: true, zoom: 16 }))
      expect(m.jumpTo).not.toHaveBeenCalled()
    })
  })

  it('reports the camera as the operator has it', () => {
    const ref = createRef<PassageMapHandle>()
    setup(null, { ref })
    expect(ref.current!.getView()).toEqual({ zoom: 12.5, pitch: 48, bearing: 33 })
  })

  it('reports no pitch while the map is flat', () => {
    save3dPreference(false)
    const ref = createRef<PassageMapHandle>()
    setup(null, { ref })
    expect(ref.current!.getView()).toEqual({ zoom: 12.5, pitch: null, bearing: 33 })
  })

  it('an overview shot is not re-cut at the edge under reduced motion', () => {
    const ov = (zoom: number) => ({ clock: 1, reduced: true, resolve: () => ({ camera: cam(zoom), shotKey: 'o', mode: 'overview' as const }) })
    const { rerender } = setup(driver({ ...ov(8), clock: 1 }))
    rerender(driver({ ...ov(8), clock: 1.5 }))
    m.projected = { x: 10, y: 10 }
    m.jumpTo.mockClear()
    rerender(driver({ ...ov(9), clock: 2 }))
    expect(m.jumpTo).not.toHaveBeenCalled()
  })

  describe('sky and flat reporting', () => {
    it('puts a sky over the tilted map', () => {
      setup(null)
      expect(m.setSky).toHaveBeenLastCalledWith(expect.objectContaining({ 'sky-color': SKY_DAY['sky-color'] }))
    })

    it('takes the sky away when the map is flat', () => {
      save3dPreference(false)
      setup(null)
      expect(m.setSky).toHaveBeenLastCalledWith(undefined)
    })

    it('tells the panel when the camera is flat and when it is not', () => {
      const seen: boolean[] = []
      render(<PassageMap passages={passages} playedIds={new Set()} boat={boat} isDarkTheme onFlatChange={(f) => seen.push(f)} />)
      expect(seen).toEqual([false])
    })

    it('re-applies the sky after a style change', () => {
      setup(null)
      m.setSky.mockClear()
      act(() => { m.handlers['style.load'](undefined) })
      expect(m.setSky).toHaveBeenCalledTimes(1)
    })
  })
})

import { act, fireEvent, render, screen } from '@testing-library/react'
import { forwardRef, useEffect, useImperativeHandle, type ReactNode } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import OfflineCoastlineMapImpl from '@/components/offline-coastline-map-impl'
import type { OfflineCoastlineStatus } from '@/lib/offline-coastline'

// MapLibre needs WebGL, which jsdom has not got. The stand-in map is a flat
// plane where 1 px is 0.1°, so a 1° square is 10 px wide and high.
const fake = vi.hoisted(() => ({
  dragPan: { enable: vi.fn(), disable: vi.fn() },
  cooperativeGestures: { enable: vi.fn(), disable: vi.fn() },
  fitBounds: vi.fn(),
  moving: false,
  stop: vi.fn(),
  selectable: { lat_min: -89, lat_max: 88, lon_min: -180, lon_max: 179 },
  span: 10,
  onMoveEnd: undefined as undefined | (() => void),
  onDragEnd: undefined as undefined | (() => void),
  mapProps: {} as Record<string, unknown>,
}))

vi.mock('react-map-gl/maplibre', () => {
  const map = {
    unproject: ([x, y]: [number, number]) => ({ lng: x / 10, lat: y / 10 }),
    getBounds: () => ({ getWest: () => 0, getSouth: () => 0, getEast: () => fake.span, getNorth: () => fake.span }),
    isMoving: () => fake.moving,
    stop: fake.stop,
    dragPan: fake.dragPan,
    cooperativeGestures: fake.cooperativeGestures,
    fitBounds: fake.fitBounds,
    getContainer: () => document.createElement('div'),
  }
  const Map = forwardRef(function Map(props: { children?: ReactNode; onLoad?: () => void; onMoveEnd?: () => void; onDragEnd?: () => void }, ref) {
    fake.mapProps = props
    fake.onMoveEnd = props.onMoveEnd
    fake.onDragEnd = props.onDragEnd
    useImperativeHandle(ref, () => ({ getMap: () => map }))
    useEffect(() => {
      props.onLoad?.()
      // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [])
    return <div data-testid="map">{props.children}</div>
  })
  return {
    Map,
    Source: ({ children }: { children?: ReactNode }) => <div>{children}</div>,
    Layer: () => null,
    Marker: ({ children }: { children?: ReactNode }) => <div>{children}</div>,
    NavigationControl: () => null,
  }
})
vi.mock('maplibre-gl', () => ({ default: {} }))
vi.mock('maplibre-gl/dist/maplibre-gl.css', () => ({}))
vi.mock('@/lib/webgl', () => ({ hasWebGL2: () => true }))
vi.mock('@/hooks/use-collapsed-map-attribution', () => ({ useCollapsedMapAttribution: () => () => {} }))

const TOKENS = ['--primary', '--foreground', '--muted-foreground', '--destructive', '--coverage-requested']

function status(): OfflineCoastlineStatus {
  return {
    enabled: true,
    selectable: fake.selectable,
    state: 'idle',
    has_fix: true,
    radius_nm: 200,
    squares: [],
    held: 0,
    wanted: 0,
    wanted_held: 0,
    inland_skipped: 0,
    failed: 0,
    edits_since: 0,
    squares_with_edits: 0,
    updating: 0,
  }
}

interface Init { x: number; y: number; id?: number; primary?: boolean; type?: string; shift?: boolean }
const init = ({ x, y, id = 1, primary = true, type = 'touch', shift = false }: Init) => ({
  clientX: x,
  clientY: y,
  pointerId: id,
  isPrimary: primary,
  pointerType: type,
  shiftKey: shift,
  button: 0,
  bubbles: true,
})

function setup(selected: string[] = []) {
  const onSelectedChange = vi.fn()
  const ui = (sel: string[], selecting = true) => (
    <OfflineCoastlineMapImpl status={status()} selecting={selecting} selected={new Set(sel)} onSelectedChange={onSelectedChange} />
  )
  const { container, rerender, unmount } = render(ui(selected))
  const el = container.firstElementChild as HTMLElement
  return {
    onSelectedChange,
    el,
    unmount,
    setSelectable: (v: typeof fake.selectable) => {
      fake.selectable = v
    },
    reselect: (sel: string[], selecting = true) => rerender(ui(sel, selecting)),
    cancel: (i: Init) => fireEvent.pointerCancel(el, init(i)),
    down: (i: Init) => fireEvent.pointerDown(el, init(i)),
    move: (i: Init) => fireEvent.pointerMove(el, init(i)),
    up: (i: Init) => fireEvent.pointerUp(el, init(i)),
    windowUp: (i: Init) => fireEvent.pointerUp(window, init(i)),
    hold: () => act(() => { vi.advanceTimersByTime(400) }),
  }
}

const committed = (fn: ReturnType<typeof vi.fn>) => [...(fn.mock.calls.at(-1)![0] as Set<string>)].sort()

beforeEach(() => {
  vi.useFakeTimers()
  for (const t of TOKENS) document.documentElement.style.setProperty(t, '10 20% 30%')
  fake.dragPan.enable.mockClear()
  fake.dragPan.disable.mockClear()
  fake.moving = false
  fake.stop.mockClear()
  fake.selectable = { lat_min: -89, lat_max: 88, lon_min: -180, lon_max: 179 }
  fake.span = 10
})
afterEach(() => {
  vi.useRealTimers()
})

describe('OfflineCoastlineMapImpl select gestures', () => {
  it('a tap toggles a square on, and off again', () => {
    const t = setup()
    t.down({ x: 25, y: 25 })
    t.up({ x: 25, y: 25 })
    expect(committed(t.onSelectedChange)).toEqual(['2,2'])
    t.reselect(['2,2'])
    t.down({ x: 25, y: 25 })
    t.up({ x: 25, y: 25 })
    expect(committed(t.onSelectedChange)).toEqual([])
  })

  it('a tap on a selected square drops it', () => {
    const t = setup(['2,2', '5,5'])
    t.down({ x: 25, y: 25 })
    t.up({ x: 25, y: 25 })
    expect(committed(t.onSelectedChange)).toEqual(['5,5'])
  })

  it('a move of more than 8 px before the hold is a pan: nothing selected, the map keeps the drag', () => {
    const t = setup()
    t.down({ x: 25, y: 25 })
    t.move({ x: 36, y: 25 })
    t.hold()
    t.move({ x: 75, y: 55 })
    t.up({ x: 75, y: 55 })
    expect(t.onSelectedChange).not.toHaveBeenCalled()
    expect(fake.dragPan.disable).not.toHaveBeenCalled()
  })

  it('press, hold 400 ms, then drag selects the block, and the map pans again afterwards', () => {
    const t = setup()
    t.down({ x: 25, y: 25 })
    t.hold()
    expect(fake.dragPan.disable).toHaveBeenCalledTimes(1)
    t.move({ x: 45, y: 35 })
    t.up({ x: 45, y: 35 })
    expect(committed(t.onSelectedChange)).toEqual(['2,2', '2,3', '2,4', '3,2', '3,3', '3,4'])
    expect(fake.dragPan.enable).toHaveBeenCalledTimes(1)
  })

  it('Shift with a mouse starts the block at once', () => {
    const t = setup()
    t.down({ x: 25, y: 25, type: 'mouse', shift: true })
    expect(fake.dragPan.disable).toHaveBeenCalledTimes(1)
    t.move({ x: 45, y: 25, type: 'mouse', shift: true })
    t.up({ x: 45, y: 25, type: 'mouse', shift: true })
    expect(committed(t.onSelectedChange)).toEqual(['2,2', '2,3', '2,4'])
  })

  it('a hold released without moving adds the square', () => {
    const t = setup()
    t.down({ x: 25, y: 25 })
    t.hold()
    t.up({ x: 25, y: 25 })
    expect(committed(t.onSelectedChange)).toEqual(['2,2'])
  })

  it('a hold released without moving never drops a square that is already selected', () => {
    const t = setup(['2,2'])
    t.down({ x: 25, y: 25 })
    t.hold()
    t.up({ x: 25, y: 25 })
    expect(committed(t.onSelectedChange)).toEqual(['2,2'])
  })

  it('the block starts at the square under the finger when the hold takes, not where it first touched', () => {
    const t = setup()
    t.down({ x: 28, y: 25 })
    t.move({ x: 31, y: 25 }) // 3 px: still inside the slop
    t.hold()
    t.up({ x: 31, y: 25 })
    expect(committed(t.onSelectedChange)).toEqual(['2,3'])
  })

  it('the hold buzzes the phone when it can', () => {
    const vibrate = vi.fn()
    Object.defineProperty(navigator, 'vibrate', { value: vibrate, configurable: true })
    try {
      const t = setup()
      t.down({ x: 25, y: 25 })
      t.hold()
      expect(vibrate).toHaveBeenCalledWith(10)
    } finally {
      delete (navigator as unknown as Record<string, unknown>).vibrate
    }
  })

  it('a release outside the map while pending cancels the hold', () => {
    const t = setup()
    t.down({ x: 25, y: 25 })
    t.windowUp({ x: 500, y: 500 })
    t.hold()
    expect(fake.dragPan.disable).not.toHaveBeenCalled()
    expect(t.onSelectedChange).not.toHaveBeenCalled()
  })

  it('a second finger cancels the hold', () => {
    const t = setup()
    t.down({ x: 25, y: 25 })
    t.down({ x: 60, y: 60, id: 2, primary: false })
    t.hold()
    expect(fake.dragPan.disable).not.toHaveBeenCalled()
    t.up({ x: 25, y: 25 })
    expect(t.onSelectedChange).not.toHaveBeenCalled()
  })

  it('gives up the block, leaving the map free, when the pointer cannot be captured', () => {
    const t = setup()
    const el = document.querySelector('[data-testid="map"]')!.parentElement!
    el.setPointerCapture = () => {
      throw new Error('no such pointer')
    }
    t.down({ x: 25, y: 25 })
    t.hold()
    expect(fake.dragPan.disable).not.toHaveBeenCalled()
    t.up({ x: 25, y: 25 })
    expect(t.onSelectedChange).not.toHaveBeenCalled()
  })

  it('a mouse press held still does not start a block, and is a tap on release', () => {
    const t = setup()
    t.down({ x: 25, y: 25, type: 'mouse' })
    t.hold()
    expect(fake.dragPan.disable).not.toHaveBeenCalled()
    t.up({ x: 25, y: 25, type: 'mouse' })
    expect(committed(t.onSelectedChange)).toEqual(['2,2'])
  })

  it('a mouse press that moves past the slop is a pan', () => {
    const t = setup()
    t.down({ x: 25, y: 25, type: 'mouse' })
    t.move({ x: 60, y: 25, type: 'mouse' })
    t.up({ x: 60, y: 25, type: 'mouse' })
    expect(t.onSelectedChange).not.toHaveBeenCalled()
  })

  it('a press that stops a gliding map is not a tap', () => {
    const t = setup()
    fake.moving = true
    act(() => fake.onDragEnd?.())
    t.down({ x: 25, y: 25 })
    t.hold()
    t.up({ x: 25, y: 25 })
    expect(t.onSelectedChange).not.toHaveBeenCalled()
    expect(fake.dragPan.disable).not.toHaveBeenCalled()
  })

  it('does not start a block if the view zoomed out past the limit during the hold', () => {
    const t = setup()
    t.down({ x: 25, y: 25 })
    fake.span = 60
    act(() => fake.onMoveEnd?.())
    t.hold()
    expect(fake.dragPan.disable).not.toHaveBeenCalled()
    t.up({ x: 25, y: 25 })
    expect(t.onSelectedChange).not.toHaveBeenCalled()
  })

  it('a cancelled pointer during a block gives the map its drag back', () => {
    const t = setup()
    t.down({ x: 25, y: 25 })
    t.hold()
    t.cancel({ x: 25, y: 25 })
    expect(fake.dragPan.enable).toHaveBeenCalledTimes(1)
    expect(t.onSelectedChange).not.toHaveBeenCalled()
  })

  it('leaving select mode during a block gives the map its drag back', () => {
    const t = setup()
    t.down({ x: 25, y: 25 })
    t.hold()
    t.reselect([], false)
    expect(fake.dragPan.enable).toHaveBeenCalled()
  })

  it('unmounting during a block gives the map its drag back', () => {
    const t = setup()
    t.down({ x: 25, y: 25 })
    t.hold()
    t.unmount()
    expect(fake.dragPan.enable).toHaveBeenCalled()
  })

  it('keeps the map from panning inside the slop and from zooming on a double tap', () => {
    setup()
    expect(fake.mapProps.clickTolerance).toBe(8)
    expect(fake.mapProps.doubleClickZoom).toBe(false)
  })

  it("another pointer's release on the window does not drop a block", () => {
    const t = setup()
    t.down({ x: 25, y: 25 })
    t.hold()
    t.windowUp({ x: 500, y: 500, id: 2, primary: false })
    expect(fake.dragPan.enable).not.toHaveBeenCalled()
    t.move({ x: 45, y: 25 })
    t.up({ x: 45, y: 25 })
    expect(committed(t.onSelectedChange)).toEqual(['2,2', '2,3', '2,4'])
  })

  it("another pointer's release on the window does not cancel a pending hold", () => {
    const t = setup()
    t.down({ x: 25, y: 25 })
    t.windowUp({ x: 500, y: 500, id: 2, primary: false })
    t.hold()
    expect(fake.dragPan.disable).toHaveBeenCalledTimes(1)
  })

  it('Shift with a mouse starts a block even while the map glides, and stops the glide', () => {
    const t = setup()
    fake.moving = true
    act(() => fake.onDragEnd?.())
    t.down({ x: 25, y: 25, type: 'mouse', shift: true })
    expect(fake.stop).toHaveBeenCalled()
    expect(fake.dragPan.disable).toHaveBeenCalledTimes(1)
    t.up({ x: 25, y: 25, type: 'mouse', shift: true })
    expect(committed(t.onSelectedChange)).toEqual(['2,2'])
  })

  it('a press that stops a gliding map halts it', () => {
    const t = setup()
    fake.moving = true
    act(() => fake.onDragEnd?.())
    t.down({ x: 25, y: 25 })
    expect(fake.stop).toHaveBeenCalled()
  })

  it('draws a box with the current selectable range, not the one at the press', () => {
    const t = setup()
    t.down({ x: 25, y: 25 })
    t.setSelectable({ lat_min: 5, lat_max: 88, lon_min: -180, lon_max: 179 })
    t.reselect([])
    t.hold()
    t.up({ x: 25, y: 25 })
    expect(committed(t.onSelectedChange)).toEqual([])
  })

  it('turns box zoom off while selecting, since Shift-drag is a block', () => {
    setup()
    expect(fake.mapProps.boxZoom).toBe(false)
  })

  it('a tap while a zoom eases, not a pan glide, still toggles the square', () => {
    const t = setup()
    fake.moving = true
    t.down({ x: 25, y: 25, type: 'mouse' })
    t.up({ x: 25, y: 25, type: 'mouse' })
    expect(committed(t.onSelectedChange)).toEqual(['2,2'])
  })

  it('a glide that has ended no longer swallows a tap', () => {
    const t = setup()
    act(() => fake.onDragEnd?.())
    act(() => fake.onMoveEnd?.())
    fake.moving = true
    t.down({ x: 25, y: 25, type: 'mouse' })
    t.up({ x: 25, y: 25, type: 'mouse' })
    expect(committed(t.onSelectedChange)).toEqual(['2,2'])
  })

  it('a tap toggles against the selection as it is at release', () => {
    const t = setup()
    t.down({ x: 25, y: 25, type: 'mouse' })
    t.reselect(['5,5'])
    t.up({ x: 25, y: 25, type: 'mouse' })
    expect(committed(t.onSelectedChange)).toEqual(['2,2', '5,5'])
  })

  it("keeps the browser's long-press menu off the map while selecting", () => {
    setup()
    const box = screen.getByTestId('map').parentElement!
    const event = new MouseEvent('contextmenu', { bubbles: true, cancelable: true })
    box.dispatchEvent(event)
    expect(event.defaultPrevented).toBe(true)
    expect(box.style.userSelect).toBe('none')
  })
})

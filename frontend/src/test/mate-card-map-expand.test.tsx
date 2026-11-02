import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { AssistantAnchorageMap } from '@/components/assistant-anchorage-map'
import AssistantAnchorageMapImpl from '@/components/assistant-anchorage-map-impl'
import { AssistantMarinaMap } from '@/components/assistant-marina-map'
import AssistantMarinaMapImpl from '@/components/assistant-marina-map-impl'

import { OverlayCloseContext } from '@/components/fullscreen-alarm-banner'
import { testRouter } from './helpers/router'
import { anchorageCard } from './helpers/anchorage-fixture'
import { marinaCard } from './helpers/marina-fixture'

// The same stand-ins the card tests use, with a ref that answers zoomIn and zoomOut.
// The full-screen alarm banner reads the live alarm list; seed it here.
const mockAlarms = vi.hoisted(() => ({ list: [] as unknown[] }))
vi.mock('@/hooks/use-alarms', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/hooks/use-alarms')>()),
  useAlarmListComparedOn: () => mockAlarms.list,
  acknowledgeAlarm: vi.fn(),
}))
vi.mock('@/hooks/use-forecast-warnings', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/hooks/use-forecast-warnings')>()),
  useForecastWarnings: () => ({ activeWarning: null }),
}))
const alarm = { rule_id: 'r', label: 'Bilge high', path: 'notifications.bilge', phase: 'active', state: 'alarm', value: 1, message: 'Bilge high', silenced: false, can_silence: false, can_acknowledge: true }
const { router, wrapper } = testRouter('/')

const mapProps: Record<string, unknown>[] = []
const zoom = vi.hoisted(() => ({ zoomIn: vi.fn(), zoomOut: vi.fn() }))
const fakeMap = vi.hoisted(() => ({
  touchZoomRotate: { disableRotation: vi.fn() },
  keyboard: { disableRotation: vi.fn() },
  getContainer: () => null,
}))
vi.mock('react-map-gl/maplibre', async () => {
  const { forwardRef, useImperativeHandle } = await import('react')
  return {
    Map: forwardRef(function FakeMap(props: { children?: ReactNode } & Record<string, unknown>, ref) {
      useImperativeHandle(ref, () => ({ zoomIn: zoom.zoomIn, zoomOut: zoom.zoomOut, getMap: () => fakeMap }))
      mapProps.push(props)
      return <div data-testid="map">{props.children}</div>
    }),
    Source: ({ id, children }: { id: string; children?: ReactNode }) => <div data-testid={`source-${id}`}>{children}</div>,
    Layer: ({ id }: { id: string }) => <i data-testid={`layer-${id}`} />,
  }
})
vi.mock('maplibre-gl', () => ({ default: {} }))
vi.mock('maplibre-gl/dist/maplibre-gl.css', () => ({}))
vi.mock('@/lib/webgl', () => ({ hasWebGL2: () => true }))
vi.mock('@/hooks/use-app-config', () => ({ useAppConfig: () => ({ ui: { distanceUnits: 'metric' } }) }))

let callbacks: Array<(entries: Array<{ isIntersecting: boolean }>) => void> = []
class FakeObserver {
  constructor(cb: (entries: Array<{ isIntersecting: boolean }>) => void) {
    callbacks.push(cb)
  }
  observe() {}
  disconnect() {}
}
const setNear = (near: boolean) => act(() => callbacks.forEach((cb) => cb([{ isIntersecting: near }])))
const lastProps = () => mapProps[mapProps.length - 1]

beforeEach(() => {
  mapProps.length = 0
  mockAlarms.list = []
  callbacks = []
  zoom.zoomIn.mockClear()
  zoom.zoomOut.mockClear()
  fakeMap.touchZoomRotate.disableRotation.mockClear()
  fakeMap.keyboard.disableRotation.mockClear()
  vi.stubGlobal('IntersectionObserver', FakeObserver)
  for (const t of ['--primary', '--gauge-primary', '--foreground', '--background', '--muted-foreground', '--destructive']) {
    document.documentElement.style.setProperty(t, '10 20% 30%')
  }
})
afterEach(() => vi.unstubAllGlobals())

const cards = [
  { label: 'marina', ui: () => <AssistantMarinaMapImpl card={marinaCard()} />, name: marinaCard().name, layer: 'layer-marina-outline-line' },
  { label: 'anchorage', ui: () => <AssistantAnchorageMapImpl card={anchorageCard()} />, name: anchorageCard().name, layer: 'layer-anchorage-rays-line' },
]

describe.each(cards)('the $label card map, expanded', ({ ui, name, layer }) => {
  it('opens a dialog named for the card with the one map in it', () => {
    render(ui(), { wrapper })
    fireEvent.click(screen.getByRole('button', { name: 'Full screen' }))
    const dialog = screen.getByRole('dialog', { name })
    expect(within(dialog).getByTestId(layer)).toBeInTheDocument()
    expect(screen.getAllByTestId('map')).toHaveLength(1)
    expect(within(dialog).getByTestId('map')).toBeInTheDocument()
  })

  it('lets a plain drag and pinch work only while expanded', () => {
    render(ui(), { wrapper })
    expect(lastProps().cooperativeGestures).toBe(true)
    fireEvent.click(screen.getByRole('button', { name: 'Full screen' }))
    expect(lastProps().cooperativeGestures).toBe(false)
    expect(lastProps().dragRotate).toBe(false)
    expect(lastProps().touchPitch).toBe(false)
  })

  it('has zoom buttons only while expanded, and they zoom the map', () => {
    render(ui(), { wrapper })
    expect(screen.queryByRole('button', { name: 'Zoom in' })).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Full screen' }))
    expect(screen.queryByRole('button', { name: 'Full screen' })).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Zoom in' }))
    fireEvent.click(screen.getByRole('button', { name: 'Zoom out' }))
    expect(zoom.zoomIn).toHaveBeenCalledTimes(1)
    expect(zoom.zoomOut).toHaveBeenCalledTimes(1)
  })

  it('returns to the inline map when closed, with the imagery choice kept', () => {
    render(ui(), { wrapper })
    fireEvent.click(screen.getByRole('button', { name: 'Toggle satellite imagery' }))
    fireEvent.click(screen.getByRole('button', { name: 'Full screen' }))
    expect(screen.queryByTestId('source-world-imagery')).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Close' }))
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(screen.getAllByTestId('map')).toHaveLength(1)
    expect(screen.getByRole('button', { name: 'Full screen' })).toBeInTheDocument()
    expect(screen.queryByTestId('source-world-imagery')).not.toBeInTheDocument()
    expect(lastProps().cooperativeGestures).toBe(true)
  })
})

describe('the marina card map, expanded', () => {
  it('keeps shops and services ashore on across opening and closing', () => {
    render(<AssistantMarinaMapImpl card={marinaCard()} />, { wrapper })
    fireEvent.click(screen.getByRole('button', { name: 'Toggle shops and services ashore' }))
    fireEvent.click(screen.getByRole('button', { name: 'Full screen' }))
    expect(screen.getByTestId('layer-marina-ashore-dot')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Close' }))
    expect(screen.getByTestId('layer-marina-ashore-dot')).toBeInTheDocument()
  })
})

describe.each([
  { label: 'marina', ui: () => <AssistantMarinaMap card={marinaCard()} />, layer: 'layer-marina-outline-line', box: 'marina-map' },
  { label: 'anchorage', ui: () => <AssistantAnchorageMap card={anchorageCard()} />, layer: 'layer-anchorage-rays-line', box: 'anchorage-map' },
])('the $label card box', ({ ui, layer, box }) => {
  it('keeps the expanded map when the card scrolls far away, and drops the map once closed', async () => {
    render(ui(), { wrapper })
    setNear(true)
    fireEvent.click(await screen.findByRole('button', { name: 'Full screen' }))
    setNear(false)
    expect(screen.getByRole('dialog')).toBeInTheDocument()
    expect(screen.getByTestId(layer)).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Close' }))
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(screen.queryByTestId('map')).not.toBeInTheDocument()
    expect(within(screen.getByTestId(box)).getByTestId(`${box}-idle`)).toBeInTheDocument()
  })
})

describe('the expanded map in detail', () => {
  const marina = () => <AssistantMarinaMapImpl card={marinaCard()} />

  it('shows a live alarm across the top of full screen only, and View closes the dialog and opens Alarms', () => {
    mockAlarms.list = [alarm]
    render(marina(), { wrapper })
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
    expect(router.getSnapshot().location.panel).not.toBe('alarms')
    fireEvent.click(screen.getByRole('button', { name: 'Full screen' }))
    const dialog = screen.getByRole('dialog')
    expect(within(dialog).getByRole('alert')).toHaveTextContent('Bilge high')
    fireEvent.click(within(dialog).getByRole('button', { name: /view/i }))
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(router.getSnapshot().location.panel).toBe('alarms')
  })

  it('also closes the Mate sheet it sits in, so Alarms is not left behind it', () => {
    mockAlarms.list = [alarm]
    const closeSheet = vi.fn()
    render(
      <OverlayCloseContext.Provider value={closeSheet}>{marina()}</OverlayCloseContext.Provider>,
      { wrapper },
    )
    fireEvent.click(screen.getByRole('button', { name: 'Full screen' }))
    fireEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: /view/i }))
    expect(closeSheet).toHaveBeenCalledTimes(1)
  })

  it('stops a pinch and the keyboard rotating the map as soon as it exists, inline and full screen', () => {
    render(marina(), { wrapper })
    expect(fakeMap.touchZoomRotate.disableRotation).toHaveBeenCalledTimes(1)
    expect(fakeMap.keyboard.disableRotation).toHaveBeenCalledTimes(1)
    fireEvent.click(screen.getByRole('button', { name: 'Full screen' }))
    expect(fakeMap.touchZoomRotate.disableRotation).toHaveBeenCalledTimes(2)
    expect(fakeMap.keyboard.disableRotation).toHaveBeenCalledTimes(2)
  })

  it('puts focus back on the Full screen button when closed', async () => {
    render(marina(), { wrapper })
    fireEvent.click(screen.getByRole('button', { name: 'Full screen' }))
    fireEvent.click(screen.getByRole('button', { name: 'Close' }))
    await waitFor(() => expect(document.activeElement).toBe(screen.getByRole('button', { name: 'Full screen' })))
  })

  it.each([
    { label: 'marina', ui: () => <AssistantMarinaMapImpl card={marinaCard({ name: '' })} />, title: 'Unnamed marina' },
    { label: 'anchorage', ui: () => <AssistantAnchorageMapImpl card={{ ...anchorageCard(), name: '' }} />, title: 'Unnamed anchorage' },
  ])('titles a $label with no name "$title", as its card heading does', ({ ui, title }) => {
    render(ui(), { wrapper })
    fireEvent.click(screen.getByRole('button', { name: 'Full screen' }))
    expect(screen.getByRole('dialog', { name: title })).toBeInTheDocument()
  })

  it('does not fill the screen with width or height units that overrun a scrollbar', () => {
    render(marina(), { wrapper })
    fireEvent.click(screen.getByRole('button', { name: 'Full screen' }))
    const cls = screen.getByRole('dialog').className
    expect(cls).toContain('inset-0')
    expect(cls).not.toMatch(/w-screen|h-dvh/)
  })

  it('never mounts an inline map when a far-away card is closed', async () => {
    render(<AssistantMarinaMap card={marinaCard()} />, { wrapper })
    setNear(true)
    fireEvent.click(await screen.findByRole('button', { name: 'Full screen' }))
    setNear(false)
    const before = mapProps.length
    fireEvent.click(screen.getByRole('button', { name: 'Close' }))
    expect(mapProps.length).toBe(before)
  })
})

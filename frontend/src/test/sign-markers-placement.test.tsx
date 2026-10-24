import { act, render, screen } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { SignMarkers } from '@/components/passages/sign-markers'
import type { ResolvedSign } from '@/lib/replay-signs'

const start = { x: 790, y: 400 }
let point = { ...start }
const handlers = new Map<string, () => void>()
const fakeMap = {
  project: () => point,
  getContainer: () => ({ clientWidth: 800, clientHeight: 600 }),
  on: (ev: string, fn: () => void) => { handlers.set(ev, fn) },
  off: (ev: string) => { handlers.delete(ev) },
}
vi.mock('react-map-gl/maplibre', () => ({
  Marker: ({ children, anchor }: { children: ReactNode; anchor: string }) => <div data-testid="marker" data-anchor={anchor}>{children}</div>,
  useMap: () => ({ current: { getMap: () => fakeMap } }),
}))

let observed: ((entries: { contentRect: { width: number; height: number } }[]) => void) | null = null
class FakeResizeObserver {
  constructor(cb: typeof observed) { observed = cb }
  observe() {}
  disconnect() {}
}

const resolved: ResolvedSign = {
  sign: { id: 's1', t_ms: 0, title: 'Reef', note: '', document_id: null, photo_url: '/api/x.jpg', visibility: 'always', show_s: 4 },
  clock: 0, at: 0, entryId: 'p1', lat: 0, lon: 0,
}
const mount = () => render(<SignMarkers cards={[resolved]} pins={[]} openId={null} canOpenPhoto={false} still onOpenPhoto={() => {}} onPinClick={() => {}} onCloseOpen={() => {}} />)
const cardShift = () => (screen.getByTestId('sign-card').parentElement as HTMLElement).style.transform
const leaderHeight = () => (document.querySelector('span.w-px') as HTMLElement).style.height

describe('SignMarkers placement', () => {
  beforeEach(() => {
    point = { ...start }
    handlers.clear()
    observed = null
    vi.stubGlobal('ResizeObserver', FakeResizeObserver)
  })
  afterEach(() => {
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
  })

  it('places a card once its size is known, with no map move', () => {
    mount()
    expect(cardShift()).toBe('')
    act(() => observed?.([{ contentRect: { width: 256, height: 220 } }]))
    expect(cardShift()).toBe('translateX(-122px)')
  })

  it('keeps one marker anchored at its point and moves the card by layout as the camera moves', () => {
    mount()
    act(() => observed?.([{ contentRect: { width: 256, height: 220 } }]))
    expect(screen.getByTestId('marker')).toHaveAttribute('data-anchor', 'center')
    point = { x: 400, y: 100 }
    act(() => handlers.get('move')?.())
    expect(cardShift()).toBe('')
    expect(screen.getByTestId('marker')).toHaveAttribute('data-anchor', 'center')
    expect(screen.getByTestId('sign-card').parentElement?.parentElement?.className).toContain('flex-col-reverse')
    expect(leaderHeight()).toBe('20px')
  })

  it('shortens the leader when the card only just fits', () => {
    mount()
    act(() => observed?.([{ contentRect: { width: 256, height: 220 } }]))
    point = { x: 400, y: 280 }
    act(() => handlers.get('move')?.())
    expect(leaderHeight()).toBe('12px')
  })
})

import { act, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { AssistantMarinaMap } from '@/components/assistant-marina-map'

import { marinaCard } from './helpers/marina-fixture'

vi.mock('@/components/assistant-marina-map-impl', () => ({
  default: () => <div data-testid="marina-map-stub" />,
}))

// jsdom has no IntersectionObserver. The stand-in lets a test say when the
// card comes near the visible area and when it leaves.
let callbacks: Array<(entries: Array<{ isIntersecting: boolean }>) => void> = []
let disconnected = 0
let options: IntersectionObserverInit | undefined
class FakeObserver {
  constructor(cb: (entries: Array<{ isIntersecting: boolean }>) => void, opts?: IntersectionObserverInit) {
    callbacks.push(cb)
    options = opts
  }
  observe() {}
  disconnect() {
    disconnected += 1
  }
}
const setNear = (near: boolean) => act(() => callbacks.forEach((cb) => cb([{ isIntersecting: near }])))

beforeEach(() => {
  callbacks = []
  disconnected = 0
  options = undefined
  vi.stubGlobal('IntersectionObserver', FakeObserver)
})
afterEach(() => vi.unstubAllGlobals())

describe('AssistantMarinaMap', () => {
  it('shows nothing about the map until it has been measured', () => {
    render(<AssistantMarinaMap card={marinaCard()} />)
    expect(screen.queryByTestId('marina-map-stub')).toBeNull()
    expect(screen.queryByTestId('marina-map-idle')).toBeNull()
    expect(screen.queryByText(/scroll to it/)).toBeNull()
  })

  it('mounts no map while the card is far from the visible area', () => {
    render(<AssistantMarinaMap card={marinaCard()} />)
    setNear(false)
    expect(screen.queryByTestId('marina-map-stub')).toBeNull()
    expect(screen.getByTestId('marina-map-idle')).toBeInTheDocument()
  })

  it('mounts the map when the card comes near, and drops it when it scrolls well away', async () => {
    render(<AssistantMarinaMap card={marinaCard()} />)
    setNear(true)
    expect(await screen.findByTestId('marina-map-stub')).toBeInTheDocument()
    setNear(false)
    expect(screen.queryByTestId('marina-map-stub')).toBeNull()
    expect(screen.getByTestId('marina-map-idle')).toBeInTheDocument()
  })

  it('measures against the thread\'s own scroll container, not the page', () => {
    render(
      <div data-testid="thread" style={{ overflowY: 'auto' }}>
        <div>
          <AssistantMarinaMap card={marinaCard()} />
        </div>
      </div>,
    )
    expect(options?.root).toBe(screen.getByTestId('thread'))
    expect(options?.rootMargin).toBe('600px 0px')
  })

  it('measures against the page when nothing around it scrolls', () => {
    render(<AssistantMarinaMap card={marinaCard()} />)
    expect(options?.root ?? null).toBeNull()
  })

  it('stops watching when the card goes away', () => {
    const { unmount } = render(<AssistantMarinaMap card={marinaCard()} />)
    unmount()
    expect(disconnected).toBe(1)
  })
})

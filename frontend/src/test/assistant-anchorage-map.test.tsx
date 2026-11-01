import { act, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { AssistantAnchorageMap } from '@/components/assistant-anchorage-map'

import { anchorageCard } from './helpers/anchorage-fixture'

vi.mock('@/components/assistant-anchorage-map-impl', () => ({
  default: () => <div data-testid="anchorage-map-stub" />,
}))

// jsdom has no IntersectionObserver. The stand-in lets a test say when the
// card comes near the visible area and when it leaves.
let callbacks: Array<(entries: Array<{ isIntersecting: boolean }>) => void> = []
let disconnected = 0
class FakeObserver {
  constructor(cb: (entries: Array<{ isIntersecting: boolean }>) => void) {
    callbacks.push(cb)
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
  vi.stubGlobal('IntersectionObserver', FakeObserver)
})
afterEach(() => vi.unstubAllGlobals())

describe('AssistantAnchorageMap', () => {
  it('shows nothing about the map until it has been measured', () => {
    render(<AssistantAnchorageMap card={anchorageCard()} />)
    expect(screen.queryByTestId('anchorage-map-stub')).toBeNull()
    expect(screen.queryByTestId('anchorage-map-idle')).toBeNull()
  })

  it('mounts no map while the card is far away, and mounts and drops it as it comes and goes', async () => {
    render(<AssistantAnchorageMap card={anchorageCard()} />)
    setNear(false)
    expect(screen.getByTestId('anchorage-map-idle')).toBeInTheDocument()
    setNear(true)
    expect(await screen.findByTestId('anchorage-map-stub')).toBeInTheDocument()
    setNear(false)
    expect(screen.queryByTestId('anchorage-map-stub')).toBeNull()
  })

  it('stops watching when the card goes away', () => {
    const { unmount } = render(<AssistantAnchorageMap card={anchorageCard()} />)
    unmount()
    expect(disconnected).toBe(1)
  })
})

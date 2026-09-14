/**
 * Coverage for useInView (hooks/use-in-view.ts). happy-dom's own
 * IntersectionObserver is an inert stub (every method is a documented
 * TODO — see node_modules/happy-dom/lib/intersection-observer/
 * IntersectionObserver.js), so it never actually reports an intersection.
 * FakeIntersectionObserver below stands in for it: it records what it was
 * asked to observe and exposes a way to fire the callback by hand, the same
 * way FakeWebSocket in use-radar-echo-stream.test.ts stands in for a socket
 * that never really connects.
 */
import { act, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { useInView } from '@/hooks/use-in-view'

class FakeIntersectionObserver {
  static instances: FakeIntersectionObserver[] = []

  readonly callback: IntersectionObserverCallback
  readonly options: IntersectionObserverInit | undefined
  observedElements: Element[] = []
  disconnected = false

  constructor(callback: IntersectionObserverCallback, options?: IntersectionObserverInit) {
    this.callback = callback
    this.options = options
    FakeIntersectionObserver.instances.push(this)
  }

  observe(target: Element) {
    this.observedElements.push(target)
  }

  unobserve(target: Element) {
    this.observedElements = this.observedElements.filter((el) => el !== target)
  }

  disconnect() {
    this.disconnected = true
  }

  takeRecords(): IntersectionObserverEntry[] {
    return []
  }

  // Test-only helper: fires the callback as the real observer would once an
  // observed element crosses the intersection threshold.
  intersect(isIntersecting = true) {
    const entries = this.observedElements.map(
      (target) => ({ isIntersecting, target }) as IntersectionObserverEntry,
    )
    this.callback(entries, this as unknown as IntersectionObserver)
  }
}

// Test-only component: attaches useInView's ref to a rendered element and
// surfaces `inView` as text, since the hook itself has no DOM footprint of
// its own to assert against.
function Probe({ rootMargin }: { rootMargin?: string }) {
  const [ref, inView] = useInView<HTMLDivElement>({ rootMargin })
  return (
    <div ref={ref} data-testid="target">
      {inView ? 'in-view' : 'out-of-view'}
    </div>
  )
}

beforeEach(() => {
  FakeIntersectionObserver.instances = []
  vi.stubGlobal('IntersectionObserver', FakeIntersectionObserver)
})

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('useInView', () => {
  it('starts out of view and observes the attached element', () => {
    render(<Probe />)

    expect(screen.getByTestId('target')).toHaveTextContent('out-of-view')
    expect(FakeIntersectionObserver.instances).toHaveLength(1)
    expect(FakeIntersectionObserver.instances[0].observedElements).toEqual([
      screen.getByTestId('target'),
    ])
  })

  it('defaults rootMargin to 200px', () => {
    render(<Probe />)

    expect(FakeIntersectionObserver.instances[0].options?.rootMargin).toBe('200px')
  })

  it('passes a caller-supplied rootMargin through to the observer', () => {
    render(<Probe rootMargin="50px" />)

    expect(FakeIntersectionObserver.instances[0].options?.rootMargin).toBe('50px')
  })

  it('flips to in view once the observer reports an intersection', () => {
    render(<Probe />)

    act(() => {
      FakeIntersectionObserver.instances[0].intersect(true)
    })

    expect(screen.getByTestId('target')).toHaveTextContent('in-view')
  })

  it('disconnects the observer as soon as it has been seen, and stays in view after a later non-intersecting report', () => {
    render(<Probe />)
    const observer = FakeIntersectionObserver.instances[0]

    act(() => {
      observer.intersect(true)
    })
    expect(observer.disconnected).toBe(true)
    expect(FakeIntersectionObserver.instances).toHaveLength(1) // no replacement observer created

    act(() => {
      observer.intersect(false)
    })
    expect(screen.getByTestId('target')).toHaveTextContent('in-view')
  })

  it('disconnects the observer on unmount', () => {
    const { unmount } = render(<Probe />)
    const observer = FakeIntersectionObserver.instances[0]

    unmount()

    expect(observer.disconnected).toBe(true)
  })
})

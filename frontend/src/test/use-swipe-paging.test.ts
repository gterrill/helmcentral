/**
 * Coverage for useSwipePaging (hooks/use-swipe-paging.ts): the full screen
 * dashboard's touch-swipe page switcher. Same Probe-component pattern as
 * use-in-view.test.tsx - the hook's only DOM footprint is the ref it
 * attaches, so a small test component renders it and PointerEvents are
 * fired against the attached element via fireEvent.
 */
import { createElement } from 'react'
import { render, fireEvent, screen } from '@testing-library/react'
import { describe, it, expect, vi, afterEach } from 'vitest'

import { useSwipePaging } from '@/hooks/use-swipe-paging'

interface ProbeProps {
  enabled: boolean
  onNext: () => void
  onPrevious: () => void
  /** A key on the rendered surface, distinct from the hook's own re-render -
   *  changing it forces React to unmount the old DOM node and mount a new
   *  one at the same spot, simulating the container being replaced (e.g.
   *  a session expiring mid-full-screen swaps the surface for
   *  LoginScreen, then a fresh one mounts after logging back in) while
   *  `enabled` stays whatever the caller already set it to. */
  surfaceKey?: string
}

// Test-only component: attaches the hook's ref to a rendered surface, with
// a few nested "ignored area" stand-ins (a chart-scrub surface, a map, a
// text input) so gestures starting inside them can be asserted separately
// from gestures starting on the bare surface.
function Probe({ enabled, onNext, onPrevious, surfaceKey }: ProbeProps) {
  const ref = useSwipePaging({ enabled, onNext, onPrevious })
  return createElement(
    'div',
    { ref, key: surfaceKey, 'data-testid': 'surface' },
    createElement('div', { 'data-no-swipe': true, 'data-testid': 'no-swipe-zone' }, 'chart'),
    createElement('div', { className: 'maplibregl-map', 'data-testid': 'map-zone' }, 'map'),
    createElement('input', { 'data-testid': 'text-input' }),
  )
}

function swipe(
  surface: Element,
  opts: {
    startX: number
    startY: number
    endX: number
    endY: number
    pointerType?: string
    pointerId?: number
    downTarget?: Element
  },
) {
  const { startX, startY, endX, endY, pointerType = 'touch', pointerId = 1, downTarget = surface } = opts
  fireEvent.pointerDown(downTarget, { pointerType, pointerId, clientX: startX, clientY: startY })
  fireEvent.pointerUp(surface, { pointerType, pointerId, clientX: endX, clientY: endY })
}

afterEach(() => {
  vi.restoreAllMocks()
})

describe('useSwipePaging', () => {
  it('calls onNext on a left swipe (touch, past the distance/ratio/duration thresholds)', () => {
    const onNext = vi.fn()
    const onPrevious = vi.fn()
    const { getByTestId } = render(createElement(Probe, { enabled: true, onNext, onPrevious }))

    swipe(getByTestId('surface'), { startX: 250, startY: 100, endX: 150, endY: 100 })

    expect(onNext).toHaveBeenCalledTimes(1)
    expect(onPrevious).not.toHaveBeenCalled()
  })

  it('calls onPrevious on a right swipe', () => {
    const onNext = vi.fn()
    const onPrevious = vi.fn()
    const { getByTestId } = render(createElement(Probe, { enabled: true, onNext, onPrevious }))

    swipe(getByTestId('surface'), { startX: 100, startY: 100, endX: 200, endY: 100 })

    expect(onPrevious).toHaveBeenCalledTimes(1)
    expect(onNext).not.toHaveBeenCalled()
  })

  it('does nothing when disabled', () => {
    const onNext = vi.fn()
    const onPrevious = vi.fn()
    const { getByTestId } = render(createElement(Probe, { enabled: false, onNext, onPrevious }))

    swipe(getByTestId('surface'), { startX: 250, startY: 100, endX: 150, endY: 100 })

    expect(onNext).not.toHaveBeenCalled()
    expect(onPrevious).not.toHaveBeenCalled()
  })

  it('ignores a mouse drag (pointerType other than touch)', () => {
    const onNext = vi.fn()
    const onPrevious = vi.fn()
    const { getByTestId } = render(createElement(Probe, { enabled: true, onNext, onPrevious }))

    swipe(getByTestId('surface'), { startX: 250, startY: 100, endX: 150, endY: 100, pointerType: 'mouse' })

    expect(onNext).not.toHaveBeenCalled()
    expect(onPrevious).not.toHaveBeenCalled()
  })

  it('ignores a short drag under the 60px distance threshold', () => {
    const onNext = vi.fn()
    const onPrevious = vi.fn()
    const { getByTestId } = render(createElement(Probe, { enabled: true, onNext, onPrevious }))

    swipe(getByTestId('surface'), { startX: 200, startY: 100, endX: 160, endY: 100 })

    expect(onNext).not.toHaveBeenCalled()
    expect(onPrevious).not.toHaveBeenCalled()
  })

  it('ignores a mostly-vertical drag (dx under 1.5x |dy|) even past the 60px floor', () => {
    const onNext = vi.fn()
    const onPrevious = vi.fn()
    const { getByTestId } = render(createElement(Probe, { enabled: true, onNext, onPrevious }))

    // dx=-70, dy=-60: |dx| >= 60 alone, but 70 < 1.5*60 (=90).
    swipe(getByTestId('surface'), { startX: 200, startY: 160, endX: 130, endY: 100 })

    expect(onNext).not.toHaveBeenCalled()
    expect(onPrevious).not.toHaveBeenCalled()
  })

  it('accepts a diagonal drag once dx clears 1.5x |dy|', () => {
    const onNext = vi.fn()
    const onPrevious = vi.fn()
    const { getByTestId } = render(createElement(Probe, { enabled: true, onNext, onPrevious }))

    // dx=-90, dy=20: 90 >= 1.5*20 (=30).
    swipe(getByTestId('surface'), { startX: 200, startY: 100, endX: 110, endY: 120 })

    expect(onNext).toHaveBeenCalledTimes(1)
  })

  it('ignores a gesture slower than 600ms', () => {
    const onNext = vi.fn()
    const onPrevious = vi.fn()
    const { getByTestId } = render(createElement(Probe, { enabled: true, onNext, onPrevious }))

    const dateNowSpy = vi.spyOn(Date, 'now')
    dateNowSpy.mockReturnValueOnce(1_000).mockReturnValueOnce(1_000 + 700)

    swipe(getByTestId('surface'), { startX: 250, startY: 100, endX: 150, endY: 100 })

    expect(onNext).not.toHaveBeenCalled()
  })

  it('accepts a gesture at exactly the duration/distance thresholds', () => {
    const onNext = vi.fn()
    const onPrevious = vi.fn()
    const { getByTestId } = render(createElement(Probe, { enabled: true, onNext, onPrevious }))

    const dateNowSpy = vi.spyOn(Date, 'now')
    dateNowSpy.mockReturnValueOnce(2_000).mockReturnValueOnce(2_000 + 600)

    swipe(getByTestId('surface'), { startX: 260, startY: 100, endX: 200, endY: 100 })

    expect(onNext).toHaveBeenCalledTimes(1)
  })

  it('ignores a gesture starting inside a data-no-swipe element (chart scrubbing)', () => {
    const onNext = vi.fn()
    const onPrevious = vi.fn()
    const { getByTestId } = render(createElement(Probe, { enabled: true, onNext, onPrevious }))

    swipe(getByTestId('surface'), {
      startX: 250, startY: 100, endX: 150, endY: 100,
      downTarget: getByTestId('no-swipe-zone'),
    })

    expect(onNext).not.toHaveBeenCalled()
    expect(onPrevious).not.toHaveBeenCalled()
  })

  it('ignores a gesture starting inside a MapLibre map', () => {
    const onNext = vi.fn()
    const onPrevious = vi.fn()
    const { getByTestId } = render(createElement(Probe, { enabled: true, onNext, onPrevious }))

    swipe(getByTestId('surface'), {
      startX: 250, startY: 100, endX: 150, endY: 100,
      downTarget: getByTestId('map-zone'),
    })

    expect(onNext).not.toHaveBeenCalled()
  })

  it('ignores a gesture starting inside a text input', () => {
    const onNext = vi.fn()
    const onPrevious = vi.fn()
    const { getByTestId } = render(createElement(Probe, { enabled: true, onNext, onPrevious }))

    swipe(getByTestId('surface'), {
      startX: 250, startY: 100, endX: 150, endY: 100,
      downTarget: getByTestId('text-input'),
    })

    expect(onNext).not.toHaveBeenCalled()
  })

  it('ignores a gesture once a second touch joins (pinch/multi-touch, not a swipe)', () => {
    const onNext = vi.fn()
    const onPrevious = vi.fn()
    const { getByTestId } = render(createElement(Probe, { enabled: true, onNext, onPrevious }))
    const surface = getByTestId('surface')

    fireEvent.pointerDown(surface, { pointerType: 'touch', pointerId: 1, clientX: 250, clientY: 100 })
    fireEvent.pointerDown(surface, { pointerType: 'touch', pointerId: 2, clientX: 60, clientY: 100 })
    fireEvent.pointerUp(surface, { pointerType: 'touch', pointerId: 1, clientX: 150, clientY: 100 })

    expect(onNext).not.toHaveBeenCalled()
    expect(onPrevious).not.toHaveBeenCalled()
  })

  it('removes its listeners on unmount', () => {
    const onNext = vi.fn()
    const onPrevious = vi.fn()
    const { getByTestId, unmount } = render(createElement(Probe, { enabled: true, onNext, onPrevious }))
    const surface = getByTestId('surface')

    unmount()
    swipe(surface, { startX: 250, startY: 100, endX: 150, endY: 100 })

    expect(onNext).not.toHaveBeenCalled()
  })

  it('starts listening once enabled flips from false to true', () => {
    const onNext = vi.fn()
    const onPrevious = vi.fn()
    const { getByTestId, rerender } = render(createElement(Probe, { enabled: false, onNext, onPrevious }))

    rerender(createElement(Probe, { enabled: true, onNext, onPrevious }))
    swipe(getByTestId('surface'), { startX: 250, startY: 100, endX: 150, endY: 100 })

    expect(onNext).toHaveBeenCalledTimes(1)
  })

  it('calls the latest onNext/onPrevious even when they change between renders (no stale closure)', () => {
    const onNextA = vi.fn()
    const onNextB = vi.fn()
    const onPrevious = vi.fn()
    const { getByTestId, rerender } = render(createElement(Probe, { enabled: true, onNext: onNextA, onPrevious }))

    rerender(createElement(Probe, { enabled: true, onNext: onNextB, onPrevious }))
    swipe(getByTestId('surface'), { startX: 250, startY: 100, endX: 150, endY: 100 })

    expect(onNextA).not.toHaveBeenCalled()
    expect(onNextB).toHaveBeenCalledTimes(1)
  })

  // [P1 finding] The listener effect used to depend on `[enabled]` alone.
  // If the ref'd element is swapped for a new DOM node while `enabled`
  // never flips - a session expiring mid-full-screen unmounts the swipe
  // surface for LoginScreen, then a fresh surface mounts once logged back
  // in, still full screen - the effect never reran, so the listeners stayed
  // on the detached old node and the new surface never got any.
  it('keeps listening after the ref\'d element is replaced while enabled stays true throughout', () => {
    const onNext = vi.fn()
    const onPrevious = vi.fn()
    const { getByTestId, rerender } = render(
      createElement(Probe, { enabled: true, onNext, onPrevious, surfaceKey: 'a' }),
    )

    // A different key forces React to unmount the first surface element and
    // mount a brand new one, standing in for the login round-trip - `enabled`
    // itself is unchanged across this rerender.
    rerender(createElement(Probe, { enabled: true, onNext, onPrevious, surfaceKey: 'b' }))

    swipe(getByTestId('surface'), { startX: 250, startY: 100, endX: 150, endY: 100 })

    expect(onNext).toHaveBeenCalledTimes(1)
  })

  // Without touch-action: pan-y the browser claims a sideways drag as its own
  // pan and sends pointercancel before the finger lifts, so pointerup (and the
  // swipe) never arrives on a real tablet.
  it('sets touch-action: pan-y on the surface while enabled, and clears it when disabled', () => {
    const { rerender } = render(createElement(Probe, { enabled: true, onNext: vi.fn(), onPrevious: vi.fn() }))
    const surface = screen.getByTestId('surface') as HTMLElement
    expect(surface.style.touchAction).toBe('pan-y')
    rerender(createElement(Probe, { enabled: false, onNext: vi.fn(), onPrevious: vi.fn() }))
    expect(surface.style.touchAction).toBe('')
  })
})

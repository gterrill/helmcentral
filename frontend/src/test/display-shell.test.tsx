import { render } from '@testing-library/react'
import { describe, expect, test, vi, afterEach, beforeEach } from 'vitest'

import { DisplayShell } from '@/components/display-shell'
import type { Display } from '@/lib/displays'
import type { ScreenWakeLockStatus } from '@/hooks/use-screen-wake-lock'

vi.mock('@/hooks/use-telemetry-stream', () => ({
  useTelemetryStatus: () => 'connected' as const,
}))

const { mockPixelShift, mockWakeLock } = vi.hoisted(() => ({
  mockPixelShift: vi.fn(() => ({ dx: 0, dy: 0 })),
  mockWakeLock: vi.fn((): { status: ScreenWakeLockStatus } => ({ status: 'off' })),
}))

vi.mock('@/hooks/use-pixel-shift', () => ({
  usePixelShift: mockPixelShift,
}))
vi.mock('@/hooks/use-screen-wake-lock', () => ({
  useScreenWakeLock: mockWakeLock,
}))

function display(overrides: Partial<Display> = {}): Display {
  return {
    id: 'd1',
    name: 'Flybridge',
    slug: 'flybridge',
    width: 1920,
    height: 360,
    scale: 1,
    rotate: 0,
    pixel_shift: false,
    wake_lock: false,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    ...overrides,
  }
}

describe('DisplayShell', () => {
  beforeEach(() => {
    mockPixelShift.mockReturnValue({ dx: 0, dy: 0 })
    mockWakeLock.mockReturnValue({ status: 'off' })
  })

  afterEach(() => {
    document.documentElement.style.cursor = ''
    document.body.style.overflow = ''
    vi.useRealTimers()
  })

  test('renders its children inside the inner canvas', () => {
    const { getByText } = render(
      <DisplayShell display={display()} alarms={[]}>
        <div>content</div>
      </DisplayShell>,
    )
    expect(getByText('content')).toBeInTheDocument()
  })

  test('sizes the outer footprint to the physical (scaled) size and the inner canvas to the logical (unscaled) size', () => {
    const { getByTestId } = render(
      <DisplayShell display={display({ width: 1920, height: 360, scale: 1.5 })} alarms={[]}>
        <div>content</div>
      </DisplayShell>,
    )
    const outer = getByTestId('display-shell-outer')
    const inner = getByTestId('display-shell-inner')
    // Outer: the physical footprint on the wall - width*scale x height*scale.
    expect(outer.style.width).toBe('2880px')
    expect(outer.style.height).toBe('540px')
    // Inner: the logical canvas a page is authored against - unscaled.
    expect(inner.style.width).toBe('1920px')
    expect(inner.style.height).toBe('360px')
  })

  test('scales the painted box via the inner transform while leaving its layout width alone (transform, not zoom)', () => {
    const { getByTestId } = render(
      <DisplayShell display={display({ width: 1920, height: 360, scale: 1.5 })} alarms={[]}>
        <div>content</div>
      </DisplayShell>,
    )
    const inner = getByTestId('display-shell-inner')

    // jsdom implements no layout at all - every element's getBoundingClientRect
    // and offsetWidth/offsetHeight are 0 regardless of any real CSS. This
    // mocks just enough of a browser's box model - offsetWidth from the box's
    // own CSS width (what a `transform` never changes, by spec) and
    // getBoundingClientRect by applying the scale factor the component itself
    // wrote into `transform` - to give a meaningful assertion of the very
    // property that makes `transform` load-bearing here: react-grid-layout's
    // WidthProvider measures `offsetWidth` (chunk-7MZZ6T4J.js), which must
    // stay at the unscaled canvas width for the 12-column grid to lay out
    // correctly, while the actually-painted box is the scaled one.
    const cssWidth = Number(inner.style.width.replace('px', ''))
    const cssHeight = Number(inner.style.height.replace('px', ''))
    Object.defineProperty(inner, 'offsetWidth', { configurable: true, value: cssWidth })
    Object.defineProperty(inner, 'offsetHeight', { configurable: true, value: cssHeight })
    const scaleMatch = /scale\(([\d.]+)\)/.exec(inner.style.transform)
    const scale = scaleMatch ? Number(scaleMatch[1]) : 1
    inner.getBoundingClientRect = () => ({
      width: cssWidth * scale, height: cssHeight * scale,
      top: 0, left: 0, right: cssWidth * scale, bottom: cssHeight * scale, x: 0, y: 0, toJSON() {},
    }) as DOMRect

    // Painted (what an eye or a screenshot sees): scaled.
    expect(inner.getBoundingClientRect().width).toBe(2880)
    expect(inner.getBoundingClientRect().height).toBe(540)
    // Layout (what WidthProvider measures to size the 12-column grid): not.
    expect(inner.offsetWidth).toBe(1920)
    expect(inner.offsetHeight).toBe(360)
  })

  test('rotates the outer box 180 degrees about its own centre, independent of scale', () => {
    const { getByTestId } = render(
      <DisplayShell display={display({ rotate: 180, scale: 2 })} alarms={[]}>
        <div>content</div>
      </DisplayShell>,
    )
    const outer = getByTestId('display-shell-outer')
    expect(outer).toHaveAttribute('data-rotate', '180')
    expect(outer.style.transform).toBe('rotate(180deg)')
    expect(outer.style.transformOrigin).toBe('center center')
  })

  test('applies no rotation transform when rotate is 0', () => {
    const { getByTestId } = render(
      <DisplayShell display={display({ rotate: 0 })} alarms={[]}>
        <div>content</div>
      </DisplayShell>,
    )
    const outer = getByTestId('display-shell-outer')
    expect(outer).toHaveAttribute('data-rotate', '0')
    expect(outer.style.transform).toBe('')
  })

  test('a zero canvas claims the full viewport and applies no scaling, even if the record carries a stale scale value', () => {
    const { getByTestId } = render(
      <DisplayShell display={display({ width: 0, height: 0, scale: 2 })} alarms={[]}>
        <div>content</div>
      </DisplayShell>,
    )
    const outer = getByTestId('display-shell-outer')
    const inner = getByTestId('display-shell-inner')
    expect(outer.style.width).toBe('100vw')
    expect(outer.style.height).toBe('100vh')
    expect(inner.style.transform).toBe('scale(1) translate(0px, 0px)')
  })

  test('composes the pixel shift into the inner transform, alongside scale', () => {
    mockPixelShift.mockReturnValue({ dx: 8, dy: 8 })
    const { getByTestId } = render(
      <DisplayShell display={display({ scale: 1.5, pixel_shift: true })} alarms={[]}>
        <div>content</div>
      </DisplayShell>,
    )
    const inner = getByTestId('display-shell-inner')
    expect(inner.style.transform).toBe('scale(1.5) translate(8px, 8px)')
  })

  test('passes the display record pixel_shift/wake_lock fields to the corresponding hooks', () => {
    render(
      <DisplayShell display={display({ pixel_shift: true, wake_lock: true })} alarms={[]}>
        <div>content</div>
      </DisplayShell>,
    )
    expect(mockPixelShift).toHaveBeenCalledWith(true)
    expect(mockWakeLock).toHaveBeenCalledWith(true)
  })

  test('renders the status badge inside the inner canvas, not as a sibling of it', () => {
    const { getByTestId } = render(
      <DisplayShell display={display()} alarms={[{
        rule_id: 'r1', label: 'depth', path: 'environment.depth.belowTransducer', phase: 'active', state: 'alarm',
        value: 1, message: 'shallow', silenced: false, can_silence: true, can_acknowledge: true,
      }]}>
        <div>content</div>
      </DisplayShell>,
    )
    const inner = getByTestId('display-shell-inner')
    expect(inner).toContainElement(document.querySelector('[data-testid="display-alarm-pill"]'))
  })

  test('surfaces a non-off/held wake-lock status through the status badge', () => {
    mockWakeLock.mockReturnValue({ status: 'denied' })
    const { getByTestId } = render(
      <DisplayShell display={display({ wake_lock: true })} alarms={[]}>
        <div>content</div>
      </DisplayShell>,
    )
    expect(getByTestId('display-wake-lock-pill')).toHaveTextContent(/denied/i)
  })

  test('locks body scroll while mounted and restores it on unmount', () => {
    const { unmount } = render(
      <DisplayShell display={display()} alarms={[]}>
        <div>content</div>
      </DisplayShell>,
    )
    expect(document.body.style.overflow).toBe('hidden')
    unmount()
    expect(document.body.style.overflow).toBe('')
  })

  test('does not hide the cursor immediately - only after ~3s with no pointer movement', () => {
    vi.useFakeTimers()
    render(
      <DisplayShell display={display()} alarms={[]}>
        <div>content</div>
      </DisplayShell>,
    )
    expect(document.documentElement.style.cursor).not.toBe('none')

    vi.advanceTimersByTime(2999)
    expect(document.documentElement.style.cursor).not.toBe('none')

    vi.advanceTimersByTime(1)
    expect(document.documentElement.style.cursor).toBe('none')
  })

  test('shows the cursor again on pointer movement and restarts the idle timer', () => {
    vi.useFakeTimers()
    render(
      <DisplayShell display={display()} alarms={[]}>
        <div>content</div>
      </DisplayShell>,
    )
    vi.advanceTimersByTime(3000)
    expect(document.documentElement.style.cursor).toBe('none')

    window.dispatchEvent(new Event('pointermove'))
    expect(document.documentElement.style.cursor).toBe('')

    vi.advanceTimersByTime(2999)
    expect(document.documentElement.style.cursor).not.toBe('none')
    vi.advanceTimersByTime(1)
    expect(document.documentElement.style.cursor).toBe('none')
  })

  test('restores the cursor on unmount even if it was hidden', () => {
    vi.useFakeTimers()
    const { unmount } = render(
      <DisplayShell display={display()} alarms={[]}>
        <div>content</div>
      </DisplayShell>,
    )
    vi.advanceTimersByTime(3000)
    expect(document.documentElement.style.cursor).toBe('none')
    unmount()
    expect(document.documentElement.style.cursor).toBe('')
  })
})

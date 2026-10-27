import { Profiler } from 'react'
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { act, render, renderHook, screen } from '@testing-library/react'

import { MarineHeader } from '@/components/marine-header'
import { useVesselClock } from '@/hooks/use-vessel-clock'
import { installFakeTelemetryStream } from './helpers/fake-telemetry-stream'

// DESIGN.md's zero-state rule: a placeholder must not be styled as if it
// were real data. Before this fix, 'VESSEL NAME NOT SET' rendered in the
// same text-primary hero display type as an actual vessel name.
//
// The header reads the vessel-state store's fields (ADR 0218), so these tests
// drive the store through the stream, the way the boat does.

const stream = installFakeTelemetryStream()

let boatModel: string | null = 'Riviera 445'
vi.mock('@/hooks/use-app-config', () => ({
  useAppConfig: () => ({ boatModel }),
}))

function emit(payload: Record<string, unknown>) {
  stream.emit('vessel-state', payload)
}

const reported = { status: 'At Anchor', name: 'Pikorua', vessel_prefix: 'M/V', datetime: '2026-09-14T00:00:00Z' }

beforeEach(() => {
  boatModel = 'Riviera 445'
})

afterEach(() => {
  vi.useRealTimers()
})

describe('MarineHeader', () => {
  it('renders a real vessel name in the display treatment', () => {
    render(<MarineHeader />)
    emit(reported)

    expect(screen.getByText('M/V Pikorua')).toHaveClass('text-primary')
  })

  // The tile title sits on the card's top border and hangs into the card, so
  // the tile's own top padding is what keeps it off the vessel name.
  it('keeps the tile top padding that clears the title on the border', () => {
    boatModel = null
    const { container } = render(<MarineHeader />)
    emit(reported)
    expect(container.querySelector('[data-slot="card"]')).toHaveClass('pt-6')
  })

  it('renders an unset vessel name as a muted placeholder, not the hero display type', () => {
    render(<MarineHeader />)
    emit({ ...reported, name: '' })

    const name = screen.getByText('VESSEL NAME NOT SET')
    expect(name).not.toHaveClass('text-primary')
    expect(name).toHaveClass('text-muted-foreground')
  })

  it('renders an unset model as a distinguishable placeholder within the status line', () => {
    boatModel = null
    render(<MarineHeader />)
    emit(reported)

    expect(screen.getByText('MODEL NOT SET')).toHaveClass('italic')
  })

  it('shows the state the boat reports', () => {
    const { container } = render(<MarineHeader />)
    emit({ ...reported, status: 'Underway' })

    expect(container).toHaveTextContent('Riviera 445 · Underway')
  })

  it('claims no state before the boat has reported one', () => {
    const { container } = render(<MarineHeader />)

    expect(container).not.toHaveTextContent('At Anchor')
    expect(container).not.toHaveTextContent('Underway')
    expect(container).toHaveTextContent('Riviera 445')
    expect(container).not.toHaveTextContent('·')

    emit({ ...reported, status: '' })
    expect(container).not.toHaveTextContent('At Anchor')
  })

  describe('re-rendering', () => {
    function renderProfiled() {
      const onRender = vi.fn()
      render(<Profiler id="header" onRender={onRender}><MarineHeader /></Profiler>)
      emit(reported)
      return onRender
    }

    it('does not re-render on a vessel-state tick that changes nothing it shows', () => {
      const onRender = renderProfiled()
      const after = onRender.mock.calls.length

      emit({ ...reported, datetime: '2026-09-14T00:00:01Z', depth: 5.1, speed_over_ground_kts: 0.3 })
      emit({ ...reported, datetime: '2026-09-14T00:00:02Z', depth: 5.2 })

      expect(onRender.mock.calls.length).toBe(after)
    })

    it('does not re-render on the clock tick', () => {
      vi.useFakeTimers()
      const onRender = renderProfiled()
      // A clock reader elsewhere on the page keeps the one-second timer running.
      const clock = renderHook(() => useVesselClock())
      expect(clock.result.current.clock?.timePart).toBeTruthy()
      const after = onRender.mock.calls.length

      act(() => { vi.advanceTimersByTime(5000) })

      expect(clock.result.current.now?.toISOString()).toBe('2026-09-14T00:00:05.000Z')
      expect(onRender.mock.calls.length).toBe(after)
    })

    it('re-renders when the state it shows changes', () => {
      const onRender = renderProfiled()
      const after = onRender.mock.calls.length

      emit({ ...reported, status: 'Underway' })

      expect(onRender.mock.calls.length).toBeGreaterThan(after)
    })
  })
})

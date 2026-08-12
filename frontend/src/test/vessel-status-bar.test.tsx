import { describe, it, expect, vi } from 'vitest'
import { render, screen } from '@testing-library/react'

import { VesselStatusBar } from '@/components/vessel-status-bar'

// The badge is the only place a dead/reconnecting telemetry stream is
// surfaced to the operator (see use-telemetry-stream.ts) - a stream outage
// must not read as "Live" just because the last event it ever received is
// still sitting in state.

const mockSignalkConnected = vi.fn<() => boolean | null>()
const mockTelemetryStatus = vi.fn<() => 'connected' | 'reconnecting' | 'disconnected'>()

vi.mock('@/hooks/use-vessel-identity', () => ({
  useVesselIdentity: () => ({
    currentDate: 'MON, JAN 1, 2026',
    clock: { timePart: '12:00:00', meridiem: 'PM' },
    vesselStatus: 'At Anchor',
    boatName: null,
    boatModel: null,
    signalkConnected: mockSignalkConnected(),
  }),
}))

vi.mock('@/hooks/use-telemetry-stream', () => ({
  useTelemetryStatus: () => mockTelemetryStatus(),
}))

describe('VesselStatusBar', () => {
  it('shows Live when SignalK and the telemetry stream are both healthy', () => {
    mockSignalkConnected.mockReturnValue(true)
    mockTelemetryStatus.mockReturnValue('connected')

    render(<VesselStatusBar />)

    expect(screen.getByText('Live')).toBeInTheDocument()
  })

  it('shows Reconnecting when the telemetry stream is recovering from a permanent close', () => {
    mockSignalkConnected.mockReturnValue(true)
    mockTelemetryStatus.mockReturnValue('reconnecting')

    render(<VesselStatusBar />)

    expect(screen.getByText('Reconnecting')).toBeInTheDocument()
    expect(screen.queryByText('Live')).not.toBeInTheDocument()
  })

  it('shows No Signal when the telemetry stream has no subscribers connected', () => {
    mockSignalkConnected.mockReturnValue(true)
    mockTelemetryStatus.mockReturnValue('disconnected')

    render(<VesselStatusBar />)

    expect(screen.getByText('No Signal')).toBeInTheDocument()
  })

  it('shows No Signal when SignalK itself is unreachable, even if the browser stream is connected', () => {
    mockSignalkConnected.mockReturnValue(false)
    mockTelemetryStatus.mockReturnValue('connected')

    render(<VesselStatusBar />)

    expect(screen.getByText('No Signal')).toBeInTheDocument()
  })
})

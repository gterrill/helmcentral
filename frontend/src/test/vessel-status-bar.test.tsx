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

  // ── the logout affordance (ADR 0040) ─────────────────────────────────────

  it('shows no logout control when onLogout is not provided — mode:none, no behaviour change', () => {
    mockSignalkConnected.mockReturnValue(true)
    mockTelemetryStatus.mockReturnValue('connected')

    render(<VesselStatusBar />)

    expect(screen.queryByRole('button', { name: /log out/i })).not.toBeInTheDocument()
  })

  it('shows a logout control that calls onLogout, and names the signed-in user, under mode:signalk', () => {
    mockSignalkConnected.mockReturnValue(true)
    mockTelemetryStatus.mockReturnValue('connected')
    const onLogout = vi.fn()

    render(<VesselStatusBar username="skipper" onLogout={onLogout} />)

    const button = screen.getByRole('button', { name: /log out skipper/i })
    button.click()
    expect(onLogout).toHaveBeenCalledTimes(1)
  })
  // ── the Day / Night / Auto button ────────────────────────────────────────

  function renderBar(props: React.ComponentProps<typeof VesselStatusBar>) {
    mockSignalkConnected.mockReturnValue(true)
    mockTelemetryStatus.mockReturnValue('connected')
    return render(<VesselStatusBar {...props} />)
  }

  it('labels Day and Night as before and names the next mode in the cycle', () => {
    const { unmount } = renderBar({ isDark: false, themeMode: 'day' })
    expect(screen.getByRole('button', { name: 'Switch to night mode' })).toHaveTextContent('Day')
    unmount()

    renderBar({ isDark: true, themeMode: 'night' })
    expect(screen.getByRole('button', { name: 'Switch to auto mode' })).toHaveTextContent('Night')
  })

  it('without a themeMode keeps the two-state behaviour', () => {
    renderBar({ isDark: true })
    expect(screen.getByRole('button', { name: 'Switch to auto mode' })).toHaveTextContent('Night')
  })

  it('shows Auto with the theme in effect and when it ends', () => {
    renderBar({ isDark: true, themeMode: 'auto', autoUntil: '6:12 AM' })
    const button = screen.getByRole('button', { name: 'Auto (night until 6:12 AM). Switch to day mode' })
    expect(button).toHaveTextContent('Auto')
    expect(button).toHaveAttribute('title', 'Auto (night until 6:12 AM). Switch to day mode')
  })

  it('says day by day in Auto', () => {
    renderBar({ isDark: false, themeMode: 'auto', autoUntil: '5:48 PM' })
    expect(screen.getByRole('button', { name: 'Auto (day until 5:48 PM). Switch to day mode' })).toBeInTheDocument()
  })

  it('says plainly that Auto cannot decide when sunrise and sunset are missing', () => {
    renderBar({ isDark: true, themeMode: 'auto', autoUntil: null })
    const button = screen.getByRole('button', { name: /sunrise and sunset are not available/i })
    expect(button).toHaveAttribute('title', expect.stringMatching(/cannot decide/i))
    expect(button).toHaveTextContent('Auto')
  })

  it('calls onToggleDarkMode on click', () => {
    const onToggle = vi.fn()
    renderBar({ themeMode: 'day', onToggleDarkMode: onToggle })
    screen.getByRole('button', { name: 'Switch to night mode' }).click()
    expect(onToggle).toHaveBeenCalledTimes(1)
  })
})

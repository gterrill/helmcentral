import { render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { VesselStatusBar } from '@/components/vessel-status-bar'
import { installFakeTelemetryStream } from './helpers/fake-telemetry-stream'

// The header clock sits in the app shell, outside any tile boundary. A zone
// name this browser's ICU does not know (an older kiosk) must show as an error
// where the time goes, not take the whole UI down.
const stream = installFakeTelemetryStream()

describe('VesselStatusBar with a zone the browser does not know', () => {
  it('still renders, shows the error where the time goes, and logs it once', () => {
    const errorSpy = vi.spyOn(console, 'error').mockImplementation(() => {})
    stream.seed('vessel-state', { source: 'signalk', datetime: '2026-10-29T03:00:00Z', timezone: 'Not/AZone', timezone_source: 'fix' })

    const { rerender } = render(<VesselStatusBar />)
    rerender(<VesselStatusBar />)

    expect(screen.getByText('Live')).toBeInTheDocument()
    const err = screen.getByTestId('clock-zone-error')
    expect(err).toHaveTextContent('--:--')
    expect(err).toHaveAttribute('title', 'Zone Not/AZone unknown to this browser')
    expect(errorSpy.mock.calls.filter((c) => String(c[0]).includes('Not/AZone'))).toHaveLength(1)
    errorSpy.mockRestore()
  })

  it('shows the time as usual for a known zone', () => {
    stream.seed('vessel-state', { source: 'signalk', datetime: '2026-10-29T03:00:00Z', timezone: 'Australia/Adelaide', timezone_source: 'fix' })

    const { container } = render(<VesselStatusBar />)

    expect(screen.queryByTestId('clock-zone-error')).toBeNull()
    // 03:00 UTC is 13:30 in Adelaide (UTC+10:30 in October).
    expect(container.querySelector('time')).toHaveTextContent(/01:30/)
  })
})

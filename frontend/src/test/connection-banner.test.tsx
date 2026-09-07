import { render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { ConnectionBanner } from '@/components/connection-banner'
import type { TelemetryStatus } from '@/hooks/use-telemetry-stream'

let status: TelemetryStatus = 'connected'
vi.mock('@/hooks/use-telemetry-stream', () => ({ useTelemetryStatus: () => status }))

describe('ConnectionBanner', () => {
  it.each(['reconnecting', 'disconnected'] as const)('warns persistently when %s and clears only on recovery', (next) => {
    status = next
    const { rerender } = render(<ConnectionBanner />)
    const banner = screen.getByRole('alert')
    expect(banner).toHaveTextContent('Server connection unavailable')
    expect(banner).toHaveTextContent('Telemetry, anchor watch and alarm status may be out of date.')
    expect(banner).toHaveTextContent('Reconnecting automatically')
    expect(screen.queryByRole('button')).not.toBeInTheDocument()
    status = 'connected'
    rerender(<ConnectionBanner />)
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })
})
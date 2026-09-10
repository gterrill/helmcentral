import { render, screen } from '@testing-library/react'
import { describe, expect, test, vi } from 'vitest'

import { KioskStatusBadge } from '@/components/kiosk-status-badge'
import type { ActiveAlarm } from '@/hooks/use-alarms'

const { mockStatus } = vi.hoisted(() => ({ mockStatus: { value: 'connected' as 'connected' | 'reconnecting' | 'disconnected' } }))

vi.mock('@/hooks/use-telemetry-stream', () => ({
  useTelemetryStatus: () => mockStatus.value,
}))

function alarm(overrides: Partial<ActiveAlarm> = {}): ActiveAlarm {
  return {
    rule_id: 'r1', label: 'depth', path: 'environment.depth.belowTransducer', phase: 'active', state: 'alarm',
    value: 1, message: 'shallow', silenced: false, can_silence: true, can_acknowledge: true,
    ...overrides,
  }
}

describe('KioskStatusBadge', () => {
  test('renders nothing while connected with no alarms', () => {
    mockStatus.value = 'connected'
    const { container } = render(<KioskStatusBadge alarms={[]} />)
    expect(container).toBeEmptyDOMElement()
  })

  test('shows "No signal" while disconnected', () => {
    mockStatus.value = 'disconnected'
    render(<KioskStatusBadge alarms={[]} />)
    expect(screen.getByTestId('kiosk-signal-pill')).toHaveTextContent('No signal')
  })

  test('shows "Reconnecting" while reconnecting', () => {
    mockStatus.value = 'reconnecting'
    render(<KioskStatusBadge alarms={[]} />)
    expect(screen.getByTestId('kiosk-signal-pill')).toHaveTextContent('Reconnecting')
  })

  test('shows a loud pill while an unacknowledged alarm is active', () => {
    mockStatus.value = 'connected'
    render(<KioskStatusBadge alarms={[alarm()]} />)
    const pill = screen.getByTestId('kiosk-alarm-pill')
    expect(pill).toHaveTextContent('1 alarm')
    expect(pill.className).toMatch(/destructive/)
  })

  test('shows a muted pill once every shown alarm is acknowledged', () => {
    mockStatus.value = 'connected'
    render(<KioskStatusBadge alarms={[alarm({ phase: 'acknowledged' }), alarm({ phase: 'acknowledged', rule_id: 'r2' })]} />)
    const pill = screen.getByTestId('kiosk-alarm-pill')
    expect(pill).toHaveTextContent('2 alarms')
    expect(pill.className).not.toMatch(/destructive/)
  })
})

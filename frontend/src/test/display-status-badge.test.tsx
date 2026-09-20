import { render, screen } from '@testing-library/react'
import { describe, expect, test, vi } from 'vitest'

import { DisplayStatusBadge } from '@/components/display-status-badge'
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

describe('DisplayStatusBadge', () => {
  test('renders nothing while connected, with no alarms and no wake-lock trouble', () => {
    mockStatus.value = 'connected'
    const { container } = render(<DisplayStatusBadge alarms={[]} />)
    expect(container).toBeEmptyDOMElement()
  })

  test('shows "No signal" while disconnected', () => {
    mockStatus.value = 'disconnected'
    render(<DisplayStatusBadge alarms={[]} />)
    expect(screen.getByTestId('display-signal-pill')).toHaveTextContent('No signal')
  })

  test('shows "Reconnecting" while reconnecting', () => {
    mockStatus.value = 'reconnecting'
    render(<DisplayStatusBadge alarms={[]} />)
    expect(screen.getByTestId('display-signal-pill')).toHaveTextContent('Reconnecting')
  })

  test('shows a loud pill while an unacknowledged alarm is active', () => {
    mockStatus.value = 'connected'
    render(<DisplayStatusBadge alarms={[alarm()]} />)
    const pill = screen.getByTestId('display-alarm-pill')
    expect(pill).toHaveTextContent('1 alarm')
    // alarm()'s default state is 'alarm', which the severity ladder (ADR
    // 0116) paints red rather than the flat destructive token every rung
    // used to share regardless of severity.
    expect(pill.className).toContain('border-red-500')
  })

  // ADR 0116: the pill used to paint every unacknowledged alarm the same
  // destructive red as an emergency; a warn-only alarm on the wall now
  // reads amber instead.
  test('shows amber, not destructive, classes for a warn-only unacknowledged alarm', () => {
    mockStatus.value = 'connected'
    render(<DisplayStatusBadge alarms={[alarm({ state: 'warn' })]} />)
    const pill = screen.getByTestId('display-alarm-pill')
    expect(pill.className).toContain('border-amber-500')
    expect(pill.className).not.toContain('destructive')
  })

  test('shows a muted pill once every shown alarm is acknowledged', () => {
    mockStatus.value = 'connected'
    render(<DisplayStatusBadge alarms={[alarm({ phase: 'acknowledged' }), alarm({ phase: 'acknowledged', rule_id: 'r2' })]} />)
    const pill = screen.getByTestId('display-alarm-pill')
    expect(pill).toHaveTextContent('2 alarms')
    expect(pill.className).not.toMatch(/destructive/)
  })

  test('says nothing about the wake lock while it is off or actually held', () => {
    mockStatus.value = 'connected'
    const { container: offContainer } = render(<DisplayStatusBadge alarms={[]} wakeLockStatus="off" />)
    expect(offContainer).toBeEmptyDOMElement()
    const { container: heldContainer } = render(<DisplayStatusBadge alarms={[]} wakeLockStatus="held" />)
    expect(heldContainer).toBeEmptyDOMElement()
  })

  test('surfaces an unsupported wake lock rather than silently doing nothing', () => {
    mockStatus.value = 'connected'
    render(<DisplayStatusBadge alarms={[]} wakeLockStatus="unsupported" />)
    expect(screen.getByTestId('display-wake-lock-pill')).toHaveTextContent(/unsupported/i)
  })

  test('surfaces a denied wake lock rather than silently doing nothing', () => {
    mockStatus.value = 'connected'
    render(<DisplayStatusBadge alarms={[]} wakeLockStatus="denied" />)
    expect(screen.getByTestId('display-wake-lock-pill')).toHaveTextContent(/denied/i)
  })
})

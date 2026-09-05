import { render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { AlarmsDrawer } from '@/components/alarms-drawer'
import type { ActiveAlarm } from '@/hooks/use-alarms'

// Only the two data hooks are stubbed; the drawer renders exactly as it does
// in the app otherwise (see alarms-drawer-actions.test.tsx for the pattern).
vi.mock('@/hooks/use-alarm-rules', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/hooks/use-alarm-rules')>()),
  useAlarmRules: () => ({
    rules: [], loading: false, error: null,
    createRule: vi.fn(), updateRule: vi.fn(), deleteRule: vi.fn(),
  }),
  useAlarmLog: () => ({ entries: [], refresh: vi.fn().mockResolvedValue(undefined) }),
}))

function makeAlarm(overrides: Partial<ActiveAlarm> = {}): ActiveAlarm {
  return {
    rule_id: 'helmcentral:anchor-drag',
    label: 'Anchor dragging',
    path: 'notifications.navigation.anchor',
    phase: 'active',
    state: 'alarm',
    value: 62,
    message: 'Anchor dragging: 62m from where it was set',
    silenced: false,
    can_silence: false,
    can_acknowledge: true,
    ...overrides,
  }
}

function renderDrawer(alarms: ActiveAlarm[]) {
  render(
    <AlarmsDrawer
      alarms={alarms}
      onAcknowledge={vi.fn().mockResolvedValue(undefined)}
      onSilence={vi.fn().mockResolvedValue(undefined)}
    />,
  )
}

describe('AlarmsDrawer active alarm card', () => {
  it('renders the rule label as headline, the operator sentence, the path once, and the acknowledged pill', () => {
    renderDrawer([
      makeAlarm({
        rule_id: 'helmcentral:barometer-falling',
        label: 'Barometer falling',
        path: 'helmcentral.environment.pressureRate',
        op: 'below',
        threshold: -0.03,
        hysteresis: 0.01,
        clear_value: -0.02,
        value: -0.0305,
        unit: 'Pa/s',
        state: 'warn',
        phase: 'acknowledged',
        raised_at: '2026-09-06T05:41:00',
        acked_at: '2026-09-06T06:12:00',
        silenced: true,
        can_silence: false,
        can_acknowledge: false,
      }),
    ])

    // Headline is the label, not the machine path.
    expect(screen.getByText('Barometer falling')).toBeInTheDocument()

    // The sailor's own sentence, not the raw Pa/s rule condition.
    expect(
      screen.getByText('Falling 1.1 mb/hr. Clears once the fall eases to 0.7 mb/hr.'),
    ).toBeInTheDocument()

    // The path appears exactly once, in its own case.
    const pathMatches = screen.getAllByText('helmcentral.environment.pressureRate')
    expect(pathMatches).toHaveLength(1)
    expect(pathMatches[0]).not.toHaveClass('uppercase')

    // No missing-time placeholder anywhere on the card.
    expect(screen.queryByText(/--/)).toBeNull()

    expect(screen.getByText('Acknowledged · still live')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /acknowledge/i })).toBeNull()
    expect(screen.queryByRole('button', { name: /silence/i })).toBeNull()
  })

  it('renders a bus notification message plus the clears-elsewhere line, and omits Raised when absent', () => {
    renderDrawer([
      makeAlarm({
        rule_id: 'notifications:radar-guard-zone-1',
        label: 'Radar guard zone 1',
        path: 'radar.fur6424A.guardZone.1',
        message: 'Radar guard zone 1: target 100000294 acquired',
        state: 'alert',
        phase: 'active',
        acked_at: undefined,
        raised_at: undefined,
      }),
    ])

    expect(
      screen.getByText('Radar guard zone 1: target 100000294 acquired Clears when the source clears it.'),
    ).toBeInTheDocument()
    expect(screen.queryByText(/^Raised/)).toBeNull()
  })

  it('renders a generic above rule sentence for a voltage alarm', () => {
    renderDrawer([
      makeAlarm({
        rule_id: 'house-bank-low',
        label: 'House bank low',
        path: 'electrical.batteries.house.voltage',
        op: 'above',
        threshold: 14.9,
        clear_value: 14.4,
        value: 14.9,
        unit: 'V',
        state: 'alarm',
        phase: 'active',
      }),
    ])

    expect(screen.getByText('Now 14.9 V. Clears below 14.4 V.')).toBeInTheDocument()
  })

  it('gives alert and warn cards different colour classes', () => {
    renderDrawer([
      makeAlarm({ rule_id: 'a', label: 'Alert one', state: 'alert' }),
      makeAlarm({ rule_id: 'b', label: 'Warn one', state: 'warn' }),
    ])

    const alertHeadline = screen.getByText('Alert one')
    const warnHeadline = screen.getByText('Warn one')
    expect(alertHeadline.className).not.toBe(warnHeadline.className)
  })
})

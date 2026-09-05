import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { AlarmsDrawer } from '@/components/alarms-drawer'
import type { AlarmRule } from '@/hooks/use-alarm-rules'
import type { ActiveAlarm } from '@/hooks/use-alarms'
import type { SignalKPath } from '@/hooks/use-signalk-paths'

/**
 * Copied from alarms-drawer-rules-list.test.tsx's module-mock setup: the
 * Rules tile needs real rule fixtures, plus (new for this fix) SignalK path
 * units so the row can convert a threshold into operator units.
 */
const rulesMock = vi.hoisted(() => ({ rules: [] as AlarmRule[] }))
const pathsMock = vi.hoisted(() => ({ paths: [] as SignalKPath[] }))
const deleteRuleMock = vi.hoisted(() => vi.fn())

vi.mock('@/hooks/use-alarm-rules', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/hooks/use-alarm-rules')>()),
  useAlarmRules: () => ({
    rules: rulesMock.rules, loading: false, error: null,
    createRule: vi.fn(), updateRule: vi.fn(), deleteRule: deleteRuleMock,
  }),
  useAlarmLog: () => ({ entries: [], refresh: vi.fn().mockResolvedValue(undefined) }),
}))

vi.mock('@/hooks/use-signalk-paths', () => ({
  useSignalKPaths: () => ({ paths: pathsMock.paths }),
}))

function rule(overrides: Partial<AlarmRule> = {}): AlarmRule {
  return {
    id: 'rule-1',
    enabled: true,
    path: 'environment.pressureRate',
    label: 'Barometer falling',
    op: 'below',
    value: -0.03,
    hysteresis: 0.01,
    dwell_seconds: 1800,
    stale_after_seconds: 0,
    state: 'warn',
    methods: ['visual', 'sound'],
    notify: null,
    escalate_after_seconds: 0,
    ...overrides,
  }
}

function renderDrawer(rules: AlarmRule[], alarms: ActiveAlarm[] = [], paths: SignalKPath[] = []) {
  rulesMock.rules = rules
  pathsMock.paths = paths
  deleteRuleMock.mockClear()
  const { container } = render(<AlarmsDrawer alarms={alarms} onAcknowledge={vi.fn()} onSilence={vi.fn()} />)
  return container
}

describe('AlarmsDrawer rules grouping', () => {
  it('groups rules by domain, domains alphabetical, with a header per group', () => {
    const container = renderDrawer([
      rule({ id: 'r1', path: 'radar.fur6424A.guardZone.1', label: 'Guard zone 1' }),
      rule({ id: 'r2', path: 'helmcentral.environment.pressureRate', label: 'Barometer falling' }),
      rule({ id: 'r3', path: 'electrical.batteries.house.voltage', label: 'House bank low' }),
    ])

    expect(screen.getByText('electrical')).toBeInTheDocument()
    expect(screen.getByText('environment')).toBeInTheDocument()
    expect(screen.getByText('radar')).toBeInTheDocument()

    const text = container.textContent ?? ''
    const electricalAt = text.indexOf('electrical')
    const environmentAt = text.indexOf('environment')
    const radarAt = text.indexOf('radar')
    expect(electricalAt).toBeGreaterThan(-1)
    expect(electricalAt).toBeLessThan(environmentAt)
    expect(environmentAt).toBeLessThan(radarAt)
  })

  it('orders rule labels alphabetically within a group', () => {
    const container = renderDrawer([
      rule({ id: 'r1', path: 'environment.pressureRate', label: 'plummeting' }),
      rule({ id: 'r2', path: 'environment.pressureRate', label: 'Barometer falling' }),
    ])

    const text = container.textContent ?? ''
    expect(text.indexOf('Barometer falling')).toBeLessThan(text.indexOf('plummeting'))
  })
})

describe('AlarmsDrawer rules firing pill', () => {
  function activeAlarm(overrides: Partial<ActiveAlarm> = {}): ActiveAlarm {
    return {
      rule_id: 'rule-1',
      label: 'Barometer falling',
      path: 'helmcentral.environment.pressureRate',
      phase: 'active',
      state: 'warn',
      value: -0.03,
      message: '',
      silenced: false,
      can_silence: false,
      can_acknowledge: true,
      ...overrides,
    }
  }

  it('shows a firing pill when the rule has an active alarm', () => {
    renderDrawer([rule({ id: 'rule-1' })], [activeAlarm({ phase: 'active' })])

    expect(screen.getByText('firing')).toBeInTheDocument()
  })

  it('shows a firing pill when the rule has an acknowledged alarm', () => {
    renderDrawer([rule({ id: 'rule-1' })], [activeAlarm({ phase: 'acknowledged' })])

    expect(screen.getByText('firing')).toBeInTheDocument()
  })

  it('shows no firing pill for a rule with no matching live alarm', () => {
    renderDrawer([rule({ id: 'rule-1' })], [])

    expect(screen.queryByText('firing')).toBeNull()
  })
})

describe('AlarmsDrawer rule condition line', () => {
  it('formats a below rule with hysteresis and dwell in operator units', () => {
    const container = renderDrawer(
      [rule({ id: 'rule-1', op: 'below', value: -0.03, hysteresis: 0.01, dwell_seconds: 1800, state: 'warn' })],
      [],
      [{ path: 'environment.pressureRate', units: 'Pa/s' }],
    )

    expect(container.textContent).toContain('below -1.1 mb/hr · clears at -0.7 mb/hr · for 30m · warn')
  })

  it('formats a stale rule as "no data for Ns"', () => {
    const container = renderDrawer([
      rule({ id: 'rule-1', op: 'stale', stale_after_seconds: 45, state: 'alert' }),
    ])

    expect(container.textContent).toContain('no data for 45s · alert')
  })
})

describe('AlarmsDrawer rule delete confirmation', () => {
  it('does not delete on the first click, and deletes on the second', () => {
    renderDrawer([rule({ id: 'rule-1', label: 'Barometer falling' })])

    fireEvent.click(screen.getByRole('button', { name: /delete barometer falling/i }))
    expect(deleteRuleMock).not.toHaveBeenCalled()

    expect(screen.getByText(/delete barometer falling\?/i)).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: /delete rule/i }))
    expect(deleteRuleMock).toHaveBeenCalledTimes(1)
    expect(deleteRuleMock).toHaveBeenCalledWith('rule-1')
  })

  it('mentions firing in the confirmation when the rule is firing', () => {
    renderDrawer(
      [rule({ id: 'rule-1', label: 'Barometer falling' })],
      [{
        rule_id: 'rule-1',
        label: 'Barometer falling',
        path: 'helmcentral.environment.pressureRate',
        phase: 'active',
        state: 'warn',
        value: -0.03,
        message: '',
        silenced: false,
        can_silence: false,
        can_acknowledge: true,
      }],
    )

    fireEvent.click(screen.getByRole('button', { name: /delete barometer falling/i }))
    expect(screen.getByText(/it is firing now\./i)).toBeInTheDocument()
  })

  it('cancels the pending delete when Keep is clicked', () => {
    renderDrawer([rule({ id: 'rule-1', label: 'Barometer falling' })])

    fireEvent.click(screen.getByRole('button', { name: /delete barometer falling/i }))
    fireEvent.click(screen.getByRole('button', { name: /keep/i }))

    expect(deleteRuleMock).not.toHaveBeenCalled()
    expect(screen.queryByText(/delete barometer falling\?/i)).toBeNull()
    expect(screen.getByRole('button', { name: /delete barometer falling/i })).toBeInTheDocument()
  })
})

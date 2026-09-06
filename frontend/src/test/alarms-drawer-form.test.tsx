import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { AlarmsDrawer } from '@/components/alarms-drawer'
import type { AlarmRule } from '@/hooks/use-alarm-rules'
import type { SignalKPath } from '@/hooks/use-signalk-paths'

/**
 * Rules are a prop now (lifted into App, see alarms-drawer-actions.test.tsx),
 * so this file passes a rules array straight into the component; only
 * useAlarmLog and useSignalKPaths still need stubbing.
 */
const pathsMock = vi.hoisted(() => ({ paths: [] as SignalKPath[] }))

vi.mock('@/hooks/use-alarm-rules', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/hooks/use-alarm-rules')>()),
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
    hysteresis: 0,
    dwell_seconds: 10,
    stale_after_seconds: 0,
    state: 'warn',
    methods: ['visual', 'sound'],
    notify: null,
    escalate_after_seconds: 0,
    ...overrides,
  }
}

function renderDrawer(rules: AlarmRule[], paths: SignalKPath[] = []) {
  pathsMock.paths = paths
  render(
    <AlarmsDrawer
      alarms={[]}
      onAcknowledge={vi.fn()}
      onSilence={vi.fn()}
      rules={rules}
      loading={false}
      error={null}
      createRule={vi.fn()}
      updateRule={vi.fn()}
      deleteRule={vi.fn()}
    />,
  )
}

describe('RuleForm advanced disclosure', () => {
  it('hides Deadband, Must hold for and Escalate after until Advanced is opened when creating', () => {
    renderDrawer([])

    fireEvent.click(screen.getByRole('button', { name: /add rule/i }))

    expect(screen.queryByLabelText(/deadband/i)).toBeNull()
    expect(screen.queryByLabelText(/must hold for/i)).toBeNull()
    expect(screen.queryByLabelText(/escalate after/i)).toBeNull()

    const advancedToggle = screen.getByRole('button', { name: /advanced/i })
    expect(advancedToggle).toHaveAttribute('aria-expanded', 'false')

    fireEvent.click(advancedToggle)

    expect(screen.getByLabelText(/deadband/i)).toBeInTheDocument()
    expect(screen.getByLabelText(/must hold for/i)).toBeInTheDocument()
    expect(screen.getByLabelText(/escalate after/i)).toBeInTheDocument()
    expect(advancedToggle).toHaveAttribute('aria-expanded', 'true')
  })

  it('opens Advanced by default when editing a rule with a non-default dwell', () => {
    renderDrawer([rule({ dwell_seconds: 1800 })])

    fireEvent.click(screen.getByRole('button', { name: /edit barometer falling/i }))

    const dwellInput = screen.getByLabelText(/must hold for/i) as HTMLInputElement
    expect(dwellInput).toBeInTheDocument()
    expect(dwellInput.value).toBe('1800')
    expect(screen.getByRole('button', { name: /advanced/i })).toHaveAttribute('aria-expanded', 'true')
  })

  it('keeps Advanced closed by default when editing a rule that matches all the defaults', () => {
    // newAlarmRuleDraft() defaults: hysteresis 0, dwell_seconds 10, escalate_after_seconds 0.
    renderDrawer([rule({ hysteresis: 0, dwell_seconds: 10, escalate_after_seconds: 0 })])

    fireEvent.click(screen.getByRole('button', { name: /edit barometer falling/i }))

    expect(screen.getByRole('button', { name: /advanced/i })).toHaveAttribute('aria-expanded', 'false')
    expect(screen.queryByLabelText(/must hold for/i)).toBeNull()
  })
})

describe('RuleForm threshold unit', () => {
  it('shows the SI unit on the Threshold label and a converted hint when the path has a known unit', () => {
    renderDrawer([rule({ path: 'helmcentral.environment.pressureRate', value: -0.03 })],
      [{ path: 'helmcentral.environment.pressureRate', units: 'Pa/s' }])

    fireEvent.click(screen.getByRole('button', { name: /edit barometer falling/i }))

    expect(screen.getByText('Threshold (Pa/s)')).toBeInTheDocument()
    expect(screen.getByText('= -1.1 mb/hr')).toBeInTheDocument()
  })

  it('leaves the Threshold label plain and shows no hint when the path has no known unit', () => {
    renderDrawer([rule({ path: 'some.unknown.path', value: 5 })], [])

    fireEvent.click(screen.getByRole('button', { name: /edit barometer falling/i }))

    expect(screen.getByText('Threshold')).toBeInTheDocument()
    expect(screen.queryByText(/^=/)).toBeNull()
  })
})

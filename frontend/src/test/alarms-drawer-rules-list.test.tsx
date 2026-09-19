import { render } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { AlarmsDrawer } from '@/components/alarms-drawer'
import type { AlarmRule } from '@/hooks/use-alarm-rules'

/**
 * Rules are a prop now (lifted into App, see alarms-drawer-actions.test.tsx),
 * so this file passes real rule fixtures straight into the component;
 * useAlarmLog is the only hook left to stub.
 */
vi.mock('@/hooks/use-alarm-rules', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/hooks/use-alarm-rules')>()),
  useAlarmLog: () => ({ entries: [], refresh: vi.fn().mockResolvedValue(undefined) }),
}))

function rule(overrides: Partial<AlarmRule> = {}): AlarmRule {
  return {
    id: 'rule-1',
    enabled: true,
    path: 'environment.pressureRate',
    label: 'Barometer falling',
    op: 'below',
    value: -0.027777777777777776,
    hysteresis: 0.005555555555555556,
    dwell_seconds: 1800,
    stale_after_seconds: 0,
    state: 'warn',
    methods: ['visual', 'sound'],
    notify: null,
    escalate_after_seconds: 0,
    ...overrides,
  }
}

// The threshold sits in a <p> alongside the path and state as separate text
// nodes (`{path} · {op} {value} · <span>{state}</span>`), so a regex looking
// for "below -0.03" spans more than one node. getByText only matches a
// single node's own text, so the row's full textContent is asserted on
// directly instead.
function renderDrawer(rules: AlarmRule[]) {
  const { container } = render(
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
      collisionTuningUrl={null}
      forecastWarnings={null}
    />,
  )
  return container
}

describe('AlarmsDrawer rules list', () => {
  // The raw threshold is whatever precision the unit conversion happened to
  // produce; the operator reads a rounded number on the panel, not a
  // debugger dump (see the alarm message formatting this mirrors).
  it('rounds a repeating-decimal threshold to two decimal places', () => {
    const container = renderDrawer([rule()])

    expect(container.textContent).toMatch(/below -0\.03/)
    expect(container.textContent).not.toMatch(/0\.027777/)
  })

  it('does not pad a whole-number threshold with trailing zeros', () => {
    const container = renderDrawer([rule({ label: 'Depth alarm', value: -150 })])

    expect(container.textContent).toMatch(/below -150\D/)
    expect(container.textContent).not.toMatch(/-150\.00/)
  })
})

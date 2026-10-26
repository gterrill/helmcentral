import { render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { AlarmsDrawer } from '@/components/alarms-drawer'
import type { ActiveAlarm } from '@/hooks/use-alarms'

vi.mock('@/hooks/use-alarm-rules', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/hooks/use-alarm-rules')>()),
  useAlarmLog: () => ({ entries: [], refresh: vi.fn().mockResolvedValue(undefined) }),
}))

const tankAlarm: ActiveAlarm = {
  rule_id: 'tank:abc:warn',
  label: 'Fuel Port Fwd Low',
  path: 'tanks.fuel.0.currentLevel',
  phase: 'active',
  state: 'warn',
  value: 0.19,
  message: '19% · 228 L',
  op: 'below',
  threshold: 0.2,
  hysteresis: 0.05,
  clear_value: 0.25,
  unit: 'ratio',
  raised_at: '2026-10-27T02:14:00Z',
  silenced: false,
  can_silence: false,
  can_acknowledge: true,
}

describe('AlarmsDrawer tank card (ADR 0214)', () => {
  it('says the level and volume in the body and how it clears in the footer', () => {
    render(
      <AlarmsDrawer
        alarms={[tankAlarm]}
        onAcknowledge={vi.fn().mockResolvedValue(undefined)}
        onSilence={vi.fn().mockResolvedValue(undefined)}
        rules={[]}
        loading={false}
        error={null}
        createRule={vi.fn()}
        updateRule={vi.fn()}
        deleteRule={vi.fn()}
        collisionTuningUrl={null}
        forecastWarnings={null}
      />,
    )
    expect(screen.getByText('Fuel Port Fwd Low')).toBeInTheDocument()
    const body = screen.getByText('19% · 228 L')
    expect(body.textContent).not.toMatch(/clears/i)
    expect(screen.getByText(/Clears above 25%/)).toBeInTheDocument()
  })
})

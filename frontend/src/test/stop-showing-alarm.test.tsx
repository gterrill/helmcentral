import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { AlarmsDrawer } from '@/components/alarms-drawer'
import type { ActiveAlarm } from '@/hooks/use-alarms'

const ignore = vi.fn().mockResolvedValue(undefined)

vi.mock('@/hooks/use-ignored-alarms', () => ({
  useIgnoredAlarms: () => ({ alarms: [], loading: false, error: null, ignore, restore: vi.fn(), refresh: vi.fn() }),
}))
vi.mock('@/hooks/use-alarm-rules', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/hooks/use-alarm-rules')>()),
  useAlarmLog: () => ({ entries: [], refresh: vi.fn().mockResolvedValue(undefined) }),
}))

function alarm(overrides: Partial<ActiveAlarm> = {}): ActiveAlarm {
  return {
    rule_id: 'notifications:perpendicularPassed',
    label: 'Waypoint Passed',
    path: 'notifications.perpendicularPassed',
    phase: 'active',
    state: 'alarm',
    value: 0,
    message: 'The boat has passed the active waypoint.',
    silenced: false,
    can_silence: true,
    can_acknowledge: true,
    ignorable: true,
    ...overrides,
  }
}

function renderDrawer(alarms: ActiveAlarm[]) {
  render(
    <AlarmsDrawer
      alarms={alarms}
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
}

describe('Stop showing on a network alarm card', () => {
  beforeEach(() => ignore.mockClear())

  it('is offered when the server marks the alarm ignorable', () => {
    renderDrawer([alarm()])
    expect(screen.getByRole('button', { name: 'Stop showing Waypoint Passed' })).toBeTruthy()
  })

  it('is absent when the alarm is not ignorable, even with acknowledge on offer', () => {
    renderDrawer([alarm({ ignorable: false })])
    expect(screen.queryByRole('button', { name: /stop showing/i })).toBeNull()
    expect(screen.getByRole('button', { name: /acknowledge/i })).toBeTruthy()
  })

  it('is absent on an alarm that carries no ignorable flag (a rule alarm)', () => {
    renderDrawer([alarm({ rule_id: 'a3f1-rule', ignorable: undefined })])
    expect(screen.queryByRole('button', { name: /stop showing/i })).toBeNull()
  })

  it('confirms first and then ignores the alarm kind', async () => {
    renderDrawer([alarm()])

    fireEvent.click(screen.getByRole('button', { name: 'Stop showing Waypoint Passed' }))
    expect(await screen.findByText('Stop showing Waypoint Passed?')).toBeTruthy()
    expect(screen.getByText(/until you restore it in Settings/)).toBeTruthy()
    expect(ignore).not.toHaveBeenCalled()

    fireEvent.click(screen.getAllByRole('button', { name: 'Stop showing' }).at(-1)!)
    await waitFor(() => expect(ignore).toHaveBeenCalledWith('perpendicularPassed'))
  })

  it('does nothing when the confirmation is cancelled', async () => {
    renderDrawer([alarm()])
    fireEvent.click(screen.getByRole('button', { name: 'Stop showing Waypoint Passed' }))
    fireEvent.click(await screen.findByRole('button', { name: 'Cancel' }))
    expect(ignore).not.toHaveBeenCalled()
  })
})

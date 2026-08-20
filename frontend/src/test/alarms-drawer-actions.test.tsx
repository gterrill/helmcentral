import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { AlarmsDrawer } from '@/components/alarms-drawer'
import type { ActiveAlarm } from '@/hooks/use-alarms'

// Only the two data hooks are stubbed; the rule-form constants stay real so
// the drawer renders exactly as it does in the app.
vi.mock('@/hooks/use-alarm-rules', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/hooks/use-alarm-rules')>()),
  useAlarmRules: () => ({
    rules: [], loading: false, error: null,
    createRule: vi.fn(), updateRule: vi.fn(), deleteRule: vi.fn(),
  }),
  useAlarmLog: () => ({ entries: [], refresh: vi.fn().mockResolvedValue(undefined) }),
}))

/**
 * SignalK separates silencing from acknowledging, and says per notification
 * which of the two it supports. The drawer offers exactly what the alarm
 * advertises rather than assuming both (ADR 0038).
 */
function alarm(overrides: Partial<ActiveAlarm> = {}): ActiveAlarm {
  return {
    rule_id: 'notifications:arrivalCircleEntered',
    label: 'arrivalCircleEntered',
    path: 'notifications.arrivalCircleEntered',
    phase: 'active',
    state: 'alarm',
    value: 0,
    message: 'WP arrival circle entered!',
    silenced: false,
    can_silence: true,
    can_acknowledge: true,
    ...overrides,
  }
}

function renderDrawer(alarms: ActiveAlarm[], handlers: Partial<{
  onAcknowledge: (id: string) => Promise<void>
  onSilence: (id: string) => Promise<void>
}> = {}) {
  const onAcknowledge = handlers.onAcknowledge ?? vi.fn().mockResolvedValue(undefined)
  const onSilence = handlers.onSilence ?? vi.fn().mockResolvedValue(undefined)
  render(<AlarmsDrawer alarms={alarms} onAcknowledge={onAcknowledge} onSilence={onSilence} />)
  return { onAcknowledge, onSilence }
}

describe('AlarmsDrawer alert actions', () => {
  it('offers both actions when the notification supports both', () => {
    renderDrawer([alarm()])

    expect(screen.getByRole('button', { name: /silence/i })).toBeTruthy()
    expect(screen.getByRole('button', { name: /acknowledge/i })).toBeTruthy()
  })

  it('calls silence and acknowledge with the alarm id', () => {
    const { onAcknowledge, onSilence } = renderDrawer([alarm()])

    fireEvent.click(screen.getByRole('button', { name: /silence/i }))
    expect(onSilence).toHaveBeenCalledWith('notifications:arrivalCircleEntered')

    fireEvent.click(screen.getByRole('button', { name: /acknowledge/i }))
    expect(onAcknowledge).toHaveBeenCalledWith('notifications:arrivalCircleEntered')
  })

  // A rule-driven alarm has no silence action, so offering one would put a
  // button on screen whose only outcome is an error.
  it('offers acknowledge alone when the alarm cannot be silenced', () => {
    renderDrawer([alarm({ rule_id: 'a3f1-rule', can_silence: false })])

    expect(screen.queryByRole('button', { name: /silence/i })).toBeNull()
    expect(screen.getByRole('button', { name: /acknowledge/i })).toBeTruthy()
  })

  // Silenced is not acknowledged: the alarm has stopped sounding but is still
  // live, so it stays listed and can still be acknowledged.
  it('marks a silenced alarm and keeps acknowledge available', () => {
    renderDrawer([alarm({ silenced: true, can_silence: false })])

    expect(screen.queryByRole('button', { name: /silence/i })).toBeNull()
    expect(screen.getByRole('button', { name: /acknowledge/i })).toBeTruthy()
    expect(screen.getByText(/silenced/i)).toBeTruthy()
  })

  it('offers nothing for an emergency, which cannot be silenced or acknowledged', () => {
    renderDrawer([alarm({ state: 'emergency', can_silence: false, can_acknowledge: false })])

    expect(screen.queryByRole('button', { name: /silence/i })).toBeNull()
    expect(screen.queryByRole('button', { name: /acknowledge/i })).toBeNull()
  })

  it('shows an acknowledged alarm as acknowledged', () => {
    renderDrawer([alarm({ phase: 'acknowledged', silenced: true, can_silence: false, can_acknowledge: false })])

    expect(screen.getByText(/acknowledged/i)).toBeTruthy()
    expect(screen.queryByRole('button', { name: /^acknowledge$/i })).toBeNull()
  })

  it('surfaces a refusal from the server instead of swallowing it', async () => {
    const onSilence = vi.fn().mockRejectedValue(new Error('SignalK does not offer that action'))
    renderDrawer([alarm()], { onSilence })

    fireEvent.click(screen.getByRole('button', { name: /silence/i }))
    expect(await screen.findByText(/SignalK does not offer that action/i)).toBeTruthy()
  })
})

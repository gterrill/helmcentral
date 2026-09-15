import { render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { AlarmsDrawer } from '@/components/alarms-drawer'
import type { ActiveAlarm } from '@/hooks/use-alarms'

// useAlarmLog is the one hook the drawer still owns itself; rules now come
// in as props (see alarms-drawer-actions.test.tsx for the pattern), so this
// file passes empty rules directly instead of mocking useAlarmRules.
vi.mock('@/hooks/use-alarm-rules', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/hooks/use-alarm-rules')>()),
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

function collisionAlarm(overrides: Partial<ActiveAlarm> = {}): ActiveAlarm {
  return makeAlarm({
    rule_id: 'notifications:navigation.closestApproach@urn:mrn:imo:mmsi:503009670',
    label: 'navigation.closestApproach',
    path: 'notifications.navigation.closestApproach',
    state: 'warn',
    message: 'EPICURUS I - CPA WARNING',
    ...overrides,
  })
}

function renderDrawer(alarms: ActiveAlarm[], collisionTuningUrl: string | null) {
  const { container } = render(
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
      collisionTuningUrl={collisionTuningUrl}
    />,
  )
  return container
}

const TUNING_URL = 'http://192.168.50.240:3000/signalk-ais-target-prioritizer/'

describe('AlarmsDrawer collision tuning link', () => {
  it('links a collision alarm card to the AIS Target Prioritizer webapp when a SignalK address is configured', () => {
    renderDrawer([collisionAlarm()], TUNING_URL)

    const link = screen.getByRole('link', { name: 'Adjust thresholds in AIS Target Prioritizer' })
    expect(link).toHaveAttribute('href', TUNING_URL)
    expect(link).toHaveAttribute('target', '_blank')
    expect(link).toHaveAttribute('rel', 'noreferrer')
  })

  it('does not add the link to a non-collision bus alarm', () => {
    renderDrawer(
      [makeAlarm({
        rule_id: 'notifications:radar-guard-zone-1',
        label: 'Radar guard zone 1',
        path: 'notifications.radar.fur6424A.guardZone.1',
        message: 'Radar guard zone 1: target 100000294 acquired',
        state: 'alert',
      })],
      TUNING_URL,
    )

    expect(screen.queryByRole('link', { name: /Adjust thresholds/i })).toBeNull()
  })

  it('does not add the link to a collision alarm when no SignalK address is configured', () => {
    renderDrawer([collisionAlarm()], null)

    expect(screen.queryByRole('link', { name: /Adjust thresholds/i })).toBeNull()
  })
})

// ADR 0098: the COLREGS "situation + role" line, its own line under
// alarmConditionSentence and above the tuning link, rendered only when the
// backend set one.
describe('AlarmsDrawer COLREGS encounter line', () => {
  const ENCOUNTER = "Crossing, she is on our starboard bow (040° rel). We give way (Rule 15): alter to starboard, pass astern."

  it('renders the encounter line when the alarm carries one', () => {
    renderDrawer([collisionAlarm({ encounter: ENCOUNTER })], TUNING_URL)

    expect(screen.getByText(ENCOUNTER)).toBeInTheDocument()
  })

  it('renders nothing extra when the alarm carries no encounter line', () => {
    const { container } = render(
      <AlarmsDrawer
        alarms={[collisionAlarm()]}
        onAcknowledge={vi.fn().mockResolvedValue(undefined)}
        onSilence={vi.fn().mockResolvedValue(undefined)}
        rules={[]}
        loading={false}
        error={null}
        createRule={vi.fn()}
        updateRule={vi.fn()}
        deleteRule={vi.fn()}
        collisionTuningUrl={TUNING_URL}
      />,
    )

    expect(screen.queryByText(/Rule \d/)).toBeNull()
    expect(container).toBeTruthy()
  })

  it('does not render an encounter line for a non-collision alarm even if one were somehow set', () => {
    renderDrawer(
      [makeAlarm({
        rule_id: 'notifications:radar-guard-zone-1',
        label: 'Radar guard zone 1',
        path: 'notifications.radar.fur6424A.guardZone.1',
        message: 'Radar guard zone 1: target 100000294 acquired',
        state: 'alert',
      })],
      TUNING_URL,
    )

    expect(screen.queryByText(/Rule \d/)).toBeNull()
  })
})

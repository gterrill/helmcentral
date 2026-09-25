import { fireEvent, render, screen, waitFor } from '@testing-library/react'
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
      forecastWarnings={null}
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
        forecastWarnings={null}
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

// The anomaly-detection "why" line, frozen at raise (anomaly_detector.go).
// Its own line, rendered only when the backend set one, and never invented
// client-side.
describe('AlarmsDrawer anomaly evidence line', () => {
  const EVIDENCE = 'House bank 96% SoC, 28.90 V, charging 42 A'

  it('renders the evidence line when the alarm carries one', () => {
    renderDrawer(
      [makeAlarm({
        rule_id: 'helmcentral:anomaly-full-bank-charging-warn',
        label: 'Charging into a full house bank',
        path: 'helmcentral.anomaly.battery.fullBankCharging',
        message: 'Charging into a full house bank: 1, clears below 0.5',
        state: 'warn',
        evidence: EVIDENCE,
      })],
      TUNING_URL,
    )

    expect(screen.getByText(EVIDENCE)).toBeInTheDocument()
  })

  it('renders nothing extra when the alarm carries no evidence', () => {
    renderDrawer([makeAlarm()], TUNING_URL)
    expect(screen.queryByText(EVIDENCE)).toBeNull()
  })
})

// "Ignore this sensor" (round 2, then code review finding 9): the
// frozen/impossible/silent-source count alarms name their offending
// paths/sources in live_evidence (comma-separated), read fresh from the
// live detector state on every poll rather than from the evidence frozen
// at raise -- a long-running count alarm's specific offenders can drift
// while the count itself stays above threshold, and the ignore action must
// offer what is failing now, not whatever first tripped the alarm. Each
// identifier gets its own small action calling POST
// /api/alarms/ignored-sensors.
describe('AlarmsDrawer ignore-this-sensor action', () => {
  function frozenAlarm(overrides: Partial<ActiveAlarm> = {}): ActiveAlarm {
    return makeAlarm({
      rule_id: 'helmcentral:anomaly-frozen-sensor',
      label: 'Frozen sensor reading',
      path: 'helmcentral.anomaly.sensor.frozenCount',
      message: 'Frozen sensor reading: 1, clears below 0.5',
      state: 'alert',
      live_evidence: 'propulsion.port.exhaustTemperature',
      ...overrides,
    })
  }

  it('renders one ignore action per offending identifier named in live_evidence', () => {
    vi.stubGlobal('fetch', vi.fn(async () => ({ ok: true, json: async () => ({ identifiers: [] }) })))

    renderDrawer([frozenAlarm({ live_evidence: 'propulsion.port.exhaustTemperature, propulsion.stbd.exhaustTemperature' })], null)

    expect(screen.getByRole('button', { name: 'Ignore propulsion.port.exhaustTemperature' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Ignore propulsion.stbd.exhaustTemperature' })).toBeInTheDocument()
  })

  it('follows live_evidence rather than the frozen evidence when they differ', () => {
    // A long-running count alarm: port's exhaust sensor (named in the
    // frozen evidence, from whenever this first raised) has since
    // recovered, and starboard's is the one actually failing now.
    vi.stubGlobal('fetch', vi.fn(async () => ({ ok: true, json: async () => ({ identifiers: [] }) })))

    renderDrawer([frozenAlarm({
      evidence: 'propulsion.port.exhaustTemperature',
      live_evidence: 'propulsion.stbd.exhaustTemperature',
    })], null)

    expect(screen.getByRole('button', { name: 'Ignore propulsion.stbd.exhaustTemperature' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Ignore propulsion.port.exhaustTemperature' })).toBeNull()
  })

  it('POSTs the identifier when clicked', async () => {
    const fetchMock = vi.fn(async (url: string, init?: RequestInit) => {
      if (String(url).endsWith('/api/alarms/ignored-sensors') && init?.method === 'POST') {
        return { ok: true, json: async () => ({ identifiers: [JSON.parse(String(init.body)).identifier] }) }
      }
      return { ok: true, json: async () => ({ identifiers: [] }) }
    })
    vi.stubGlobal('fetch', fetchMock)

    renderDrawer([frozenAlarm()], null)

    fireEvent.click(await screen.findByRole('button', { name: 'Ignore propulsion.port.exhaustTemperature' }))

    await waitFor(() => expect(fetchMock).toHaveBeenCalledWith('/api/alarms/ignored-sensors', expect.objectContaining({
      method: 'POST',
      body: JSON.stringify({ identifier: 'propulsion.port.exhaustTemperature' }),
    })))
  })

  it('renders no ignore action for a non-sensor-health alarm even with evidence-shaped text', () => {
    vi.stubGlobal('fetch', vi.fn(async () => ({ ok: true, json: async () => ({ identifiers: [] }) })))

    renderDrawer([makeAlarm({
      rule_id: 'helmcentral:anomaly-full-bank-charging-warn',
      label: 'Charging into a full house bank',
      path: 'helmcentral.anomaly.battery.fullBankCharging',
      live_evidence: 'House bank 96% SoC, 28.90 V, charging 42 A',
    })], null)

    expect(screen.queryByRole('button', { name: /^Ignore /i })).toBeNull()
  })
})

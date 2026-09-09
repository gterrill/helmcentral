import { describe, expect, it } from 'vitest'

import { collisionAlarmStatesByVessel } from '@/hooks/use-alarms'
import type { ActiveAlarm } from '@/hooks/use-alarms'

function makeAlarm(overrides: Partial<ActiveAlarm> = {}): ActiveAlarm {
  return {
    rule_id: 'notifications:navigation.closestApproach@urn:mrn:imo:mmsi:512213970',
    label: 'AIS collision',
    path: 'notifications.navigation.closestApproach',
    phase: 'active',
    state: 'warn',
    value: 0,
    message: 'WINGS 12 - CPA WARNING',
    silenced: false,
    can_silence: false,
    can_acknowledge: true,
    ...overrides,
  }
}

describe('collisionAlarmStatesByVessel', () => {
  it('returns an empty map for no alarms', () => {
    expect(collisionAlarmStatesByVessel([])).toEqual(new Map())
  })

  it('ignores alarms that are not AIS collision notifications', () => {
    const alarms = [
      makeAlarm({ rule_id: 'helmcentral:anchor-drag' }),
      makeAlarm({ rule_id: 'rule:battery-low' }),
      // Self-tree notification (no vessel id after '@') is a sensor-fault
      // alarm (ADR 0057 §3), not a collision alarm for a nearby target.
      makeAlarm({ rule_id: 'notifications:navigation.closestApproach' }),
    ]

    expect(collisionAlarmStatesByVessel(alarms)).toEqual(new Map())
  })

  it('keys a collision alarm by the vessel id after the @', () => {
    const alarms = [
      makeAlarm({
        rule_id: 'notifications:navigation.closestApproach@urn:mrn:imo:mmsi:512213970',
        state: 'warn',
      }),
    ]

    const result = collisionAlarmStatesByVessel(alarms)
    expect(result.size).toBe(1)
    expect(result.get('urn:mrn:imo:mmsi:512213970')).toBe('warn')
  })

  it('includes an acknowledged collision alarm — acknowledging stops the alert, not the fact', () => {
    const alarms = [
      makeAlarm({
        rule_id: 'notifications:navigation.closestApproach@urn:mrn:imo:mmsi:512213970',
        state: 'alarm',
        phase: 'acknowledged',
      }),
    ]

    expect(collisionAlarmStatesByVessel(alarms).get('urn:mrn:imo:mmsi:512213970')).toBe('alarm')
  })

  it('returns one entry per distinct vessel', () => {
    const alarms = [
      makeAlarm({ rule_id: 'notifications:navigation.closestApproach@urn:mrn:imo:mmsi:100000001', state: 'warn' }),
      makeAlarm({
        rule_id: 'notifications:navigation.closestApproach@urn:mrn:imo:mmsi:100000002',
        state: 'alarm',
        phase: 'acknowledged',
      }),
    ]

    const result = collisionAlarmStatesByVessel(alarms)
    expect(result.size).toBe(2)
    expect(result.get('urn:mrn:imo:mmsi:100000001')).toBe('warn')
    expect(result.get('urn:mrn:imo:mmsi:100000002')).toBe('alarm')
  })

  it('keeps the worse state when the same vessel appears twice, warn then alarm', () => {
    const alarms = [
      makeAlarm({ rule_id: 'notifications:navigation.closestApproach@urn:mrn:imo:mmsi:100000001', state: 'warn' }),
      makeAlarm({ rule_id: 'notifications:navigation.closestApproach@urn:mrn:imo:mmsi:100000001', state: 'alarm' }),
    ]

    expect(collisionAlarmStatesByVessel(alarms).get('urn:mrn:imo:mmsi:100000001')).toBe('alarm')
  })

  it('keeps the worse state when the same vessel appears twice, alarm then warn', () => {
    const alarms = [
      makeAlarm({ rule_id: 'notifications:navigation.closestApproach@urn:mrn:imo:mmsi:100000001', state: 'alarm' }),
      makeAlarm({ rule_id: 'notifications:navigation.closestApproach@urn:mrn:imo:mmsi:100000001', state: 'warn' }),
    ]

    expect(collisionAlarmStatesByVessel(alarms).get('urn:mrn:imo:mmsi:100000001')).toBe('alarm')
  })
})

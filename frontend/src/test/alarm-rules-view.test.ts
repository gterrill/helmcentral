import { describe, expect, it } from 'vitest'

import { groupRulesByDomain, ruleDomain } from '@/lib/alarm-rules-view'
import type { AlarmRule } from '@/hooks/use-alarm-rules'

/**
 * The Rules tile groups by the domain already encoded in the SignalK path
 * (PFD fix 3), so there is no second taxonomy to invent or keep in sync.
 */
function rule(overrides: Partial<AlarmRule> = {}): AlarmRule {
  return {
    id: 'rule-1',
    enabled: true,
    path: 'electrical.batteries.house.voltage',
    label: 'House bank low',
    op: 'below',
    value: 11.8,
    hysteresis: 0,
    dwell_seconds: 10,
    stale_after_seconds: 0,
    state: 'alarm',
    methods: ['visual', 'sound'],
    notify: null,
    escalate_after_seconds: 0,
    ...overrides,
  }
}

describe('ruleDomain', () => {
  it('takes the first path segment as the domain', () => {
    expect(ruleDomain('electrical.batteries.house.voltage')).toBe('electrical')
  })

  it('strips a leading helmcentral namespace before taking the domain', () => {
    expect(ruleDomain('helmcentral.environment.pressureRate')).toBe('environment')
  })

  it('takes the whole path as the domain when it has only one segment', () => {
    expect(ruleDomain('depth')).toBe('depth')
  })

  it('does not strip helmcentral when it is not the leading segment', () => {
    // Only a genuine namespace prefix is stripped; a domain that merely
    // contains the word "helmcentral" further down the path is left alone.
    expect(ruleDomain('radar.fur6424A.guardZone.1')).toBe('radar')
  })
})

describe('groupRulesByDomain', () => {
  it('sorts domains alphabetically: electrical before environment before radar', () => {
    const rules = [
      rule({ id: 'r1', path: 'radar.fur6424A.guardZone.1', label: 'Guard zone 1' }),
      rule({ id: 'r2', path: 'helmcentral.environment.pressureRate', label: 'Barometer falling' }),
      rule({ id: 'r3', path: 'electrical.batteries.house.voltage', label: 'House bank low' }),
    ]

    const groups = groupRulesByDomain(rules)

    expect(groups.map((g) => g.domain)).toEqual(['electrical', 'environment', 'radar'])
  })

  it('sorts rules within a domain by label, case-insensitively', () => {
    const rules = [
      rule({ id: 'r1', path: 'environment.pressureRate', label: 'plummeting' }),
      rule({ id: 'r2', path: 'environment.pressureRate', label: 'Barometer falling' }),
      rule({ id: 'r3', path: 'environment.pressureRate', label: 'ANOMALY' }),
    ]

    const groups = groupRulesByDomain(rules)

    expect(groups).toHaveLength(1)
    expect(groups[0].rules.map((r) => r.label)).toEqual(['ANOMALY', 'Barometer falling', 'plummeting'])
  })

  it('groups a one-segment path under that segment', () => {
    const rules = [rule({ id: 'r1', path: 'depth', label: 'Shallow water' })]

    const groups = groupRulesByDomain(rules)

    expect(groups).toEqual([{ domain: 'depth', rules: [rules[0]] }])
  })

  it('returns no groups for an empty rule list', () => {
    expect(groupRulesByDomain([])).toEqual([])
  })
})

import { describe, expect, test } from 'vitest'

import {
  alarmZoneCount,
  applyProfileToGauges,
  commonInstancePrefix,
  instancePrefixCandidates,
  mergeGaugeSettingsBySuffix,
  profileToGauges,
  type EngineProfile,
} from '@/lib/engine-profiles'

const profile: EngineProfile = {
  id: 'cummins-qsb67-550',
  name: 'Cummins QSB 6.7 550',
  gauges: [
    {
      path_suffix: 'oilPressure', label: 'Oil Press', display: 'radial',
      quantity: 'pressure', unit: 'psi', min: 0, max: 100,
      zones: [
        { direction: 'above', threshold: 40, state: 'normal', source: 'sbmar.com' },
        { direction: 'below', threshold: null, state: 'alarm', note: 'from your manual' },
      ],
    },
    {
      path_suffix: 'coolantTemperature', label: 'Coolant', display: 'radial',
      quantity: 'temperature', unit: 'C', min: 0, max: 120,
      zones: [{ direction: 'below', threshold: 85, state: 'normal' }],
    },
  ],
  service: [],
}

describe('profileToGauges', () => {
  test('composes the full path from the instance prefix and the suffix', () => {
    const gauges = profileToGauges(profile, 'propulsion.port')
    expect(gauges.map((g) => g.path)).toEqual([
      'propulsion.port.oilPressure',
      'propulsion.port.coolantTemperature',
    ])
  })

  test('tolerates a prefix given with a trailing dot', () => {
    expect(profileToGauges(profile, 'propulsion.port.')[0].path).toBe('propulsion.port.oilPressure')
  })

  test('carries the display, quantity, unit and scale through', () => {
    const [oil] = profileToGauges(profile, 'propulsion.port')
    expect(oil).toMatchObject({
      label: 'Oil Press', display: 'radial', quantity: 'pressure', unit: 'psi', min: 0, max: 100,
    })
  })

  /**
   * Zones are stored as {from, to} but authored as direction + threshold, the
   * same model the zone editor uses. Anchoring is what stops a profile
   * describing a mid-range band the backend would reject.
   */
  test('anchors a below-band to the bottom of the scale', () => {
    const [, coolant] = profileToGauges(profile, 'propulsion.port')
    expect(coolant.zones).toEqual([{ from: 0, to: 85, state: 'normal' }])
  })

  test('anchors an above-band to the top of the scale', () => {
    const [oil] = profileToGauges(profile, 'propulsion.port')
    expect(oil.zones).toEqual([{ from: 40, to: 100, state: 'normal' }])
  })

  // A null threshold is a slot. Saving it would either fail validation or,
  // worse, become a zone at some default number nobody chose.
  test('drops zones whose threshold is unset', () => {
    const [oil] = profileToGauges(profile, 'propulsion.port')
    expect(oil.zones).toHaveLength(1)
    expect(oil.zones!.every((z) => z.state === 'normal')).toBe(true)
  })

  test('omits the zones key entirely when every zone is a slot', () => {
    const slotsOnly: EngineProfile = {
      ...profile,
      gauges: [{
        path_suffix: 'exhaustTemperature', label: 'Exhaust', display: 'bar',
        quantity: 'temperature', unit: 'C', min: 0, max: 600,
        zones: [{ direction: 'above', threshold: null, state: 'warn' }],
      }],
    }
    expect(profileToGauges(slotsOnly, 'propulsion.port')[0].zones).toBeUndefined()
  })
})

describe('alarmZoneCount', () => {
  // The dialog's safety gate: you confirm a count of thresholds that will
  // become alarms before any of them do.
  test('counts only zones that will raise an alarm', () => {
    expect(alarmZoneCount(profile)).toBe(0)
  })

  test('counts a filled non-normal threshold', () => {
    const armed: EngineProfile = {
      ...profile,
      gauges: [{
        ...profile.gauges[0],
        zones: [
          { direction: 'below', threshold: 15, state: 'alarm' },
          { direction: 'below', threshold: 25, state: 'warn' },
          { direction: 'above', threshold: 40, state: 'normal' },
          { direction: 'below', threshold: null, state: 'emergency' },
        ],
      }],
    }
    expect(alarmZoneCount(armed)).toBe(2)
  })
})

describe('applyProfileToGauges', () => {
  const existing = [
    { path: 'propulsion.stbd.oilPressure', label: 'My oil label', display: 'numeric' as const, quantity: 'raw', unit: 'raw' },
    { path: 'propulsion.stbd.rudderAngle', label: 'Rudder', display: 'numeric' as const, quantity: 'raw', unit: 'raw' },
  ]

  test('fills scale, units and zones on gauges whose path matches a suffix', () => {
    const next = applyProfileToGauges(existing, profile)
    expect(next[0]).toMatchObject({ quantity: 'pressure', unit: 'psi', min: 0, max: 100, display: 'radial' })
    expect(next[0].zones).toEqual([{ from: 40, to: 100, state: 'normal' }])
  })

  // Path and label are the operator's own choices; a profile has no business
  // overwriting either.
  test('leaves the path and the label alone', () => {
    const next = applyProfileToGauges(existing, profile)
    expect(next[0].path).toBe('propulsion.stbd.oilPressure')
    expect(next[0].label).toBe('My oil label')
  })

  test('leaves gauges the profile knows nothing about untouched', () => {
    expect(applyProfileToGauges(existing, profile)[1]).toEqual(existing[1])
  })

  test('does not mutate the input', () => {
    applyProfileToGauges(existing, profile)
    expect(existing[0].quantity).toBe('raw')
  })
})

describe('instancePrefixCandidates', () => {
  test('derives prefixes from published paths matching the profile suffixes', () => {
    const candidates = instancePrefixCandidates(profile, [
      { path: 'propulsion.port.oilPressure' },
      { path: 'propulsion.starboard.coolantTemperature' },
      { path: 'environment.depth.belowTransducer' },
    ])
    expect(candidates).toEqual(['propulsion.port', 'propulsion.starboard'])
  })

  test('is empty when nothing is published, so the field falls back to free text', () => {
    expect(instancePrefixCandidates(profile, [])).toEqual([])
  })
})

describe('mergeGaugeSettingsBySuffix', () => {
  const existing = [
    { path: 'propulsion.stbd.oilPressure', label: 'My label', display: 'numeric' as const, quantity: 'raw', unit: 'raw' },
    { path: 'propulsion.stbd.rudderAngle', label: 'Rudder', display: 'numeric' as const, quantity: 'raw', unit: 'raw' },
  ]
  const incoming = () => profileToGauges(profile, 'propulsion.stbd')

  test('copies settings onto matching gauges, keeping path and label', () => {
    const { gauges } = mergeGaugeSettingsBySuffix(existing, incoming())

    expect(gauges[0]).toMatchObject({ quantity: 'pressure', unit: 'psi', min: 0, max: 100, display: 'radial' })
    expect(gauges[0].path).toBe('propulsion.stbd.oilPressure')
    expect(gauges[0].label).toBe('My label')
  })

  test('leaves unmatched gauges alone and does not mutate', () => {
    const { gauges } = mergeGaugeSettingsBySuffix(existing, incoming())
    expect(gauges[1]).toEqual(existing[1])
    expect(existing[0].quantity).toBe('raw')
  })

  /**
   * Applying a profile to a half-built tile should finish it. Only updating
   * what is already there means the gauges you did not think to add stay
   * missing, which is the opposite of what a profile is for.
   */
  test('appends gauges the profile has and the tile does not', () => {
    const { gauges } = mergeGaugeSettingsBySuffix(existing, incoming())

    expect(gauges.map((g) => g.path)).toEqual([
      'propulsion.stbd.oilPressure',
      'propulsion.stbd.rudderAngle',
      'propulsion.stbd.coolantTemperature',
    ])
    expect(gauges[2]).toMatchObject({ label: 'Coolant', quantity: 'temperature', unit: 'C' })
  })

  test('reports how many were updated and how many added', () => {
    const { updated, added } = mergeGaugeSettingsBySuffix(existing, incoming())
    expect(updated).toBe(1)
    expect(added).toBe(1)
  })

  test('appends everything when the tile is empty', () => {
    const { gauges, updated, added } = mergeGaugeSettingsBySuffix([], incoming())
    expect(gauges).toHaveLength(2)
    expect(updated).toBe(0)
    expect(added).toBe(2)
  })

  test('adds nothing when the tile already covers the profile', () => {
    const full = incoming()
    const { gauges, updated, added } = mergeGaugeSettingsBySuffix(full, full)
    expect(gauges).toHaveLength(2)
    expect(updated).toBe(2)
    expect(added).toBe(0)
  })
})

describe('commonInstancePrefix', () => {
  /**
   * When applying to a tile that already exists, the prefix must come from
   * that tile — not from whatever the server happens to publish first, which
   * would append starboard gauges to the Port tile.
   */
  test('derives the prefix shared by a tile\'s gauges', () => {
    expect(commonInstancePrefix([
      { path: 'propulsion.port.oilPressure', label: '', display: 'numeric', quantity: 'raw', unit: 'raw' },
      { path: 'propulsion.port.coolantTemperature', label: '', display: 'numeric', quantity: 'raw', unit: 'raw' },
    ])).toBe('propulsion.port')
  })

  test('is null when the gauges do not agree', () => {
    expect(commonInstancePrefix([
      { path: 'propulsion.port.oilPressure', label: '', display: 'numeric', quantity: 'raw', unit: 'raw' },
      { path: 'tanks.fuel.0.currentLevel', label: '', display: 'numeric', quantity: 'raw', unit: 'raw' },
    ])).toBeNull()
  })

  test('is null for an empty or blank-path tile', () => {
    expect(commonInstancePrefix([])).toBeNull()
    expect(commonInstancePrefix([
      { path: '', label: '', display: 'numeric', quantity: 'raw', unit: 'raw' },
    ])).toBeNull()
  })
})

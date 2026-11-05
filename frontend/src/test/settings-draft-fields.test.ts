import { describe, expect, it } from 'vitest'

import {
  buildRegularSettingsPatch,
  draftsEqual,
  hydrateDraftFromSettings,
  initialRegularSettingsDraft,
  type RegularSettingsDraft,
} from '@/components/settings/settings-draft'
import { emptyVesselRoles } from '@/lib/vessel-settings'
import type { SettingsPayload } from '@/hooks/use-settings-form'

// Every draft field has a value here that differs from its initial value and
// survives a save and a reload unchanged. Typed as a full draft, so a field
// added to RegularSettingsDraft without a value here fails tsc.
const changed: RegularSettingsDraft = {
  signalkAddress: 'signalk.example',
  signalkPort: '4000',
  vesselPrefix: 'S/Y',
  boatModel: 'Test 40',
  distanceUnits: 'imperial',
  tideStationId: 'st-1',
  tideStationName: 'Test Point',
  tideAutoStation: true,
  bowRollerHeightM: '2.5',
  gpsFromBowM: '4',
  loaM: '12.5',
  transducerDepthM: '0.6',
  chainSizeMm: '10',
  chainOnboardM: '80',
  hullType: 'sail_mono',
  scopeMethod: 'catenary',
  windageAreaM2: '20',
  autoRaiseOnMotoring: false,
  minClearanceAtLowM: '1.2',
  influxdbEnabled: true,
  influxdbUrl: 'http://influx.example',
  influxdbOrg: 'org',
  influxdbBucket: 'bucket',
  mayaraAddress: 'radar.example',
  mayaraPort: '7000',
  authMode: 'signalk',
  assistantEnabled: true,
  assistantModel: 'test/model',
  assistantDocumentModel: 'test/doc',
  assistantNotes: 'standing note',
  assistantAllowedModels: ['a/*'],
  assistantExcludedModels: ['b/*'],
  assistantCostTier: 'high',
  assistantVoiceInput: true,
  assistantReadAloud: true,
  assistantWakeWord: true,
  assistantVerboseRunLog: true,
  assistantWebSearch: true,
  assistantWebSearchModel: 'test/search',
  assistantPassageLimits: {
    ahead: { wind: '20', wave: '2' },
    abeam: { wind: '25', wave: '2.5' },
    astern: { wind: '30', wave: '3' },
    tideOffset: '5',
    daylightArrival: true,
  },
  offlineCoastlineEnabled: true,
  roles: { ...emptyVesselRoles, house_bank: 'bank-1' },
}

const keys = Object.keys(changed) as Array<keyof RegularSettingsDraft>

describe('settings draft fields', () => {
  it('declares a value for every draft field that differs from the initial one', () => {
    expect(Object.keys(initialRegularSettingsDraft).sort()).toEqual([...keys].sort())
    for (const key of keys) {
      expect(changed[key], key).not.toEqual(initialRegularSettingsDraft[key])
    }
  })

  it.each(keys)('%s: a change is seen by draftsEqual and the patch, and survives save then reload', (key) => {
    const base = initialRegularSettingsDraft
    const edited = { ...base, [key]: changed[key] } as RegularSettingsDraft

    expect(draftsEqual(base, edited)).toBe(false)
    expect(draftsEqual(edited, { ...edited })).toBe(true)

    expect(buildRegularSettingsPatch(edited)).not.toEqual(buildRegularSettingsPatch(base))

    const saved = buildRegularSettingsPatch(edited) as SettingsPayload
    expect(hydrateDraftFromSettings(saved)[key]).toEqual(changed[key])
  })

  it('round-trips every field at once', () => {
    const saved = buildRegularSettingsPatch(changed) as SettingsPayload
    expect(hydrateDraftFromSettings(saved)).toEqual(changed)
  })

  it('hydrates an empty payload to the initial draft', () => {
    expect(hydrateDraftFromSettings({})).toEqual(initialRegularSettingsDraft)
  })

  // The wire format as the hand-written patch sent it. Every text value has
  // spaces round it, and every string is different, so a swapped path, a
  // dropped trim or a changed fallback shows here. The same draft was run
  // through the code this table replaced to get these figures.
  const typed: RegularSettingsDraft = {
    ...initialRegularSettingsDraft,
    signalkAddress: ' sk.local ',
    signalkPort: ' 4000 ',
    vesselPrefix: '  S/Y ',
    boatModel: ' Model ',
    distanceUnits: 'imperial',
    tideStationId: ' id ',
    tideStationName: ' name ',
    tideAutoStation: true,
    bowRollerHeightM: 'abc',
    gpsFromBowM: '-3',
    loaM: ' 12 ',
    transducerDepthM: 'abc',
    chainSizeMm: '0',
    chainOnboardM: 'x',
    hullType: 'sail_cat',
    scopeMethod: 'catenary',
    windageAreaM2: ' 20',
    autoRaiseOnMotoring: false,
    minClearanceAtLowM: '-1',
    influxdbEnabled: true,
    influxdbUrl: ' http://influx ',
    influxdbOrg: ' org ',
    influxdbBucket: ' bucket ',
    mayaraAddress: ' radar ',
    mayaraPort: 'zz',
    authMode: 'signalk',
    assistantEnabled: true,
    assistantModel: ' m ',
    assistantDocumentModel: ' d ',
    assistantNotes: ' notes ',
    assistantAllowedModels: [' a ', ''],
    assistantExcludedModels: ['', ' b '],
    assistantCostTier: ' HIGH ' as RegularSettingsDraft['assistantCostTier'],
    assistantVoiceInput: true,
    assistantReadAloud: false,
    assistantWakeWord: true,
    assistantVerboseRunLog: false,
    assistantWebSearch: true,
    assistantWebSearchModel: ' s ',
    assistantPassageLimits: {
      ahead: { wind: ' 12,5 ', wave: 'x' },
      abeam: { wind: '', wave: '2' },
      astern: { wind: '30', wave: '' },
      tideOffset: '5',
      daylightArrival: true,
    },
    offlineCoastlineEnabled: false,
  }

  const typedPatch = {
    signalk: { address: 'sk.local', port: 4000 },
    boat: { vessel_prefix: 'S/Y', model: 'Model' },
    units: 'imperial',
    ui: { tide_station_id: ' id ', tide_station_name: ' name ', tide_auto_station: true },
    anchor: {
      bow_roller_height_m: 1.5,
      gps_from_bow_m: 0,
      loa_m: 12,
      transducer_depth_m: -1,
      chain_size_mm: 12,
      chain_onboard_m: 150,
      hull_type: 'sail_cat',
      scope_method: 'catenary',
      windage_area_m2: 20,
      auto_raise_on_motoring: false,
      min_clearance_at_low_m: 0.5,
    },
    influxdb: { enabled: true, url: 'http://influx', org: 'org', bucket: 'bucket' },
    mayara: { address: 'radar', port: 6502 },
    auth: { mode: 'signalk' },
    assistant: {
      enabled: true,
      model: 'm',
      document_model: 'd',
      notes: ' notes ',
      allowed_models: ['a'],
      excluded_models: ['b'],
      cost_tier: 'high',
      voice_input: true,
      read_aloud: false,
      wake_word: true,
      verbose_run_log: false,
      web_search: true,
      web_search_model: 's',
      passage_limits: {
        ahead: { wind_kn_no_go: 12.5, wave_m_no_go: null },
        abeam: { wind_kn_no_go: null, wave_m_no_go: 2 },
        astern: { wind_kn_no_go: 30, wave_m_no_go: null },
        wind_against_tide_offset_kn: 5,
        daylight_arrival: true,
      },
    },
    offline_coastline: { enabled: false },
  }

  it('saves exactly what the hand-written patch saved', () => {
    expect(buildRegularSettingsPatch(typed)).toEqual(typedPatch)
  })

  it('sends roles only once they have been read', () => {
    const roles = { ...emptyVesselRoles, house_bank: 'bank-1' }
    expect(buildRegularSettingsPatch({ ...typed, roles })).toEqual({ ...typedPatch, roles })
  })

  it('saves each starting default when its box holds no usable number', () => {
    const blank = Object.fromEntries(
      ['signalkPort', 'bowRollerHeightM', 'gpsFromBowM', 'loaM', 'chainSizeMm', 'chainOnboardM', 'windageAreaM2', 'minClearanceAtLowM', 'mayaraPort']
        .map((key) => [key, 'abc']),
    )
    const patch = buildRegularSettingsPatch({ ...initialRegularSettingsDraft, ...blank })
    expect(patch.signalk?.port).toBe(3000)
    expect(patch.anchor).toMatchObject({
      bow_roller_height_m: 1.5,
      gps_from_bow_m: 0,
      loa_m: 0,
      chain_size_mm: 12,
      chain_onboard_m: 150,
      windage_area_m2: 35,
      min_clearance_at_low_m: 0.5,
    })
    expect(patch.mayara?.port).toBe(6502)
  })

  it('reads the server answer as it always did', () => {
    const answer: SettingsPayload = {
      signalk: { address: '', port: 0 },
      anchor: { transducer_depth_m: 0, hull_type: 'junk', scope_method: 'ratio', gps_from_bow_m: 0 },
      mayara: { address: '' },
      assistant: { model: '', cost_tier: 'bogus', allowed_models: [' a ', ''] },
      ui: { tide_station_id: ' id ' },
      units: 'furlongs',
      auth: { mode: 'other' },
    }
    const draft = hydrateDraftFromSettings(answer)
    expect(draft.signalkAddress).toBe('localhost')
    expect(draft.signalkPort).toBe('0')
    expect(draft.transducerDepthM).toBe('')
    expect(draft.hullType).toBe('power_cat')
    expect(draft.gpsFromBowM).toBe('0')
    expect(draft.mayaraAddress).toBe('')
    expect(draft.assistantModel).toBe('')
    expect(draft.assistantCostTier).toBe('')
    expect(draft.assistantAllowedModels).toEqual(['a'])
    expect(draft.tideStationId).toBe(' id ')
    expect(draft.distanceUnits).toBe('metric')
    expect(draft.authMode).toBe('none')
  })
})

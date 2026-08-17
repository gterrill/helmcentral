import type { DeepPartial, SettingsPayload } from '@/hooks/use-settings-form'

export const defaultTankLabelIds = [
  'blackWater.1',
  'blackWater.6',
  'freshWater.0',
  'freshWater.3',
  'fuel.2',
  'fuel.4',
  'fuel.5',
  'fuel.7',
]

export type HullType = 'power_cat' | 'sail_mono' | 'power_mono' | 'sail_cat'

/**
 * Local-form mirror of every field owned by the "regular" settings
 * sections (SignalK Connection, Boat & UI, Labels, Anchor, InfluxDB) plus
 * the Tide station fields that live visually in the Widgets > Tide tab but
 * are still plain form fields saved through the same pinned "Save
 * Settings" button, not provider-card auto-save. Numbers are kept as
 * strings while editing (matching the old panel's UX of allowing partial
 * numeric input), and only coerced to numbers when building the save
 * patch.
 */
export interface RegularSettingsDraft {
  signalkAddress: string
  signalkPort: string
  vesselStateRefreshSeconds: string
  vesselPrefix: string
  boatModel: string
  houseBatteryCapacityAh: string
  distanceUnits: 'metric' | 'imperial'
  tankLabels: Record<string, string>
  tideStationId: string
  tideStationName: string
  tideAutoStation: boolean
  bowRollerHeightM: string
  gpsFromBowM: string
  loaM: string
  chainSizeMm: string
  chainOnboardM: string
  hullType: HullType
  windageAreaM2: string
  influxdbEnabled: boolean
  influxdbUrl: string
  influxdbOrg: string
  influxdbBucket: string
  authMode: 'none' | 'signalk'
}

export const initialRegularSettingsDraft: RegularSettingsDraft = {
  signalkAddress: 'localhost',
  signalkPort: '3000',
  vesselStateRefreshSeconds: '10',
  vesselPrefix: '',
  boatModel: '',
  houseBatteryCapacityAh: '1440',
  distanceUnits: 'metric',
  tankLabels: Object.fromEntries(defaultTankLabelIds.map((id) => [id, ''])),
  tideStationId: '',
  tideStationName: '',
  tideAutoStation: false,
  bowRollerHeightM: '1.5',
  gpsFromBowM: '0',
  loaM: '0',
  chainSizeMm: '12',
  chainOnboardM: '150',
  hullType: 'power_cat',
  windageAreaM2: '35',
  influxdbEnabled: false,
  authMode: 'none',
  influxdbUrl: '',
  influxdbOrg: '',
  influxdbBucket: '',
}

/** Builds the draft's starting values from a freshly-fetched settings payload. */
export function hydrateDraftFromSettings(settings: SettingsPayload): RegularSettingsDraft {
  const draft: RegularSettingsDraft = { ...initialRegularSettingsDraft, tankLabels: { ...initialRegularSettingsDraft.tankLabels } }

  if (settings.signalk?.address) draft.signalkAddress = settings.signalk.address
  if (typeof settings.signalk?.port === 'number') draft.signalkPort = String(settings.signalk.port)

  if (typeof settings.boat?.vessel_prefix === 'string') draft.vesselPrefix = settings.boat.vessel_prefix
  if (typeof settings.boat?.model === 'string') draft.boatModel = settings.boat.model
  if (typeof settings.boat?.house_battery_capacity_ah === 'number') {
    draft.houseBatteryCapacityAh = String(settings.boat.house_battery_capacity_ah)
  }

  if (settings.units === 'metric' || settings.units === 'imperial') draft.distanceUnits = settings.units
  if (typeof settings.ui?.vessel_state_refresh_seconds === 'number') {
    draft.vesselStateRefreshSeconds = String(settings.ui.vessel_state_refresh_seconds)
  }
  if (settings.ui?.tank_labels && typeof settings.ui.tank_labels === 'object') {
    draft.tankLabels = { ...draft.tankLabels, ...settings.ui.tank_labels }
  }
  if (typeof settings.ui?.tide_station_id === 'string') draft.tideStationId = settings.ui.tide_station_id
  if (typeof settings.ui?.tide_station_name === 'string') draft.tideStationName = settings.ui.tide_station_name
  if (typeof settings.ui?.tide_auto_station === 'boolean') draft.tideAutoStation = settings.ui.tide_auto_station

  if (typeof settings.anchor?.bow_roller_height_m === 'number') draft.bowRollerHeightM = String(settings.anchor.bow_roller_height_m)
  // gps_from_bow_m: 0 is a meaningful explicit value ("no correction"), not
  // an absent one, so it must hydrate the same as any other number here.
  if (typeof settings.anchor?.gps_from_bow_m === 'number') draft.gpsFromBowM = String(settings.anchor.gps_from_bow_m)
  if (typeof settings.anchor?.loa_m === 'number') draft.loaM = String(settings.anchor.loa_m)
  if (typeof settings.anchor?.chain_size_mm === 'number') draft.chainSizeMm = String(settings.anchor.chain_size_mm)
  if (typeof settings.anchor?.chain_onboard_m === 'number') draft.chainOnboardM = String(settings.anchor.chain_onboard_m)
  if (
    settings.anchor?.hull_type === 'power_cat'
    || settings.anchor?.hull_type === 'sail_mono'
    || settings.anchor?.hull_type === 'power_mono'
    || settings.anchor?.hull_type === 'sail_cat'
  ) {
    draft.hullType = settings.anchor.hull_type
  }
  if (typeof settings.anchor?.windage_area_m2 === 'number') draft.windageAreaM2 = String(settings.anchor.windage_area_m2)

  if (typeof settings.influxdb?.enabled === 'boolean') draft.influxdbEnabled = settings.influxdb.enabled
  if (settings.auth?.mode === 'signalk' || settings.auth?.mode === 'none') draft.authMode = settings.auth.mode
  if (typeof settings.influxdb?.url === 'string') draft.influxdbUrl = settings.influxdb.url
  if (typeof settings.influxdb?.org === 'string') draft.influxdbOrg = settings.influxdb.org
  if (typeof settings.influxdb?.bucket === 'string') draft.influxdbBucket = settings.influxdb.bucket

  return draft
}

const parseNumber = (value: string, fallback: number) => {
  const parsed = Number.parseFloat(value)
  return Number.isFinite(parsed) && parsed > 0 ? parsed : fallback
}

// gps_from_bow_m defaults to 0, meaning "no correction" - unlike every other
// numeric anchor field, 0 is the meaningful explicit value here, not an
// absent one, so parseNumber's `parsed > 0` guard would wrongly discard it.
const parseNonNegativeNumber = (value: string, fallback: number) => {
  const parsed = Number.parseFloat(value)
  return Number.isFinite(parsed) && parsed >= 0 ? parsed : fallback
}

/**
 * Deep-equality check for two RegularSettingsDraft values, used to derive
 * whether the draft actually differs from what's saved (rather than
 * tracking "was any field ever touched," which incorrectly stays dirty if a
 * field is edited and then changed back to its original value).
 */
export function draftsEqual(a: RegularSettingsDraft, b: RegularSettingsDraft): boolean {
  // Compare every top-level field; tankLabels is the one nested
  // Record<string,string> field and needs its own key-by-key comparison.
  if (a.signalkAddress !== b.signalkAddress) return false
  if (a.signalkPort !== b.signalkPort) return false
  if (a.vesselStateRefreshSeconds !== b.vesselStateRefreshSeconds) return false
  if (a.vesselPrefix !== b.vesselPrefix) return false
  if (a.boatModel !== b.boatModel) return false
  if (a.houseBatteryCapacityAh !== b.houseBatteryCapacityAh) return false
  if (a.distanceUnits !== b.distanceUnits) return false
  if (a.tideStationId !== b.tideStationId) return false
  if (a.tideStationName !== b.tideStationName) return false
  if (a.tideAutoStation !== b.tideAutoStation) return false
  if (a.bowRollerHeightM !== b.bowRollerHeightM) return false
  if (a.gpsFromBowM !== b.gpsFromBowM) return false
  if (a.loaM !== b.loaM) return false
  if (a.chainSizeMm !== b.chainSizeMm) return false
  if (a.chainOnboardM !== b.chainOnboardM) return false
  if (a.hullType !== b.hullType) return false
  if (a.windageAreaM2 !== b.windageAreaM2) return false
  if (a.influxdbEnabled !== b.influxdbEnabled) return false
  if (a.authMode !== b.authMode) return false
  if (a.influxdbUrl !== b.influxdbUrl) return false
  if (a.influxdbOrg !== b.influxdbOrg) return false
  if (a.influxdbBucket !== b.influxdbBucket) return false

  // Compare tankLabels: same keys and same value for each key
  const aLabelsKeys = Object.keys(a.tankLabels).sort()
  const bLabelsKeys = Object.keys(b.tankLabels).sort()
  if (aLabelsKeys.length !== bLabelsKeys.length) return false
  for (let i = 0; i < aLabelsKeys.length; i++) {
    if (aLabelsKeys[i] !== bLabelsKeys[i]) return false
  }
  for (const key of aLabelsKeys) {
    if (a.tankLabels[key] !== b.tankLabels[key]) return false
  }

  return true
}

/**
 * Builds the settings patch for the pinned "Save Settings" button. Note
 * this deliberately does NOT include `ui.tide_provider` /
 * `ui.weather_provider` / `ui.wave_provider` / `ui.forecast_warnings_provider`
 * — those are owned exclusively by the Widgets provider cards' own
 * immediate `save()` calls, and omitting them here means the settings
 * form's one-level-per-subobject merge (see `deepMergeSettings`) leaves
 * whatever is already persisted for them untouched.
 */
export function buildRegularSettingsPatch(draft: RegularSettingsDraft): DeepPartial<SettingsPayload> {
  return {
    signalk: {
      address: draft.signalkAddress.trim(),
      port: Number.parseInt(draft.signalkPort, 10) || 3000,
    },
    boat: {
      vessel_prefix: draft.vesselPrefix.trim(),
      model: draft.boatModel.trim(),
      house_battery_capacity_ah: parseNumber(draft.houseBatteryCapacityAh, 1440),
    },
    units: draft.distanceUnits,
    ui: {
      vessel_state_refresh_seconds: Math.round(parseNumber(draft.vesselStateRefreshSeconds, 10)),
      tank_labels: draft.tankLabels,
      tide_station_id: draft.tideStationId,
      tide_station_name: draft.tideStationName,
      tide_auto_station: draft.tideAutoStation,
    },
    anchor: {
      bow_roller_height_m: parseNumber(draft.bowRollerHeightM, 1.5),
      gps_from_bow_m: parseNonNegativeNumber(draft.gpsFromBowM, 0),
      loa_m: parseNumber(draft.loaM, 0),
      chain_size_mm: parseNumber(draft.chainSizeMm, 12),
      chain_onboard_m: parseNumber(draft.chainOnboardM, 150),
      hull_type: draft.hullType,
      windage_area_m2: parseNumber(draft.windageAreaM2, 35),
    },
    influxdb: {
      enabled: draft.influxdbEnabled,
      url: draft.influxdbUrl.trim(),
      org: draft.influxdbOrg.trim(),
      bucket: draft.influxdbBucket.trim(),
    },
    auth: {
      mode: draft.authMode,
    },
  }
}

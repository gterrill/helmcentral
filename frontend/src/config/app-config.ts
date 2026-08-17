export type HullType = 'power_cat' | 'sail_mono' | 'power_mono' | 'sail_cat'

export type AnchorConfig = {
  bowRollerHeightM: number
  chainSizeMm: number
  chainOnboardM: number
  hullType: HullType
  windageAreaM2: number
  gpsFromBowM: number
  loaM: number
}

export type DistanceUnits = 'metric' | 'imperial'

export type UiConfig = {
  vesselStateRefreshSeconds: number
  distanceUnits: DistanceUnits
  autoCloseAnchorWatchOnEngine: boolean
}

// Defaults, used until the backend's settings arrive and whenever it can't be
// reached. These were once overlaid with the repo-root settings.yaml read at
// build time, which both froze them at compile time and inlined the builder's
// private config into the bundle — see
// src/test/app-config-no-build-time-bake.test.ts. Operator values now come
// from GET /api/settings at runtime instead; see @/hooks/use-app-config.
export const fallbackUiConfig: UiConfig = {
  distanceUnits: 'metric' as DistanceUnits,
  vesselStateRefreshSeconds: 10,
  autoCloseAnchorWatchOnEngine: true,
}

// How often the browser polls the backend for a fresh forecast. This is not
// a user-facing setting: upstream provider calls are already bounded
// independently by each plugin's own ttl_seconds() (open-meteo/weatherkit
// weather 900s, open-meteo-marine waves 3600s, nws warnings 1800s, bom
// warnings 5400s), so this constant only governs how quickly the UI notices
// data the backend has already refreshed.
export const FORECAST_REFRESH_SECONDS = 600

export const fallbackAnchorConfig: AnchorConfig = {
  bowRollerHeightM: 1.5,
  chainSizeMm: 12,
  chainOnboardM: 150,
  hullType: 'power_cat',
  windageAreaM2: 35,
  gpsFromBowM: 0,
  loaM: 0,
}

/** The subset of GET /api/settings this module reads. */
export type AppConfigSettings = {
  ui?: {
    vessel_state_refresh_seconds?: number
  }
  units?: string
  anchor?: {
    bow_roller_height_m?: number
    chain_size_mm?: number
    chain_onboard_m?: number
    hull_type?: string
    windage_area_m2?: number
    gps_from_bow_m?: number
    loa_m?: number
  }
}

const HULL_TYPES: HullType[] = ['power_cat', 'sail_mono', 'power_mono', 'sail_cat']

// Every field is validated rather than trusted: settings.yaml is hand-editable
// and the endpoint will faithfully return whatever it was given. An
// unrecognised or non-positive value falls back to the default for that field
// alone, so one bad key can't take out the rest of the config.
function positiveNumber(value: unknown, fallback: number): number {
  return typeof value === 'number' && Number.isFinite(value) && value > 0 ? value : fallback
}

// gps_from_bow_m defaults to 0, meaning "no correction" — unlike every other
// anchor field, 0 is itself the meaningful, valid value (not an absent one),
// so it must not be rejected the way positiveNumber rejects 0.
function nonNegativeNumber(value: unknown, fallback: number): number {
  return typeof value === 'number' && Number.isFinite(value) && value >= 0 ? value : fallback
}

export function normalizeUiConfig(settings: AppConfigSettings | null | undefined): UiConfig {
  const config: UiConfig = { ...fallbackUiConfig }

  const units = settings?.units
  if (typeof units === 'string') {
    const normalized = units.trim().toLowerCase()
    if (normalized === 'metric' || normalized === 'imperial') {
      config.distanceUnits = normalized
    }
  }

  config.vesselStateRefreshSeconds = positiveNumber(
    settings?.ui?.vessel_state_refresh_seconds,
    fallbackUiConfig.vesselStateRefreshSeconds,
  )

  return config
}

export function normalizeAnchorConfig(settings: AppConfigSettings | null | undefined): AnchorConfig {
  const anchor = settings?.anchor

  const hullType = typeof anchor?.hull_type === 'string'
    ? anchor.hull_type.trim().toLowerCase()
    : ''

  return {
    bowRollerHeightM: positiveNumber(anchor?.bow_roller_height_m, fallbackAnchorConfig.bowRollerHeightM),
    chainSizeMm: positiveNumber(anchor?.chain_size_mm, fallbackAnchorConfig.chainSizeMm),
    chainOnboardM: positiveNumber(anchor?.chain_onboard_m, fallbackAnchorConfig.chainOnboardM),
    hullType: (HULL_TYPES as string[]).includes(hullType)
      ? (hullType as HullType)
      : fallbackAnchorConfig.hullType,
    windageAreaM2: positiveNumber(anchor?.windage_area_m2, fallbackAnchorConfig.windageAreaM2),
    gpsFromBowM: nonNegativeNumber(anchor?.gps_from_bow_m, fallbackAnchorConfig.gpsFromBowM),
    loaM: positiveNumber(anchor?.loa_m, fallbackAnchorConfig.loaM),
  }
}

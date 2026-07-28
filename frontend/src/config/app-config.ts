import YAML from 'yaml'

let settingsRawCache: string | undefined

// Deferred behind a function (not a top-level const) so importing this module
// never triggers the glob read — non-Vite consumers (e.g. the design-sync
// converter's plain esbuild bundle) can import types/values from here without
// import.meta.glob throwing at module-eval time.
function getSettingsRaw(): string {
  if (settingsRawCache === undefined) {
    settingsRawCache = Object.values(
      import.meta.glob<string>('../../../settings.yaml', {
        query: '?raw',
        import: 'default',
        eager: true,
      }),
    )[0] ?? ''
  }
  return settingsRawCache
}

export type HullType = 'power_cat' | 'sail_mono' | 'power_mono' | 'sail_cat'

export type AnchorConfig = {
  bowRollerHeightM: number
  chainSizeMm: number
  chainOnboardM: number
  hullType: HullType
  windageAreaM2: number
}

type UiConfig = {
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
  }
}

export type DistanceUnits = 'metric' | 'imperial'

const fallbackUiConfig = {
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

const fallbackAnchorConfig: AnchorConfig = {
  bowRollerHeightM: 1.5,
  chainSizeMm: 12,
  chainOnboardM: 150,
  hullType: 'power_cat',
  windageAreaM2: 35,
}

function parseUiConfig(): {
  vesselStateRefreshSeconds: number
  distanceUnits: DistanceUnits
  autoCloseAnchorWatchOnEngine: boolean
} {
  const parsedConfig = {
    ...fallbackUiConfig,
  }

  try {
    const parsed = YAML.parse(getSettingsRaw()) as UiConfig | null
    const configuredSeconds = parsed?.ui?.vessel_state_refresh_seconds
    const configuredUnits = parsed?.units

    if (typeof configuredUnits === 'string') {
      const normalized = configuredUnits.trim().toLowerCase()
      if (normalized === 'metric' || normalized === 'imperial') {
        parsedConfig.distanceUnits = normalized
      }
    }

    if (typeof configuredSeconds === 'number' && configuredSeconds > 0) {
      parsedConfig.vesselStateRefreshSeconds = configuredSeconds
    }
  } catch {
    // Keep fallback.
  }

  return parsedConfig
}

function parseAnchorConfig(): AnchorConfig {
  const parsedConfig: AnchorConfig = {
    ...fallbackAnchorConfig,
  }

  try {
    const parsed = YAML.parse(getSettingsRaw()) as UiConfig | null
    const anchor = parsed?.anchor

    if (typeof anchor?.bow_roller_height_m === 'number' && anchor.bow_roller_height_m > 0) {
      parsedConfig.bowRollerHeightM = anchor.bow_roller_height_m
    }
    if (typeof anchor?.chain_size_mm === 'number' && anchor.chain_size_mm > 0) {
      parsedConfig.chainSizeMm = anchor.chain_size_mm
    }
    if (typeof anchor?.chain_onboard_m === 'number' && anchor.chain_onboard_m > 0) {
      parsedConfig.chainOnboardM = anchor.chain_onboard_m
    }
    if (typeof anchor?.windage_area_m2 === 'number' && anchor.windage_area_m2 > 0) {
      parsedConfig.windageAreaM2 = anchor.windage_area_m2
    }
    if (typeof anchor?.hull_type === 'string') {
      const normalized = anchor.hull_type.trim().toLowerCase()
      if (
        normalized === 'power_cat'
        || normalized === 'sail_mono'
        || normalized === 'power_mono'
        || normalized === 'sail_cat'
      ) {
        parsedConfig.hullType = normalized
      }
    }
  } catch {
    // Keep fallback.
  }

  return parsedConfig
}

let uiConfigCache: ReturnType<typeof parseUiConfig> | undefined
export function getUiConfig() {
  if (!uiConfigCache) uiConfigCache = parseUiConfig()
  return uiConfigCache
}

let anchorConfigCache: AnchorConfig | undefined
export function getAnchorConfig(): AnchorConfig {
  if (!anchorConfigCache) anchorConfigCache = parseAnchorConfig()
  return anchorConfigCache
}

import YAML from 'yaml'

const settingsRaw = Object.values(
  import.meta.glob<string>('../../../settings.yaml', {
    query: '?raw',
    import: 'default',
    eager: true,
  }),
)[0] ?? ''

type BoatConfig = {
  boat: {
    name: string
    model: string
  }
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

const fallbackConfig: BoatConfig = {
  boat: {
    name: 'S/V INGENUITY',
    model: '2018 FP SAONA 47',
  },
}

const fallbackUiConfig = {
  distanceUnits: 'metric' as DistanceUnits,
  vesselStateRefreshSeconds: 10,
  autoCloseAnchorWatchOnEngine: true,
}

const fallbackAnchorConfig: AnchorConfig = {
  bowRollerHeightM: 1.5,
  chainSizeMm: 12,
  chainOnboardM: 150,
  hullType: 'power_cat',
  windageAreaM2: 35,
}

function parseBoatConfig(): BoatConfig {
  try {
    const parsed = YAML.parse(settingsRaw) as Partial<BoatConfig> | null

    if (!parsed?.boat?.name || !parsed.boat.model) {
      return fallbackConfig
    }

    return {
      boat: {
        name: parsed.boat.name,
        model: parsed.boat.model,
      },
    }
  } catch {
    return fallbackConfig
  }
}

function parseUiConfig(): { vesselStateRefreshSeconds: number; distanceUnits: DistanceUnits; autoCloseAnchorWatchOnEngine: boolean } {
  const parsedConfig = {
    ...fallbackUiConfig,
  }

  try {
    const parsed = YAML.parse(settingsRaw) as UiConfig | null
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
    const parsed = YAML.parse(settingsRaw) as UiConfig | null
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

export const appConfig = parseBoatConfig()
export const uiConfig = parseUiConfig()
export const anchorConfig = parseAnchorConfig()

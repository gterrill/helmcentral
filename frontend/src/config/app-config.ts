import YAML from 'yaml'

import settingsRaw from '../../../settings.yaml?raw'

type BoatConfig = {
  boat: {
    name: string
    model: string
  }
}

type UiConfig = {
  ui?: {
    vessel_state_refresh_seconds?: number
  }
}

const fallbackConfig: BoatConfig = {
  boat: {
    name: 'S/V INGENUITY',
    model: '2018 FP SAONA 47',
  },
}

const fallbackUiConfig = {
  vesselStateRefreshSeconds: 10,
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

function parseUiConfig(): { vesselStateRefreshSeconds: number } {
  try {
    const parsed = YAML.parse(settingsRaw) as UiConfig | null
    const configuredSeconds = parsed?.ui?.vessel_state_refresh_seconds

    if (typeof configuredSeconds === 'number' && configuredSeconds > 0) {
      return {
        vesselStateRefreshSeconds: configuredSeconds,
      }
    }
  } catch {
    // Keep fallback.
  }

  return fallbackUiConfig
}

export const appConfig = parseBoatConfig()
export const uiConfig = parseUiConfig()

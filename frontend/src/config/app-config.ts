import YAML from 'yaml'

import settingsRaw from '../../../settings.yaml?raw'

type BoatConfig = {
  boat: {
    name: string
    model: string
  }
}

const fallbackConfig: BoatConfig = {
  boat: {
    name: 'S/V INGENUITY',
    model: '2018 FP SAONA 47',
  },
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

export const appConfig = parseBoatConfig()

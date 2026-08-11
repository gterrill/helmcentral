export type HullType = 'power_cat' | 'sail_mono' | 'power_mono' | 'sail_cat'

export type AnchorConfig = {
  bowRollerHeightM: number
  chainSizeMm: number
  chainOnboardM: number
  hullType: HullType
  windageAreaM2: number
}

export type DistanceUnits = 'metric' | 'imperial'

// Build-independent defaults. These used to be overlaid with the repo-root
// settings.yaml read at build time, which both froze them at compile time and
// inlined the builder's private config into the bundle — see
// src/test/app-config-no-build-time-bake.test.ts.
//
// KNOWN GAP: `units` and the `anchor` block are operator-configurable in
// settings.yaml and the Settings UI, but nothing reads them back at runtime,
// so these defaults are what every shipped artifact uses. That predates the
// packaging work (the Docker image never copied settings.yaml either) and is
// tracked separately; wiring them to GET /api/settings is the fix.
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

export function getUiConfig(): {
  vesselStateRefreshSeconds: number
  distanceUnits: DistanceUnits
  autoCloseAnchorWatchOnEngine: boolean
} {
  return fallbackUiConfig
}

export function getAnchorConfig(): AnchorConfig {
  return fallbackAnchorConfig
}

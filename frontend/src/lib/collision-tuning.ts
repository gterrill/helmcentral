/**
 * Builds the URL to the AIS Target Prioritizer plugin's own webapp, where a
 * collision alarm's CPA/TCPA thresholds actually live (ADR 0090). This
 * mirrors backend/signalk.go's buildSignalKURL (~line 2133) for turning a
 * configured address into a base URL, with one deliberate difference: an
 * empty/whitespace address returns null here instead of defaulting to
 * localhost. The backend defaults to localhost because it runs on the same
 * host as SignalK; the browser does not, so a guessed host would produce a
 * broken link, and omitting the link is the honest outcome.
 */

/** Path SignalK serves the plugin's webapp under, appended to the base URL. */
export const COLLISION_TUNING_WEBAPP_PATH = '/signalk-ais-target-prioritizer/'

// Mirrors backend/signalk.go's defaultSignalKPort. Used only when the
// address has no scheme and no usable port was given.
const DEFAULT_SIGNALK_PORT = 3000

export function collisionTuningUrl(address: string | undefined, port: number | undefined): string | null {
  const trimmed = (address ?? '').trim()
  if (trimmed === '') return null

  if (trimmed.startsWith('http://') || trimmed.startsWith('https://')) {
    return `${trimmed.replace(/\/+$/, '')}${COLLISION_TUNING_WEBAPP_PATH}`
  }

  const usablePort = port === undefined || port <= 0 || port > 65535 ? DEFAULT_SIGNALK_PORT : port
  return `http://${trimmed}:${usablePort}${COLLISION_TUNING_WEBAPP_PATH}`
}

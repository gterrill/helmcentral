import { useMemo } from 'react'

import { useApiQuery } from '@/lib/api-query'
import { buildEchoPalette, parseCapabilities, type RadarCapabilities } from '@/lib/radar-echo/legend'

export interface RadarCapabilitiesResult {
  capabilities: RadarCapabilities | null
  palette: Uint32Array | null
  error: string | null
  loading: boolean
}

// No radar id selected: nothing to load, nothing wrong, not in flight.
// Distinct from LOADING below so a caller can tell "hasn't started" apart
// from "fetch in progress".
const IDLE: RadarCapabilitiesResult = { capabilities: null, palette: null, error: null, loading: false }
const LOADING: RadarCapabilitiesResult = { capabilities: null, palette: null, error: null, loading: true }

function errorResult(message: string): RadarCapabilitiesResult {
  return { capabilities: null, palette: null, error: message, loading: false }
}

/**
 * Fetches and caches one radar's capabilities: the colour legend and the
 * spoke/range geometry constants the picture overlay needs, proxied
 * verbatim by the backend from mayara (`GET /api/radar/capabilities?radar=<id>`).
 *
 * `capabilities` and `palette` are only ever both present or both null --
 * never a palette built from a partial legend. See `error` for why.
 *
 * IMPORTANT: `capabilities.spokesPerRevolution` here is exactly what the
 * radar's capabilities response says -- 8192 on the boat's Furuno -- NOT
 * the number of spokes actually observed on the wire (4096: every angle is
 * even, stride 2; see backend/testdata/mayara/spokes-fur6424A.md). This
 * hook reports the capabilities payload as-is, on purpose. The real spoke
 * count belongs to whichever consumer derives it from observed angles
 * (the decoder/buffer, not this hook) -- correcting the mismatch here would
 * just hide it from that consumer instead of fixing it.
 */
export function useRadarCapabilities(radarId: string | null): RadarCapabilitiesResult {
  // Tile and drawer can both be showing the overlay for the same radar id at
  // once, and capabilities only change on a range change (the backend itself
  // tolerates a 30s TTL), so the shared query store gives the second mount
  // the first one's answer instead of a duplicate GET. The body is parsed
  // below rather than in `select`, so an unparseable payload is reported as
  // an error instead of throwing during render.
  const { data, error } = useApiQuery<unknown>({
    queryKey: ['/api/radar/capabilities', radarId],
    path: `/api/radar/capabilities?radar=${encodeURIComponent(radarId ?? '')}`,
    enabled: radarId !== null,
  })

  return useMemo(() => {
    if (radarId === null) return IDLE
    if (data !== undefined) {
      const capabilities = parseCapabilities(data)
      if (capabilities === null) {
        // Per AGENTS.md's fallback policy: a payload that doesn't parse
        // cleanly must never fall back to a partial or defaulted legend --
        // that would draw real returns in synthesized colours. A radar whose
        // capabilities can't be read is one whose picture must not be drawn.
        return errorResult(`radar capabilities for ${radarId} could not be parsed`)
      }
      return { capabilities, palette: buildEchoPalette(capabilities.legend), error: null, loading: false }
    }
    // radarCapabilitiesHandler (backend/radar_capabilities.go) answers a
    // failure as JSON carrying an `error` key (502), never an empty or
    // defaulted legend; ApiError carries that message through.
    if (error !== null) return errorResult(error.message)
    return LOADING
  }, [radarId, data, error])
}

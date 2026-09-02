import { useEffect, useState } from 'react'

import { apiBaseUrl } from '@/config/api'
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

/**
 * Per-radar-id cache, module-level rather than per-hook-instance. Tile and
 * drawer can both be showing the overlay for the same radar id at once (the
 * plan's "Two mounted maps"), and capabilities only change on a range
 * change -- which the backend itself already tolerates with a 30s TTL
 * (radarCapabilitiesCacheTTL, backend/radar_capabilities.go) -- so a second
 * mount for the same id reuses this rather than issuing a duplicate GET.
 * A failed result is cached too: an unconfigured or unreachable radar
 * doesn't become configured mid-session, and re-hitting the backend on
 * every remount would just repeat the same 502.
 */
const cache = new Map<string, RadarCapabilitiesResult>()
const inflight = new Map<string, Promise<void>>()
const listeners = new Map<string, Set<(result: RadarCapabilitiesResult) => void>>()

function publish(radarId: string, result: RadarCapabilitiesResult): void {
  cache.set(radarId, result)
  for (const listener of listeners.get(radarId) ?? []) listener(result)
}

function errorResult(message: string): RadarCapabilitiesResult {
  return { capabilities: null, palette: null, error: message, loading: false }
}

async function load(radarId: string): Promise<void> {
  try {
    const response = await fetch(`${apiBaseUrl}/api/radar/capabilities?radar=${encodeURIComponent(radarId)}`)
    const body: unknown = await response.json().catch(() => null)

    if (!response.ok) {
      // radarCapabilitiesHandler (backend/radar_capabilities.go) answers a
      // failure as JSON carrying an `error` key (502), never an empty or
      // defaulted legend -- surface that message rather than a generic one.
      const message =
        body !== null && typeof body === 'object' && typeof (body as { error?: unknown }).error === 'string'
          ? (body as { error: string }).error
          : `radar capabilities request for ${radarId} failed with status ${response.status}`
      publish(radarId, errorResult(message))
      return
    }

    const capabilities = parseCapabilities(body)
    if (capabilities === null) {
      // Per AGENTS.md's fallback policy: a payload that doesn't parse
      // cleanly must never fall back to a partial or defaulted legend --
      // that would draw real returns in synthesized colours. A radar whose
      // capabilities can't be read is one whose picture must not be drawn.
      publish(radarId, errorResult(`radar capabilities for ${radarId} could not be parsed`))
      return
    }

    const palette = buildEchoPalette(capabilities.legend)
    publish(radarId, { capabilities, palette, error: null, loading: false })
  } catch (err) {
    publish(radarId, errorResult(err instanceof Error ? err.message : `radar capabilities request for ${radarId} failed`))
  }
}

function ensureLoaded(radarId: string): void {
  if (cache.has(radarId) || inflight.has(radarId)) return
  const promise = load(radarId).finally(() => inflight.delete(radarId))
  inflight.set(radarId, promise)
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
  const [result, setResult] = useState<RadarCapabilitiesResult>(() => (radarId !== null ? (cache.get(radarId) ?? LOADING) : IDLE))

  useEffect(() => {
    if (radarId === null) {
      setResult(IDLE)
      return
    }

    setResult(cache.get(radarId) ?? LOADING)

    let radarListeners = listeners.get(radarId)
    if (!radarListeners) {
      radarListeners = new Set()
      listeners.set(radarId, radarListeners)
    }
    radarListeners.add(setResult)

    ensureLoaded(radarId)

    return () => {
      radarListeners!.delete(setResult)
      if (radarListeners!.size === 0) listeners.delete(radarId)
    }
  }, [radarId])

  return result
}

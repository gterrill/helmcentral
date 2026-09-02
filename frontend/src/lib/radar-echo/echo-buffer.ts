import type { EchoLut } from '@/lib/radar-echo/geometry'
import type { RadarSpoke } from '@/lib/radar-echo/spoke-message'

/**
 * The buffer IS the accumulator; there is no separate "frame". Each row
 * holds the latest bytes seen for one bearing plus when it was last
 * updated, so a partial sweep is simply a buffer with some rows older than
 * others, and the picture rotates into place naturally as fresh spokes
 * arrive -- there is nothing to assemble or swap.
 */
export interface EchoBuffer {
  spokeCount: number
  binCount: number
  /** spokeCount * binCount bytes, row-major: row = spoke index. */
  data: Uint8Array
  /** Epoch-ms (or any consistent clock) each row was last written. */
  lastSeenMs: Float64Array
  rangeM: number
}

/**
 * About 2.5 missed revolutions at 24-48rpm (a typical marine radar's
 * range; this boat's DRS4D-NXT measured at 7.9rpm in the Phase 0 capture,
 * slower still). A frozen radar picture around the boat reads as live
 * returns and is worse than no picture -- the failure this integration has
 * met four times before on other inputs, arriving on a fifth here. The
 * caller also clears the whole buffer immediately on disconnect and on
 * range change; this constant only governs the ordinary case of one row
 * going quiet while its neighbours keep updating.
 */
export const RADAR_ECHO_SPOKE_MAX_AGE_MS = 6000

export function createEchoBuffer(spokeCount: number, binCount: number): EchoBuffer {
  const lastSeenMs = new Float64Array(spokeCount)
  lastSeenMs.fill(-Infinity) // never seen: infinitely stale under any finite `now`
  return {
    spokeCount,
    binCount,
    data: new Uint8Array(spokeCount * binCount),
    lastSeenMs,
    rangeM: 0,
  }
}

/**
 * Write one decoded spoke into its row. `row` is the caller's resolved
 * spoke index (e.g. `angle >> 1` given the Phase 0-measured angle step of
 * 2), not derived here -- this module knows nothing about the wire's angle
 * encoding.
 *
 * Phase 0 measured every captured spoke at exactly `binCount` (1024) bytes
 * despite `hasSparseSpokes: true`, so there is deliberately no resampling
 * here. If a future capture proves a shorter spoke exists, a short write
 * simply leaves that row's tail bins holding whatever they held before --
 * stale data, which reads as a smaller problem than fabricated pixels.
 */
export function ingestSpoke(buf: EchoBuffer, spoke: RadarSpoke, row: number, nowMs: number): void {
  const offset = row * buf.binCount
  const length = Math.min(spoke.data.length, buf.binCount)
  buf.data.set(spoke.data.subarray(0, length), offset)
  buf.lastSeenMs[row] = nowMs
}

/** Blank every row untouched for longer than maxAgeMs. Returns how many. */
export function expireStaleSpokes(buf: EchoBuffer, nowMs: number, maxAgeMs: number): number {
  let expired = 0
  for (let row = 0; row < buf.spokeCount; row++) {
    if (nowMs - buf.lastSeenMs[row] > maxAgeMs) {
      const start = row * buf.binCount
      buf.data.fill(0, start, start + buf.binCount)
      buf.lastSeenMs[row] = -Infinity
      expired += 1
    }
  }
  return expired
}

/** Immediate, total clear: socket close and range change both call this. */
export function clearEchoBuffer(buf: EchoBuffer): void {
  buf.data.fill(0)
  buf.lastSeenMs.fill(-Infinity)
}

/**
 * Paint the buffer through the LUT and palette into `out`, an
 * ImageData-shaped Uint32Array. Branch-free by construction: `inside`
 * (0 or 1) multiplies the looked-up colour instead of an if, so the cost
 * is identical for an empty ocean and a target-dense sweep, and there is
 * nothing here for a JIT to mispredict.
 */
export function fillEchoPixels(out: Uint32Array, buf: EchoBuffer, lut: EchoLut, palette: Uint32Array): void {
  const { spokeIndex, rangeBin, inside } = lut
  const { data, binCount } = buf
  const n = spokeIndex.length
  for (let i = 0; i < n; i++) {
    const bin = spokeIndex[i] * binCount + rangeBin[i]
    out[i] = palette[data[bin]] * inside[i]
  }
}

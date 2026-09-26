import type { TideToday } from '@/hooks/use-tide-today'
import { feetToMeters } from '@/lib/units'

/**
 * How long a tide reading stays usable after its own timestamp.
 * useTideToday polls every ten minutes; thirty minutes gives it three missed
 * polls of slack before this warning would rather say nothing than reason
 * from a tide reading that might no longer be true. Exported so tests (and
 * the ADR) can cite the same number rather than duplicate it.
 */
export const TIDE_STALE_AFTER_MINUTES = 30

export type LowWaterClearanceReason = 'no_depth' | 'no_draft' | 'no_tide' | 'tide_stale'

export type LowWaterClearanceResult =
  | { status: 'unknown'; reason: LowWaterClearanceReason }
  | {
      status: 'ok' | 'too_shallow'
      /** Depth at the boat's current position, projected to the next low tide. */
      depthAtLowM: number
      /** Water under the keel at the next low tide (depthAtLowM - draftM). */
      clearanceM: number
      lowTideTime: string
    }

export interface LowWaterClearanceInput {
  /** Live environment.depth.belowTransducer, or null when no depth reading is available. */
  depthM: number | null
  tide: TideToday | null
  /** Vessel's maximum design draft, or null when SignalK publishes none. */
  draftM: number | null
  /** Minimum water the operator wants under the keel at low tide (settings.anchor.min_clearance_at_low_m). */
  marginM: number
  /**
   * The instant to reason from. Callers pass `new Date()` computed fresh at
   * render time (depth is live; the tide is not, so freshness has to be
   * judged against "now," not baked into the function).
   */
  now: Date
}

/**
 * Whether a tide height in feet is real data rather than the -1 sentinel
 * useTideToday substitutes for "not published" (or a non-number it
 * coerced to -1, see use-tide-today.ts). The sentinel is exactly -1, not
 * "-1 or below" - heights below chart datum are real (spring lows commonly
 * read negative, and can fall below -1 ft on some datums), so unlike
 * tideHeightFtOrNull (rode-plan.ts, `>= 0`, which this module deliberately
 * does not reuse) this only rejects the sentinel value itself, not negative
 * numbers generally. A real reading of exactly -1.0 ft is indistinguishable
 * from the sentinel (docs/adr/0135, accepted).
 */
function isUsableTideHeightFt(value: number): boolean {
  return Number.isFinite(value) && value !== -1
}

/**
 * Anchor Watch's low-water clearance warning (ADR 0135): projects the depth
 * at the boat's current position forward to the next low tide and compares
 * the water that would be left under the keel against the operator's
 * configured margin.
 *
 * No masking fallback (AGENTS.md's fallback policy): depth, draft and a
 * usable tide (both the current height and the next low height - usable
 * meaning not the -1 sentinel useTideToday substitutes for "not published",
 * per isUsableTideHeightFt above; a real reading below chart datum, e.g.
 * -0.4 ft on a spring low, is accepted) are each required, with a distinct
 * reason when one is missing, rather than the function silently substituting
 * a zero tide fall or a zero draft. A tide reading older than
 * TIDE_STALE_AFTER_MINUTES, or one whose "next low" has already passed, is
 * treated the same as missing (`tide_stale`) for the same reason: depth is
 * live on every render, but the tide is only as fresh as the last successful
 * poll, and useTideToday keeps serving that same reading indefinitely if a
 * later poll fails.
 *
 * belowTransducer under-reads true depth by the transducer's own immersion
 * (there is no surfaceToTransducer offset published), so depthAtLowM and
 * clearanceM both run slightly conservative (shallower than reality) - the
 * warning errs on the safe, early side.
 */
export function computeLowWaterClearance(input: LowWaterClearanceInput): LowWaterClearanceResult {
  if (typeof input.depthM !== 'number' || !Number.isFinite(input.depthM) || input.depthM < 0) {
    return { status: 'unknown', reason: 'no_depth' }
  }
  if (typeof input.draftM !== 'number' || !Number.isFinite(input.draftM) || input.draftM <= 0) {
    return { status: 'unknown', reason: 'no_draft' }
  }

  const currentFt =
    input.tide != null && isUsableTideHeightFt(input.tide.current_tide_height_ft)
      ? input.tide.current_tide_height_ft
      : null
  const nextLowFt =
    input.tide != null && isUsableTideHeightFt(input.tide.low_tide_height_ft)
      ? input.tide.low_tide_height_ft
      : null
  if (currentFt === null || nextLowFt === null) {
    return { status: 'unknown', reason: 'no_tide' }
  }

  // Depth is live; the tide reading is whatever useTideToday last managed to
  // fetch, and it keeps serving that same reading indefinitely if a later
  // poll fails. Two ways for it to have gone stale: the reading itself is
  // old, or the low it names is already behind us (the "next low" is no
  // longer next). An unparseable timestamp is treated the same as stale
  // rather than let NaN arithmetic silently produce a false ok/too_shallow.
  const tideDatetimeMs = new Date(input.tide!.datetime).getTime()
  const lowTideTimeMs = new Date(input.tide!.low_tide_time).getTime()
  const nowMs = input.now.getTime()
  const readingIsStale = Number.isNaN(tideDatetimeMs) || nowMs - tideDatetimeMs > TIDE_STALE_AFTER_MINUTES * 60 * 1000
  const lowHasPassed = Number.isNaN(lowTideTimeMs) || lowTideTimeMs <= nowMs
  if (readingIsStale || lowHasPassed) {
    return { status: 'unknown', reason: 'tide_stale' }
  }

  // If the current level is already at or below the next low's forecast
  // height, the shallowest water before that low is right now - the fall
  // clamps to zero rather than going negative (which would raise the
  // projected depth, the wrong direction for a safety warning).
  const fallFt = Math.max(0, currentFt - nextLowFt)
  const fallM = feetToMeters(fallFt)

  const depthAtLowM = input.depthM - fallM
  const clearanceM = depthAtLowM - input.draftM
  const status = clearanceM < input.marginM ? 'too_shallow' : 'ok'

  return {
    status,
    depthAtLowM,
    clearanceM,
    lowTideTime: input.tide!.low_tide_time,
  }
}

/** What's missing, in the operator's words. Shared by the tile and the drawer. */
export function lowWaterClearanceReasonLabel(reason: LowWaterClearanceReason): string {
  switch (reason) {
    case 'no_depth':
      return 'No depth'
    case 'no_draft':
      return 'No draft from the boat'
    case 'no_tide':
      return 'No tide station'
    case 'tide_stale':
      return 'Tide forecast out of date'
  }
}

/**
 * The clearance as the operator reads it. A negative clearance means the keel
 * would be on the bottom at low water, which "-0.3 m under keel" gets across
 * badly, so it gets its own wording. Rounded to one place first, so a figure
 * that rounds to zero never prints as "-0.0".
 */
export function underKeelPhrase(clearanceM: number): string {
  const rounded = Math.round(clearanceM * 10) / 10
  if (rounded < 0) return `keel ${Math.abs(rounded).toFixed(1)} m into the bottom`
  return `${Math.abs(rounded).toFixed(1)} m under keel`
}

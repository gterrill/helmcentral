/**
 * How the Anchor Watch's low-water clearance reads (ADR 0135). The figures are
 * worked out on the server (ADR 0272) and arrive through useAnchorLowWater;
 * this file holds the shape they arrive in and the words the page uses.
 */

export type LowWaterClearanceReason =
  | 'no_depth'
  | 'no_draft'
  | 'no_tide_station'
  | 'tide_unreachable'
  | 'no_tide'
  | 'tide_stale'
  | 'not_anchored'

export type LowWaterClearanceResult =
  | { status: 'unknown'; reason: LowWaterClearanceReason }
  | {
      status: 'ok' | 'too_shallow'
      /** Depth at the boat's current position, projected to the next low tide. */
      depthAtLowM: number
      /** Water under the keel at the next low tide (depthAtLowM - draft). */
      clearanceM: number
      lowTideTime: string
    }

/** What's missing, in the operator's words. Shared by the tile and the drawer. */
export function lowWaterClearanceReasonLabel(reason: LowWaterClearanceReason): string {
  switch (reason) {
    case 'no_depth':
      return 'No depth'
    case 'no_draft':
      return 'No draft from the boat'
    case 'no_tide_station':
      return 'No tide station'
    case 'tide_unreachable':
    case 'no_tide':
      return 'Tide unavailable'
    case 'not_anchored':
      return 'Not anchored'
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

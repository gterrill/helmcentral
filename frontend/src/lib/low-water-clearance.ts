/**
 * How the Anchor Watch's low-water clearance reads (ADR 0135). The figures are
 * worked out on the server (ADR 0272) and arrive through useAnchorLowWater;
 * this file holds the shape they arrive in and the words the page uses.
 */

export type LowWaterClearanceReason =
  | 'no_depth'
  | 'no_draft'
  | 'no_tide_station'
  | 'no_position'
  | 'no_plugin'
  | 'tide_unreachable'
  | 'no_tide'
  | 'tide_stale'
  | 'not_anchored'

/** Why the local tide calibration (ADR 0274) is not ready, in the server's words. */
export type LowWaterCalibrationNotReady =
  | 'too_few_readings'
  | 'readings_too_scattered'
  | 'implausible_fit'
  | 'no_recent_reading'
  | 'no_tide_curve'
  | 'no_clearance_set'
  | 'working_it_out'
  | 'unavailable'

/** Which figure the verdict rests on once calibrated. */
export type LowWaterCautiousSource = 'boat_now' | 'station' | 'shallowest_sounded'

/**
 * The local calibration beside the station estimate. A depth is metres and a
 * half width is the 95% interval. The figures are there only when ready.
 */
export interface LowWaterCalibration {
  ready: boolean
  notReady?: LowWaterCalibrationNotReady
  hoursOfReadings: number
  /** The shallowest spot sounded, projected to the low; shown when it differs from the boat's cell, or sets the verdict. */
  shallowestSounded?: { depthM: number; halfWidthM: number }
  shallowestDiffers: boolean
  highTideTime?: string
  highAtBoat?: { depthM: number; halfWidthM: number }
  deepestSounded?: { depthM: number; halfWidthM: number }
  deepestDiffers: boolean
  /** "The tide here doesn't follow ...", from the server, or absent. */
  stationMatchNote?: string
}

/** What the tide prediction is called: the model that follows the position, or a station. */
export function tideSourceName(verdict: { tideIsPoint?: boolean }): 'tide model' | 'tide station' {
  return verdict.tideIsPoint === true ? 'tide model' : 'tide station'
}

export type LowWaterClearanceResult =
  | { status: 'unknown'; reason: LowWaterClearanceReason }
  | {
      status: 'ok' | 'too_shallow'
      /** Depth at the boat's current position, projected to the next low tide. */
      depthAtLowM: number
      /** Water under the keel at the next low tide (depthAtLowM - draft). */
      clearanceM: number
      lowTideTime: string
      /** The 95% half width of depthAtLowM, there while calibration.ready: the depth is then the calibrated one, otherwise the station's. */
      depthAtLowHalfWidthM?: number
      cautiousSource?: LowWaterCautiousSource
      /** The clearance of the figure the verdict rests on, with its half width when it has one. */
      verdictClearanceM?: number
      verdictHalfWidthM?: number
      calibration?: LowWaterCalibration
      /** The tide comes from the worldwide model that follows the position, not a tide station. */
      tideIsPoint?: boolean
    }

/** How a length is written for the operator: the digits and the unit beside them. */
export interface LengthFormat {
  format: (meters: number) => string
  unit: string
}

/**
 * The clearance of a calibrated estimate with its 95% range, in the operator's
 * units, such as "0.8 m ±0.4 under keel, could be 0.4 m". The "could be" half is
 * the bottom of the range, which is what the warning is judged on.
 */
export function underKeelRangePhrase(clearanceM: number, halfWidthM: number, withBottom: boolean, length: LengthFormat): string {
  const { format, unit } = length
  const intoBottom = (m: number) => `keel ${format(Math.abs(m))} ${unit} into the bottom`
  const head = Math.round(clearanceM * 10) / 10 < 0 ? `${intoBottom(clearanceM)} ±${format(halfWidthM)} ${unit}` : `${format(clearanceM)} ${unit} ±${format(halfWidthM)} under keel`
  if (!withBottom) return head
  const bottom = clearanceM - halfWidthM
  return `${head}, could be ${Math.round(bottom * 10) / 10 < 0 ? intoBottom(bottom) : `${format(bottom)} ${unit}`}`
}

/** Hours of readings as the label reads them: "4 h", or minutes under the first hour. */
export function readingsSpanText(hours: number): string {
  if (hours >= 1) return `${Math.floor(hours)} h`
  return `${Math.max(1, Math.round(hours * 60))} min`
}

/** Why the calibration is not ready yet, for the line under the station estimate. */
export function calibrationNotReadyText(reason: LowWaterCalibrationNotReady, hoursOfReadings: number, isPoint = false): string {
  switch (reason) {
    case 'too_few_readings':
      return `calibrating, ${readingsSpanText(hoursOfReadings)} of readings so far`
    case 'readings_too_scattered':
      return 'readings too scattered to calibrate yet'
    case 'implausible_fit':
      return isPoint ? "readings don't fit the tide model, not used" : "readings don't fit the station's tide, not used"
    case 'no_recent_reading':
      return 'no recent depth readings to calibrate with'
    case 'no_tide_curve':
      return isPoint ? "the tide model's predictions don't cover the readings" : "the station's predictions don't cover the readings"
    case 'working_it_out':
      return 'working out the calibration'
    case 'no_clearance_set':
      return 'set a low-water clearance in Anchor Watch settings to judge the calibration'
    case 'unavailable':
      return 'calibration unavailable'
  }
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
    case 'no_position':
      return 'No position for the tide'
    case 'no_plugin':
      return 'Tide plugin not installed'
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

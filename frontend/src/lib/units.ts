export function fahrenheitToCelsius(tempF: number): number {
  return (tempF - 32) * (5 / 9)
}

/**
 * The one copy of this ratio (code-review finding: anchor-adjust.ts,
 * low-water-clearance.ts, tide-estimate.ts, anchor-watch-drawer.tsx and
 * anchor-adjust-bar.tsx each grew their own local 3.28084 rather than
 * importing this). Exported (not just used internally by metersToFeet/
 * feetToMeters below) for the rare caller that needs the bare ratio itself,
 * e.g. a step size given in feet that has to convert the other direction.
 */
export const METERS_PER_FOOT = 3.28084

export function metersToFeet(meters: number): number {
  return meters * METERS_PER_FOOT
}

export function feetToMeters(feet: number): number {
  return feet / METERS_PER_FOOT
}

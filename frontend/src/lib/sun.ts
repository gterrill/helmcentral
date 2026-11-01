// Where the sun is. The low-precision solar position of Meeus' Astronomical
// Algorithms, the same used by the SunCalc library: good to a fraction of a
// degree, which is far finer than a sky colour needs. Pure; a replay frame asks
// it for the boat's position and the track's own time.

const RAD = Math.PI / 180
const DAY_MS = 86_400_000
const J1970 = 2_440_588
const J2000 = 2_451_545
const OBLIQUITY = RAD * 23.4397

export interface SunPosition {
  /** Degrees above the horizon, negative below it. No allowance for refraction. */
  altitudeDeg: number
  /** Degrees clockwise from true north, 0 to 360: the way to face the sun. */
  azimuthDeg: number
}

/** The sun for a place and an instant (Unix milliseconds). */
export function sunPosition(atMs: number, latDeg: number, lonDeg: number): SunPosition {
  const days = atMs / DAY_MS - 0.5 + J1970 - J2000
  const meanAnomaly = RAD * (357.5291 + 0.98560028 * days)
  const centre = RAD * (1.9148 * Math.sin(meanAnomaly) + 0.02 * Math.sin(2 * meanAnomaly) + 0.0003 * Math.sin(3 * meanAnomaly))
  const eclipticLongitude = meanAnomaly + centre + RAD * 102.9372 + Math.PI
  const declination = Math.asin(Math.sin(OBLIQUITY) * Math.sin(eclipticLongitude))
  const rightAscension = Math.atan2(Math.cos(OBLIQUITY) * Math.sin(eclipticLongitude), Math.cos(eclipticLongitude))
  const phi = RAD * latDeg
  const hourAngle = RAD * (280.16 + 360.9856235 * days) - RAD * -lonDeg - rightAscension
  const altitude = Math.asin(Math.sin(phi) * Math.sin(declination) + Math.cos(phi) * Math.cos(declination) * Math.cos(hourAngle))
  // Measured from the south, westward; turned to a compass bearing.
  const fromSouth = Math.atan2(Math.sin(hourAngle), Math.cos(hourAngle) * Math.sin(phi) - Math.tan(declination) * Math.cos(phi))
  return { altitudeDeg: altitude / RAD, azimuthDeg: (((fromSouth + Math.PI) / RAD) % 360 + 360) % 360 }
}

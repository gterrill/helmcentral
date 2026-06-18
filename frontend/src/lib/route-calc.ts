import { haversineMeters, bearingDeg } from '@/lib/geo'

export interface RouteWaypoint {
  lat: number
  lon: number
  name?: string
}

export interface RouteLeg {
  fromIndex: number
  toIndex: number
  distanceM: number
  bearingDeg: number
  etaHours: number
}

export interface RouteTotals {
  totalDistanceM: number
  totalEtaHours: number
}

const METERS_PER_NM = 1852

export function calculateLegs(waypoints: RouteWaypoint[], speedKts: number): RouteLeg[] {
  const legs: RouteLeg[] = []
  for (let i = 0; i < waypoints.length - 1; i++) {
    const from = waypoints[i]
    const to = waypoints[i + 1]
    const distanceM = haversineMeters(from.lat, from.lon, to.lat, to.lon)
    const distanceNm = distanceM / METERS_PER_NM
    legs.push({
      fromIndex: i,
      toIndex: i + 1,
      distanceM,
      bearingDeg: bearingDeg(from.lat, from.lon, to.lat, to.lon),
      etaHours: speedKts > 0 ? distanceNm / speedKts : Infinity,
    })
  }
  return legs
}

export function calculateRouteTotals(legs: RouteLeg[]): RouteTotals {
  return legs.reduce(
    (totals, leg) => ({
      totalDistanceM: totals.totalDistanceM + leg.distanceM,
      totalEtaHours: totals.totalEtaHours + leg.etaHours,
    }),
    { totalDistanceM: 0, totalEtaHours: 0 },
  )
}

export function formatNm(distanceM: number): string {
  const nm = distanceM / METERS_PER_NM
  return `${nm.toFixed(nm < 10 ? 2 : 1)} nm`
}

export function formatEtaHours(hours: number): string {
  if (!Number.isFinite(hours)) return '—'
  const totalMinutes = Math.round(hours * 60)
  const h = Math.floor(totalMinutes / 60)
  const m = totalMinutes % 60
  if (h === 0) return `${m}m`
  return `${h}h ${String(m).padStart(2, '0')}m`
}

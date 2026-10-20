export interface RouteWaypoint {
  lat: number
  lon: number
  name?: string
}

const METERS_PER_NM = 1852

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

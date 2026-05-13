import { useCallback, useEffect, useRef, useState } from 'react'
import type { SeabedType, SeaState } from '@/lib/catenary'

export type AnchorWatchState = 'none' | 'set' | 'dragging'

interface AnchorWatchServerState {
  active: boolean
  lat?: number
  lon?: number
  radius_meters?: number
  rode_deployed_m?: number
  sea_state?: SeaState
  seabed_type?: SeabedType
  set_at?: string
}

export interface AnchorWatchResult {
  anchorState: AnchorWatchState
  anchorLat: number | null
  anchorLon: number | null
  radiusMeters: number
  rodeDeployedM: number
  seaState: SeaState
  seabedType: SeabedType
  distanceMeters: number | null
  bearingDeg: number | null
  suggestSet: boolean
  setAnchorHere: (lat: number, lon: number, radiusMeters?: number) => Promise<void>
  updateRadius: (radiusMeters: number) => Promise<void>
  updateRodeAndConditions: (rodeDeployedM: number, seaState: SeaState, seabedType: SeabedType) => Promise<void>
  clearAnchor: () => Promise<void>
}

const DRAG_BUFFER_METERS = 4.572 // 15 ft
const DEFAULT_RADIUS_METERS = 45.72 // 150 ft
const DEFAULT_SEA_STATE: SeaState = 'calm'
const DEFAULT_SEABED_TYPE: SeabedType = 'sand'

function isSeaState(value: string | undefined): value is SeaState {
  return value === 'calm' || value === 'choppy' || value === 'rough' || value === 'storm'
}

function isSeabedType(value: string | undefined): value is SeabedType {
  return value === 'sand' || value === 'mud' || value === 'rock' || value === 'grass'
}

function haversineMeters(lat1: number, lon1: number, lat2: number, lon2: number): number {
  const R = 6371000
  const toRad = (d: number) => (d * Math.PI) / 180
  const dLat = toRad(lat2 - lat1)
  const dLon = toRad(lon2 - lon1)
  const a =
    Math.sin(dLat / 2) ** 2 +
    Math.cos(toRad(lat1)) * Math.cos(toRad(lat2)) * Math.sin(dLon / 2) ** 2
  return R * 2 * Math.atan2(Math.sqrt(a), Math.sqrt(1 - a))
}

function bearingDegrees(lat1: number, lon1: number, lat2: number, lon2: number): number {
  const toRad = (d: number) => (d * Math.PI) / 180
  const toDeg = (r: number) => (r * 180) / Math.PI
  const dLon = toRad(lon2 - lon1)
  const y = Math.sin(dLon) * Math.cos(toRad(lat2))
  const x =
    Math.cos(toRad(lat1)) * Math.sin(toRad(lat2)) -
    Math.sin(toRad(lat1)) * Math.cos(toRad(lat2)) * Math.cos(dLon)
  return ((toDeg(Math.atan2(y, x)) + 360) % 360)
}

export function useAnchorWatch(
  currentLat: number | null,
  currentLon: number | null,
  navigationState: string | null,
  refreshInterval: number,
): AnchorWatchResult {
  const [serverState, setServerState] = useState<AnchorWatchServerState>({ active: false })
  const timerRef = useRef<ReturnType<typeof setInterval> | null>(null)

  const fetchState = useCallback(async () => {
    try {
      const res = await fetch('/api/anchor-watch')
      if (!res.ok) return
      const data = (await res.json()) as AnchorWatchServerState
      setServerState(data)
    } catch {
      // silently retain last known state
    }
  }, [])

  useEffect(() => {
    void fetchState()
    timerRef.current = setInterval(() => { void fetchState() }, refreshInterval * 1000)
    return () => {
      if (timerRef.current !== null) clearInterval(timerRef.current)
    }
  }, [fetchState, refreshInterval])

  const setAnchorHere = useCallback(async (lat: number, lon: number, radiusMeters?: number) => {
    const res = await fetch('/api/anchor-watch', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ lat, lon, radius_meters: radiusMeters ?? DEFAULT_RADIUS_METERS }),
    })
    if (res.ok) {
      const data = (await res.json()) as AnchorWatchServerState
      setServerState(data)
    }
  }, [])

  const updateRadius = useCallback(async (radiusMeters: number) => {
    const res = await fetch('/api/anchor-watch', {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ radius_meters: radiusMeters }),
    })
    if (res.ok) {
      const data = (await res.json()) as AnchorWatchServerState
      setServerState(data)
    }
  }, [])

  const updateRodeAndConditions = useCallback(async (
    rodeDeployedM: number,
    seaState: SeaState,
    seabedType: SeabedType,
  ) => {
    const res = await fetch('/api/anchor-watch', {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        rode_deployed_m: rodeDeployedM,
        sea_state: seaState,
        seabed_type: seabedType,
      }),
    })
    if (res.ok) {
      const data = (await res.json()) as AnchorWatchServerState
      setServerState(data)
    }
  }, [])

  const clearAnchor = useCallback(async () => {
    const res = await fetch('/api/anchor-watch', { method: 'DELETE' })
    if (res.ok) {
      setServerState({ active: false })
    }
  }, [])

  const anchorLat = serverState.active && serverState.lat !== undefined ? serverState.lat : null
  const anchorLon = serverState.active && serverState.lon !== undefined ? serverState.lon : null
  const radiusMeters = serverState.active && serverState.radius_meters !== undefined
    ? serverState.radius_meters
    : DEFAULT_RADIUS_METERS
  const rodeDeployedM = serverState.active && serverState.rode_deployed_m !== undefined && serverState.rode_deployed_m >= 0
    ? serverState.rode_deployed_m
    : 0
  const seaState = serverState.active && isSeaState(serverState.sea_state)
    ? serverState.sea_state
    : DEFAULT_SEA_STATE
  const seabedType = serverState.active && isSeabedType(serverState.seabed_type)
    ? serverState.seabed_type
    : DEFAULT_SEABED_TYPE

  let distanceMeters: number | null = null
  let bearingDeg: number | null = null
  if (anchorLat !== null && anchorLon !== null && currentLat !== null && currentLon !== null) {
    distanceMeters = haversineMeters(anchorLat, anchorLon, currentLat, currentLon)
    bearingDeg = Math.round(bearingDegrees(anchorLat, anchorLon, currentLat, currentLon))
  }

  let anchorState: AnchorWatchState = 'none'
  if (serverState.active) {
    if (distanceMeters !== null && distanceMeters > radiusMeters + DRAG_BUFFER_METERS) {
      anchorState = 'dragging'
    } else {
      anchorState = 'set'
    }
  }

  const suggestSet = !serverState.active && navigationState === 'anchored'

  return {
    anchorState,
    anchorLat,
    anchorLon,
    radiusMeters,
    rodeDeployedM,
    seaState,
    seabedType,
    distanceMeters,
    bearingDeg,
    suggestSet,
    setAnchorHere,
    updateRadius,
    updateRodeAndConditions,
    clearAnchor,
  }
}

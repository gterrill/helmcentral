import { useCallback, useEffect, useRef, useState } from 'react'
import { haversineMeters, bearingDeg as bearingDegrees } from '@/lib/geo'
import type { SeabedType, SeaState } from '@/lib/catenary'

export type AnchorWatchState = 'none' | 'set' | 'dragging' | 'critical'

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
  setAt: string | null
  setAnchorHere: (lat: number, lon: number, radiusMeters?: number) => Promise<void>
  updatePosition: (lat: number, lon: number) => Promise<void>
  updateRadius: (radiusMeters: number) => Promise<void>
  updateRodeAndConditions: (rodeDeployedM: number, seaState: SeaState, seabedType: SeabedType) => Promise<void>
  clearAnchor: () => Promise<void>
}

const DRAG_BUFFER_METERS = 4.572 // 15 ft
const DEFAULT_RADIUS_METERS = 20
const DEFAULT_SEA_STATE: SeaState = 'calm'
const DEFAULT_SEABED_TYPE: SeabedType = 'sand'

function isSeaState(value: string | undefined): value is SeaState {
  return value === 'calm' || value === 'choppy' || value === 'rough' || value === 'storm'
}

function isSeabedType(value: string | undefined): value is SeabedType {
  return value === 'sand' || value === 'mud' || value === 'rock' || value === 'grass'
}

export function useAnchorWatch(
  currentLat: number | null,
  currentLon: number | null,
  navigationState: string | null,
  refreshInterval: number,
  positionCritical = false,
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
    const payload: { lat: number; lon: number; radius_meters?: number } = { lat, lon }
    if (typeof radiusMeters === 'number' && radiusMeters > 0) {
      payload.radius_meters = radiusMeters
    }

    const res = await fetch('/api/anchor-watch', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload),
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

  const updatePosition = useCallback(async (lat: number, lon: number) => {
    const payload: { lat: number; lon: number; radius_meters?: number } = { lat, lon }
    if (typeof serverState.radius_meters === 'number' && serverState.radius_meters > 0) {
      payload.radius_meters = serverState.radius_meters
    }

    const res = await fetch('/api/anchor-watch', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload),
    })
    if (res.ok) {
      const data = (await res.json()) as AnchorWatchServerState
      setServerState(data)
    }
  }, [serverState.radius_meters])

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
    if (positionCritical) {
      anchorState = 'critical'
    } else if (distanceMeters !== null && distanceMeters > radiusMeters + DRAG_BUFFER_METERS) {
      anchorState = 'dragging'
    } else {
      anchorState = 'set'
    }
  }

  const suggestSet = !serverState.active && navigationState === 'anchored'
  const setAt = serverState.active && serverState.set_at ? serverState.set_at : null

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
    setAt,
    setAnchorHere,
    updatePosition,
    updateRadius,
    updateRodeAndConditions,
    clearAnchor,
  }
}

import { useCallback, useEffect, useRef, useState } from 'react'
import { haversineMeters, bearingDeg as bearingDegrees } from '@/lib/geo'
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
  bow_offset_m?: number
  bow_offset_applied?: boolean
  bow_offset_reason?: string
  planning_depth_m?: number
  planning_tide_height_ft?: number
}

export interface AnchorWatchResult {
  anchorState: AnchorWatchState
  gnssCritical: boolean
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
  bowOffsetM: number
  bowOffsetApplied: boolean
  bowOffsetReason: string
  /** The planning depth: seeded from the depth reading at the moment the
   * anchor was dropped, and editable by the operator from there — a
   * contemporaneous pair with the tide height, never re-derived from the
   * live tide station (ADR 0063). Null when nothing has been recorded
   * (legacy record, or no reading was available at the moment of drop). */
  planningDepthM: number | null
  planningTideHeightFt: number | null
  setAnchorHere: (
    lat: number,
    lon: number,
    capture: { planningDepthM: number | null; planningTideHeightFt: number | null; radiusMeters?: number },
  ) => Promise<void>
  updatePosition: (lat: number, lon: number) => Promise<void>
  updateRadius: (radiusMeters: number) => Promise<void>
  updateRodeAndConditions: (rodeDeployedM: number, seaState: SeaState, seabedType: SeabedType) => Promise<void>
  updatePlanningDepth: (depthM: number, tideHeightFt: number) => Promise<void>
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
  gnssCritical = false,
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

  const setAnchorHere = useCallback(async (
    lat: number,
    lon: number,
    capture: { planningDepthM: number | null; planningTideHeightFt: number | null; radiusMeters?: number },
  ) => {
    // Fed the live GPS fix, so the backend should apply the bow-offset
    // correction (projecting forward by gps_from_bow_m along heading) if
    // it's configured. updatePosition below is a user-dragged map point
    // that is already meant to be the anchor, so it deliberately omits this.
    //
    // planning_depth_m/planning_tide_height_ft always ride along, using the
    // -1 sentinel when the caller had nothing to capture (ADR 0063) — the
    // capture argument is required, not optional, so a caller can't silently
    // create a watch the planner has nothing to plan against.
    const payload: {
      lat: number
      lon: number
      radius_meters?: number
      apply_bow_offset: true
      planning_depth_m: number
      planning_tide_height_ft: number
    } = {
      lat,
      lon,
      apply_bow_offset: true,
      planning_depth_m: capture.planningDepthM ?? -1,
      planning_tide_height_ft: capture.planningTideHeightFt ?? -1,
    }
    if (typeof capture.radiusMeters === 'number' && capture.radiusMeters > 0) {
      payload.radius_meters = capture.radiusMeters
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

  const updatePlanningDepth = useCallback(async (depthM: number, tideHeightFt: number) => {
    // The planning depth pair is PATCHable — this is how the operator edits
    // the depth seeded at drop. Follows updateRodeAndConditions exactly:
    // await, replace state with the server echo, no optimistic update.
    const res = await fetch('/api/anchor-watch', {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        planning_depth_m: depthM,
        planning_tide_height_ft: tideHeightFt,
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

  // A corrupt/jammed GPS fix is surfaced via gnssCritical as a diagnostic,
  // not folded into anchorState - a brief bad fix (e.g. GPS resettling after
  // a laptop wakes from sleep) shouldn't sound the same alarm as an actual
  // drag, which is determined purely by distance from the anchor point.
  let anchorState: AnchorWatchState = 'none'
  if (serverState.active) {
    if (distanceMeters !== null && distanceMeters > radiusMeters + DRAG_BUFFER_METERS) {
      anchorState = 'dragging'
    } else {
      anchorState = 'set'
    }
  }

  const suggestSet = !serverState.active && navigationState === 'anchored'
  const setAt = serverState.active && serverState.set_at ? serverState.set_at : null
  const bowOffsetM = serverState.active && typeof serverState.bow_offset_m === 'number' ? serverState.bow_offset_m : 0
  const bowOffsetApplied = serverState.active ? Boolean(serverState.bow_offset_applied) : false
  const bowOffsetReason = serverState.active && typeof serverState.bow_offset_reason === 'string'
    ? serverState.bow_offset_reason
    : ''

  // Read predicate is `> 0` for depth and `>= 0` for tide, not `!== -1`
  // (ADR 0063) — a legacy anchor_watch.json with none of these fields decodes
  // them to 0, which must read as "not recorded", same as the -1 sentinel a
  // fresh POST/PATCH writes explicitly for "unset".
  const planningDepthM = serverState.active && typeof serverState.planning_depth_m === 'number' && serverState.planning_depth_m > 0
    ? serverState.planning_depth_m
    : null
  const planningTideHeightFt = serverState.active && typeof serverState.planning_tide_height_ft === 'number' && serverState.planning_tide_height_ft >= 0
    ? serverState.planning_tide_height_ft
    : null

  return {
    anchorState,
    gnssCritical,
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
    bowOffsetM,
    bowOffsetApplied,
    bowOffsetReason,
    planningDepthM,
    planningTideHeightFt,
    setAnchorHere,
    updatePosition,
    updateRadius,
    updateRodeAndConditions,
    updatePlanningDepth,
    clearAnchor,
  }
}

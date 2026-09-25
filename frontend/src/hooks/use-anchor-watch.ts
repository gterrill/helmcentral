import { useCallback, useEffect, useMemo, useState } from 'react'
import { haversineMeters, bearingDeg as bearingDegrees } from '@/lib/geo'
import type { SeabedType, SeaState } from '@/lib/catenary'
import { toast } from 'sonner'
import { anchorRequest } from '@/lib/anchor-request'
import { ANCHOR_WATCH_ACTIVE_REFRESH_SECONDS, ANCHOR_WATCH_IDLE_REFRESH_SECONDS } from '@/config/app-config'

export type AnchorWatchState = 'none' | 'set' | 'dragging'

interface AnchorWatchServerState {
  active: boolean
  // Set instead of lat/lon/radius_meters etc. when the persisted watch could
  // not be loaded (a damaged anchor_watch.json) — never alongside them, and
  // never invented lat/lon in that case. Names the file path and the parse
  // error (backend/anchor_watch_load_error.go).
  error?: string
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
  // ADR 0099: present whenever the server has ever auto-raised a watch
  // (in this process's lifetime), regardless of whether one is active now -
  // a successful auto-raise is exactly what makes `active` false again.
  last_auto_raise?: { at: string; reason: string }
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
  setAt: string | null
  /** True once the first GET /api/anchor-watch has resolved successfully.
   * Until then, `setAt: null` is ambiguous — it means either "no watch is
   * running" or "we haven't heard back yet" — and a caller that treats
   * those the same (e.g. discarding a stored map centre because "there's no
   * anchor") is acting on data it doesn't have. Only flips true on a
   * successful response; a failed or errored poll leaves it false and lets
   * the next poll retry, rather than masking the failure by treating
   * "attempted" as "confirmed". */
  loaded: boolean
  /** The persisted anchor watch couldn't be read back (a damaged
   * anchor_watch.json) — the backend's own explicit error state, naming the
   * file path and the parse error, never an invented or empty watch in its
   * place. Null in the ordinary "no watch is set" case, which also has
   * anchorState 'none' but carries no error. */
  error: string | null
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
  /** The server's most recent automatic raise (ADR 0099), if it has ever
   * raised one in this process's lifetime - independent of `active`/
   * `anchorState`, since a successful auto-raise is what makes the watch
   * inactive. App.tsx watches this for a change to show a one-time toast. */
  lastAutoRaise: { at: string; reason: string } | null
  setAnchorHere: (
    lat: number,
    lon: number,
    capture: { planningDepthM: number | null; planningTideHeightFt: number | null; radiusMeters?: number },
  ) => Promise<void>
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
  gnssCritical = false,
): AnchorWatchResult {
  const [serverState, setServerState] = useState<AnchorWatchServerState>({ active: false })
  // Only ever set true, and only on a genuine successful response — see the
  // `loaded` doc comment above for why a failed/errored poll must leave this
  // false rather than a fallback that reads "attempted" as "confirmed".
  const [loaded, setLoaded] = useState(false)

  const fetchState = useCallback(async () => {
    try {
      const res = await fetch('/api/anchor-watch')
      if (!res.ok) return
      const data = (await res.json()) as AnchorWatchServerState
      setServerState(data)
      setLoaded(true)
    } catch {
      // silently retain last known state
    }
  }, [])

  // Fetched once on mount, independent of the interval below, so a cadence
  // change (idle -> active or back) never doubles up on an extra immediate
  // fetch — see ANCHOR_WATCH_ACTIVE_REFRESH_SECONDS/ANCHOR_WATCH_IDLE_REFRESH_SECONDS
  // (config/app-config.ts) for why the two cadences differ.
  useEffect(() => {
    void fetchState()
  }, [fetchState])

  useEffect(() => {
    const intervalSeconds = serverState.active
      ? ANCHOR_WATCH_ACTIVE_REFRESH_SECONDS
      : ANCHOR_WATCH_IDLE_REFRESH_SECONDS
    const timer = setInterval(() => { void fetchState() }, intervalSeconds * 1000)
    return () => clearInterval(timer)
  }, [fetchState, serverState.active])

  const setAnchorHere = useCallback(async (
    lat: number,
    lon: number,
    capture: { planningDepthM: number | null; planningTideHeightFt: number | null; radiusMeters?: number },
  ) => {
    // Fed the live GPS fix, so the backend should apply the bow-offset
    // correction (projecting forward by gps_from_bow_m along heading) if
    // it's configured.
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

    try {
      const res = await anchorRequest({
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload),
      })
      setServerState(await res.json() as AnchorWatchServerState)
      // A successful mutation response is just as authoritative about "we
      // have heard from the server" as a GET — see `loaded`'s own doc
      // comment. Without this, dropping anchor before the first GET
      // resolves would leave `loaded` false despite a real, current server
      // state already sitting in hand.
      setLoaded(true)
    } catch (error) {
      toast.error('Could not drop anchor', { description: error instanceof Error ? error.message : 'Request failed' })
    }
  }, [])

  // These three PATCH mutations used to swallow a failed response (silent
  // no-op on !res.ok) — the P1 the impeccable critique of the anchor-watch
  // map raised against updateRadius applies just as much to the other two.
  // anchorRequest throws on a non-OK response or a network error, and
  // there's no catch here: on a throw, setServerState below never runs, so
  // state is left exactly as it was, and the rejection propagates to the
  // caller — the drawer's radius stepper, the rode planner's Apply-as-
  // alarm-radius path, and (for the other two) their own callers, all of
  // which are what actually shows the toast (they know the retry value and
  // want a Retry action, which this hook has no context to build).
  const updateRadius = useCallback(async (radiusMeters: number) => {
    const res = await anchorRequest({
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ radius_meters: radiusMeters }),
    })
    setServerState(await res.json() as AnchorWatchServerState)
    setLoaded(true)
  }, [])

  const updateRodeAndConditions = useCallback(async (
    rodeDeployedM: number,
    seaState: SeaState,
    seabedType: SeabedType,
  ) => {
    const res = await anchorRequest({
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        rode_deployed_m: rodeDeployedM,
        sea_state: seaState,
        seabed_type: seabedType,
      }),
    })
    setServerState(await res.json() as AnchorWatchServerState)
    setLoaded(true)
  }, [])

  const updatePlanningDepth = useCallback(async (depthM: number, tideHeightFt: number) => {
    // The planning depth pair is PATCHable — this is how the operator edits
    // the depth seeded at drop. Follows updateRodeAndConditions exactly:
    // await, replace state with the server echo, no optimistic update.
    const res = await anchorRequest({
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        planning_depth_m: depthM,
        planning_tide_height_ft: tideHeightFt,
      }),
    })
    setServerState(await res.json() as AnchorWatchServerState)
    setLoaded(true)
  }, [])

  const clearAnchor = useCallback(async () => {
    try {
      await anchorRequest({ method: 'DELETE' })
      setServerState({ active: false })
      setLoaded(true)
    } catch (error) {
      toast.error('Could not raise anchor', { description: error instanceof Error ? error.message : 'Request failed' })
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

  // Not gated on serverState.active — it never is true alongside an error —
  // but explicitly typeof-checked so a stray non-string value from a future
  // backend change can't leak through as a truthy, unrenderable object.
  const error = typeof serverState.error === 'string' ? serverState.error : null

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

  // Not gated on serverState.active, unlike every field above: a successful
  // auto-raise is exactly what flips active to false, so gating this the
  // same way would make the one case it exists for unreachable.
  const lastAutoRaise = serverState.last_auto_raise ?? null

  // Memoized: latitude/longitude arrive over the 1Hz vessel-state SSE stream
  // and every tick re-renders App and therefore re-runs this hook, so without
  // this every consumer (the anchor-watch tile, its fullscreen drawer) would
  // see a new object identity every second even while every field below is
  // unchanged — defeating their own React.memo. The dependency list is the
  // output values themselves, not the raw inputs, so a tick that leaves every
  // one of them the same (e.g. a fix arriving with the same coordinates, or
  // one that moves the vessel by less than distanceMeters/bearingDeg's own
  // rounding) still returns the previous reference.
  return useMemo(() => ({
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
    setAt,
    loaded,
    error,
    bowOffsetM,
    bowOffsetApplied,
    bowOffsetReason,
    planningDepthM,
    planningTideHeightFt,
    lastAutoRaise,
    setAnchorHere,
    updateRadius,
    updateRodeAndConditions,
    updatePlanningDepth,
    clearAnchor,
  }), [
    anchorState, gnssCritical, anchorLat, anchorLon, radiusMeters, rodeDeployedM,
    seaState, seabedType, distanceMeters, bearingDeg, setAt, loaded, error, bowOffsetM, lastAutoRaise,
    bowOffsetApplied, bowOffsetReason, planningDepthM, planningTideHeightFt,
    setAnchorHere, updateRadius, updateRodeAndConditions,
    updatePlanningDepth, clearAnchor,
  ])
}

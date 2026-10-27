import { useCallback, useMemo } from 'react'
import { haversineMeters, bearingDeg as bearingDegrees } from '@/lib/geo'
import type { SeabedType, SeaState } from '@/lib/catenary'
import { toast } from 'sonner'
import { apiRequest, declareQuery, setQueryData, useApiQuerySelector } from '@/lib/api-query'
import type { AnchorAdjustRestore } from '@/lib/anchor-adjust'
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
  // -1 when not read, matching the backend's own "not captured" sentinel
  // (backend/anchor.go's anchorWatchData.HeadingAtSetDeg) - feeds Adjust's
  // own Undo restore payload (anchor-watch-drawer.tsx's adjustOpenedFromRef),
  // which needs the watch's own facts, not just the three bow-offset fields
  // above.
  heading_at_set_deg?: number
  // Empty until the background resolver pins one (place_name.go) - same
  // Undo-restore use as heading_at_set_deg above.
  place_name?: string
  planning_depth_m?: number
  planning_tide_height_ft?: number
  // ADR 0099: present whenever the server has ever auto-raised a watch
  // (in this process's lifetime), regardless of whether one is active now -
  // a successful auto-raise is exactly what makes `active` false again.
  last_auto_raise?: { at: string; reason: string }
}

/**
 * The anchor watch record as the server keeps it: no live fix in it. The
 * anchor watch tile and page read it (ADR 0215) and apply their own fix with
 * anchorWatchAt; AppShell reads only the slice it needs through
 * useAnchorWatchSelector.
 */
export interface AnchorWatchRecord {
  /** Whether a watch is set. */
  active: boolean
  anchorLat: number | null
  anchorLon: number | null
  radiusMeters: number
  rodeDeployedM: number
  seaState: SeaState
  seabedType: SeabedType
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
   * active false but carries no error. */
  error: string | null
  bowOffsetM: number
  bowOffsetApplied: boolean
  bowOffsetReason: string
  /** -1 when not read. Feeds Adjust's own Undo restore payload alongside the three bowOffset* fields above (code-review finding, round 2). */
  headingAtSetDeg: number
  /** Empty until the background resolver pins one. Same Undo-restore use as headingAtSetDeg. */
  placeName: string
  /** The planning depth: seeded from the depth reading at the moment the
   * anchor was dropped, and editable by the operator from there — a
   * contemporaneous pair with the tide height, never re-derived from the
   * live tide station (ADR 0063). Null when nothing has been recorded
   * (legacy record, or no reading was available at the moment of drop). */
  planningDepthM: number | null
  planningTideHeightFt: number | null
  /** The server's most recent automatic raise (ADR 0099), if it has ever
   * raised one in this process's lifetime - independent of `active`,
   * since a successful auto-raise is what makes the watch inactive. App.tsx watches this for a change to show a one-time toast. */
  lastAutoRaise: { at: string; reason: string } | null
  setAnchorHere: (
    lat: number,
    lon: number,
    capture: { planningDepthM: number | null; planningTideHeightFt: number | null; radiusMeters?: number },
  ) => Promise<void>
  updateRadius: (radiusMeters: number) => Promise<void>
  /**
   * The Adjust mode write: radius alone, or radius plus position, in one
   * atomic PATCH, mirroring the backend's own lat+lon+radius_meters contract
   * (backend/anchor.go's patchAnchorWatch — lat/lon optional but must arrive
   * together). lat/lon are optional here too, both or neither: the caller
   * (the drawer's handleAdjustSet, via lib/anchor-adjust.ts's
   * buildAdjustCommitTargets) omits them entirely when the draft position
   * wasn't actually moved, so a radius-only Set doesn't ask the backend to
   * treat it as a reposition (self-trail reset, a fresh SignalK publish that
   * 502s if SignalK happens to be down — code-review finding). Mirrors
   * updateRadius exactly otherwise — awaits apiRequest, replaces state
   * with the server echo, rethrows on failure rather than swallowing it, so
   * the caller (the Adjust mode's Set button) is what shows a Retry toast.
   *
   * `restore` (code-review finding, round 2): forwarded verbatim as the
   * PATCH body's own `restore` object when the caller provides one - only
   * ever Undo does (buildAdjustCommitTargets' own `undo` target), carrying
   * the committed point's pre-Adjust bow-offset/heading/place-name facts so
   * the backend puts them back instead of stamping "placed by hand in
   * Adjust" over a position Undo is putting back exactly where it was.
   */
  adjustAnchor: (params: { lat?: number; lon?: number; radiusMeters: number; restore?: AnchorAdjustRestore }) => Promise<void>
  updateRodeAndConditions: (rodeDeployedM: number, seaState: SeaState, seabedType: SeabedType) => Promise<void>
  updatePlanningDepth: (depthM: number, tideHeightFt: number) => Promise<void>
  clearAnchor: () => Promise<void>
}

/** The record at one fix: where the boat is against the anchor, and whether that is a drag. */
export interface AnchorWatchResult extends AnchorWatchRecord {
  anchorState: AnchorWatchState
  gnssCritical: boolean
  distanceMeters: number | null
  bearingDeg: number | null
}

// Exported for the Adjust mode's own drag-alarm-preview warning ("Alarm
// would sound now" when the draft radius would leave the boat outside it) —
// part 2 of the anchor-adjust-sheet plan, which needs the exact same buffer
// this hook's own anchorState computation below uses, not a second copy of it.
export const DRAG_BUFFER_METERS = 4.572 // 15 ft
const DEFAULT_RADIUS_METERS = 20
const DEFAULT_SEA_STATE: SeaState = 'calm'
const DEFAULT_SEABED_TYPE: SeabedType = 'sand'

const ANCHOR_WATCH_KEY = ['/api/anchor-watch'] as const
const INACTIVE: AnchorWatchServerState = { active: false }

// A successful mutation response is just as authoritative about "we have
// heard from the server" as a GET (see `loaded`), so it lands in the cache.
function land(state: AnchorWatchServerState) {
  setQueryData<AnchorWatchServerState>(ANCHOR_WATCH_KEY, state)
}

function isSeaState(value: string | undefined): value is SeaState {
  return value === 'calm' || value === 'choppy' || value === 'rough' || value === 'storm'
}

function isSeabedType(value: string | undefined): value is SeabedType {
  return value === 'sand' || value === 'mud' || value === 'rock' || value === 'grass'
}

/**
 * Where the boat is against the anchor: distance, bearing from the anchor,
 * and whether that is a drag. Null distance and bearing without a fix or an
 * anchor.
 *
 * A corrupt/jammed GPS fix is surfaced via gnssCritical as a diagnostic, not
 * folded into anchorState - a brief bad fix (e.g. GPS resettling after a
 * laptop wakes from sleep) shouldn't sound the same alarm as an actual drag,
 * which is determined purely by distance from the anchor point.
 */
function positionAgainstAnchor(
  active: boolean,
  anchorLat: number | null,
  anchorLon: number | null,
  radiusMeters: number,
  currentLat: number | null,
  currentLon: number | null,
): { distanceMeters: number | null; bearingDeg: number | null; anchorState: AnchorWatchState } {
  let distanceMeters: number | null = null
  let bearingDeg: number | null = null
  if (anchorLat !== null && anchorLon !== null && currentLat !== null && currentLon !== null) {
    distanceMeters = haversineMeters(anchorLat, anchorLon, currentLat, currentLon)
    bearingDeg = Math.round(bearingDegrees(anchorLat, anchorLon, currentLat, currentLon))
  }
  let anchorState: AnchorWatchState = 'none'
  if (active) {
    anchorState = distanceMeters !== null && distanceMeters > radiusMeters + DRAG_BUFFER_METERS ? 'dragging' : 'set'
  }
  return { distanceMeters, bearingDeg, anchorState }
}

/**
 * The watch as read at a given fix: the record from useAnchorWatch with its
 * distance, bearing, drag state and GNSS flag worked out for this position.
 * This is the only place a fix is applied to the record. AppShell reads the
 * record alone (ADR 0215), so a fix moving does not re-render the shell; the
 * anchor watch tile and page apply the live fix with this.
 */
export function anchorWatchAt(
  watch: AnchorWatchRecord,
  currentLat: number | null,
  currentLon: number | null,
  gnssCritical: boolean,
): AnchorWatchResult {
  const derived = positionAgainstAnchor(
    watch.active, watch.anchorLat, watch.anchorLon, watch.radiusMeters, currentLat, currentLon,
  )
  return { ...watch, ...derived, gnssCritical }
}

/** The record's fields without its actions: what the server said, parsed. */
export type AnchorWatchFields = Omit<AnchorWatchRecord,
  'setAnchorHere' | 'updateRadius' | 'adjustAnchor' | 'updateRodeAndConditions' | 'updatePlanningDepth' | 'clearAnchor'>

/**
 * One GET /api/anchor-watch answer, as every reader sees it. Undefined (no
 * answer yet, or none that landed) reads as no watch, with `loaded` false.
 */
export function parseAnchorWatch(data: AnchorWatchServerState | undefined): AnchorWatchFields {
  const serverState = data ?? INACTIVE
  const on = serverState.active
  return {
    active: on,
    anchorLat: on && serverState.lat !== undefined ? serverState.lat : null,
    anchorLon: on && serverState.lon !== undefined ? serverState.lon : null,
    radiusMeters: on && serverState.radius_meters !== undefined ? serverState.radius_meters : DEFAULT_RADIUS_METERS,
    rodeDeployedM: on && serverState.rode_deployed_m !== undefined && serverState.rode_deployed_m >= 0 ? serverState.rode_deployed_m : 0,
    seaState: on && isSeaState(serverState.sea_state) ? serverState.sea_state : DEFAULT_SEA_STATE,
    seabedType: on && isSeabedType(serverState.seabed_type) ? serverState.seabed_type : DEFAULT_SEABED_TYPE,
    setAt: on && serverState.set_at ? serverState.set_at : null,
    // `loaded` stays false until a response has landed: a failed or errored
    // poll leaves no data, so "attempted" is never read as "confirmed". Once
    // data exists, a later failed poll keeps the last known state.
    loaded: data !== undefined,
    // Not gated on active (it never is true alongside an error), but
    // typeof-checked so a stray non-string value from a future backend change
    // can't leak through as a truthy, unrenderable object.
    error: typeof serverState.error === 'string' ? serverState.error : null,
    bowOffsetM: on && typeof serverState.bow_offset_m === 'number' ? serverState.bow_offset_m : 0,
    bowOffsetApplied: on ? Boolean(serverState.bow_offset_applied) : false,
    bowOffsetReason: on && typeof serverState.bow_offset_reason === 'string' ? serverState.bow_offset_reason : '',
    headingAtSetDeg: on && typeof serverState.heading_at_set_deg === 'number' ? serverState.heading_at_set_deg : -1,
    placeName: on && typeof serverState.place_name === 'string' ? serverState.place_name : '',
    // `> 0` for depth and `>= 0` for tide, not `!== -1` (ADR 0063): a legacy
    // anchor_watch.json with none of these fields decodes them to 0, which
    // must read as "not recorded", same as the -1 sentinel a fresh POST/PATCH
    // writes explicitly for "unset".
    planningDepthM: on && typeof serverState.planning_depth_m === 'number' && serverState.planning_depth_m > 0
      ? serverState.planning_depth_m
      : null,
    planningTideHeightFt: on && typeof serverState.planning_tide_height_ft === 'number' && serverState.planning_tide_height_ft >= 0
      ? serverState.planning_tide_height_ft
      : null,
    // Not gated on active, unlike every field above: a successful auto-raise
    // is exactly what flips active to false, so gating this the same way
    // would make the one case it exists for unreachable.
    lastAutoRaise: serverState.last_auto_raise ?? null,
  }
}

/** Fields equal by value; the auto-raise by its time and reason, since every poll parses a new object for it. */
export function sameAnchorWatchFields(a: AnchorWatchFields, b: AnchorWatchFields): boolean {
  return (Object.keys(a) as Array<keyof AnchorWatchFields>).every((key) => {
    if (key === 'lastAutoRaise') {
      return a.lastAutoRaise === b.lastAutoRaise
        || (a.lastAutoRaise !== null && b.lastAutoRaise !== null
          && a.lastAutoRaise.at === b.lastAutoRaise.at && a.lastAutoRaise.reason === b.lastAutoRaise.reason)
    }
    return Object.is(a[key], b[key])
  })
}

// ── the writes ───────────────────────────────────────────────────────────────
//
// Plain module functions: each lands the server's answer in the shared query
// entry, so every reader of the watch hears it, and none closes over anything,
// so they are stable without useCallback.

/**
 * Drops the anchor at a fix. Fed the live GPS fix, so the backend should apply
 * the bow-offset correction (projecting forward by gps_from_bow_m along
 * heading) if it's configured.
 *
 * planning_depth_m/planning_tide_height_ft always ride along, using the -1
 * sentinel when the caller had nothing to capture (ADR 0063). The capture
 * argument is required, not optional, so a caller can't silently create a
 * watch the planner has nothing to plan against.
 */
export async function setAnchorHere(
  lat: number,
  lon: number,
  capture: { planningDepthM: number | null; planningTideHeightFt: number | null; radiusMeters?: number },
): Promise<void> {
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
    // Landing the response also marks `loaded`: dropping anchor before the
    // first GET resolves would otherwise leave it false despite a real,
    // current server state already in hand.
    land(await apiRequest<AnchorWatchServerState>('/api/anchor-watch', { method: 'POST', json: payload }))
  } catch (error) {
    toast.error('Could not drop anchor', { description: error instanceof Error ? error.message : 'Request failed' })
  }
}

// These PATCH writes used to swallow a failed response (silent no-op on
// !res.ok), the P1 the impeccable critique of the anchor-watch map raised
// against updateRadius. apiRequest throws on a non-OK response or a network
// error, and there's no catch here: on a throw, land() never runs, so state is
// left exactly as it was, and the rejection propagates to the caller. The
// drawer's radius stepper, the rode planner's Apply-as-alarm-radius path and
// the other callers are what show the toast: they know the retry value and
// want a Retry action, which this module has no context to build.

export async function updateAnchorRadius(radiusMeters: number): Promise<void> {
  land(await apiRequest<AnchorWatchServerState>('/api/anchor-watch', { method: 'PATCH', json: { radius_meters: radiusMeters } }))
}

export async function adjustAnchor(
  { lat, lon, radiusMeters, restore }: { lat?: number; lon?: number; radiusMeters: number; restore?: AnchorAdjustRestore },
): Promise<void> {
  // lat/lon travel together or not at all: the caller (buildAdjustCommitTargets)
  // already decides that; this just forwards whatever it built rather than
  // re-deciding, so a radius-only target really does PATCH radius_meters alone.
  const body: {
    radius_meters: number
    lat?: number
    lon?: number
    restore?: {
      bow_offset_m: number
      bow_offset_applied: boolean
      bow_offset_reason: string
      heading_at_set_deg: number
      place_name: string
    }
  } = { radius_meters: radiusMeters }
  if (lat !== undefined && lon !== undefined) {
    body.lat = lat
    body.lon = lon
  }
  // Only ever present alongside lat/lon (buildAdjustCommitTargets never sets
  // it otherwise), matching the backend's own "restore requires a position
  // change" rule.
  if (restore !== undefined) {
    body.restore = {
      bow_offset_m: restore.bowOffsetM,
      bow_offset_applied: restore.bowOffsetApplied,
      bow_offset_reason: restore.bowOffsetReason,
      heading_at_set_deg: restore.headingAtSetDeg,
      place_name: restore.placeName,
    }
  }
  land(await apiRequest<AnchorWatchServerState>('/api/anchor-watch', { method: 'PATCH', json: body }))
}

export async function updateRodeAndConditions(rodeDeployedM: number, seaState: SeaState, seabedType: SeabedType): Promise<void> {
  land(await apiRequest<AnchorWatchServerState>('/api/anchor-watch', { method: 'PATCH', json: {
    rode_deployed_m: rodeDeployedM,
    sea_state: seaState,
    seabed_type: seabedType,
  } }))
}

/**
 * The planning depth pair is PATCHable: this is how the operator edits the
 * depth seeded at drop. Await, replace state with the server echo, no
 * optimistic update.
 */
export async function updatePlanningDepth(depthM: number, tideHeightFt: number): Promise<void> {
  land(await apiRequest<AnchorWatchServerState>('/api/anchor-watch', { method: 'PATCH', json: {
    planning_depth_m: depthM,
    planning_tide_height_ft: tideHeightFt,
  } }))
}

export async function clearAnchor(): Promise<void> {
  try {
    await apiRequest<void>('/api/anchor-watch', { method: 'DELETE' })
    land(INACTIVE)
  } catch (error) {
    toast.error('Could not raise anchor', { description: error instanceof Error ? error.message : 'Request failed' })
  }
}

// ── the reads ────────────────────────────────────────────────────────────────

// The record is parsed once per answer, here, and every reader selects from
// the parsed fields: with a dozen readers a poll parses once, not a dozen times.
declareQuery(ANCHOR_WATCH_KEY, { parse: parseAnchorWatch })

const selectActive = (fields: AnchorWatchFields | undefined) => fields?.active === true

/**
 * One slice of the watch record, polled from the server. The reader
 * re-renders only when `isEqual` calls the slice different, not on every
 * poll: AppShell reads three fields this way (ADR 0215), so a poll that
 * changes the radius or the rode re-renders the anchor watch tile and page,
 * not the shell. Pass a stable `select`.
 */
export function useAnchorWatchSelector<T>(
  select: (fields: AnchorWatchFields) => T,
  isEqual: (a: T, b: T) => boolean = Object.is,
): T {
  // The poll cadence follows the watch (see ANCHOR_WATCH_ACTIVE_REFRESH_SECONDS/
  // ANCHOR_WATCH_IDLE_REFRESH_SECONDS in config/app-config.ts for why the two
  // differ), so the interval is asked for by a second reader of the same
  // entry once the first has said whether a watch is active. The two share
  // one request.
  const active = useApiQuerySelector<AnchorWatchFields, boolean>({ queryKey: ANCHOR_WATCH_KEY, path: '/api/anchor-watch' }, selectActive)
  const selectFromParsed = useCallback((fields: AnchorWatchFields | undefined) => select(fields as AnchorWatchFields), [select])
  return useApiQuerySelector<AnchorWatchFields, T>({
    queryKey: ANCHOR_WATCH_KEY,
    path: '/api/anchor-watch',
    refetchInterval: (active ? ANCHOR_WATCH_ACTIVE_REFRESH_SECONDS : ANCHOR_WATCH_IDLE_REFRESH_SECONDS) * 1000,
  }, selectFromParsed, isEqual)
}

const selectFields = (fields: AnchorWatchFields) => fields

const ACTIONS = {
  setAnchorHere,
  updateRadius: updateAnchorRadius,
  adjustAnchor,
  updateRodeAndConditions,
  updatePlanningDepth,
  clearAnchor,
}

/**
 * The whole watch record and its writes, for the anchor watch tile and page.
 * Keeps its object while a poll changes nothing, so a memoized map below it
 * does not redraw. Apply a fix with anchorWatchAt.
 */
export function useAnchorWatch(): AnchorWatchRecord {
  const fields = useAnchorWatchSelector(selectFields, sameAnchorWatchFields)
  return useMemo(() => ({ ...fields, ...ACTIONS }), [fields])
}

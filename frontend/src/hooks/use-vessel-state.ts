import { useCallback } from 'react'

import { ageFromPayload, shownAge } from '@/lib/staleness'

import { GUST_WINDOWS, type GustWindow } from '@/lib/gust-windows'
import { subscribeTelemetry } from '@/hooks/use-telemetry-stream'
import { createTelemetryStore, type SubscribeTelemetry, type TelemetryUpdate } from '@/lib/telemetry-store'

interface VesselState {
  status: string
  datetime: string
  depth: number
  length_overall_m: number | null
  draft_m: number | null
  current_drift_kts: number
  current_set_deg: number
  current_drift_impact_kts: number | null
  latitude: number
  longitude: number
  gnss_quality_indicator: number
  gnss_hdop: number
  gnss_satellites: number
  gnss_validation_state: string
  gnss_validation_reason: string
  gnss_critical_alert: boolean
  heading_true: number
  speed_over_ground_kts: number
  wind_speed_apparent_kts: number
  wind_angle_apparent_deg: number
  wind_side: string
  wind_angle_relative_deg: number
  wind_speed_true_kts: number
  wind_angle_true_deg: number
  wind_side_true: string
  wind_angle_true_relative_deg: number
  wind_direction_true_deg: number
  max_gust_kts: Record<string, number>
  max_gust_true_kts: Record<string, number>
  generator_state: string
  generator_manual_start: boolean
  generator_manual_start_timer: number
  generator_running_by_condition: string
  generator_runtime: number
  /** null when no engine is set up in the roles or none is reporting. */
  engines_running: boolean | null
  depth_last_update_age_s: number
  position_last_update_age_s: number
  wind_last_update_age_s: number
  source: string
}

type GustLadder = Record<GustWindow, number | null>
type Side = 'port' | 'starboard' | null

/** The vessel state as the tiles read it: parsed, with absent fields as null. */
export interface VesselStateValue {
  depth: number | null
  // Per-source freshness (ADR 0068). Null means the source publishes no
  // timestamp, which reads as 'not stale' rather than as fresh.
  depthLastUpdateAgeS: number | null
  positionLastUpdateAgeS: number | null
  windLastUpdateAgeS: number | null
  // The Rode Planner's swing radius fallback source when settings.anchor.loa_m
  // is unset (ADR 0047). Backend already nils this out when unpublished, so
  // no >= 0 guard is needed here, unlike the sentinel-bearing fields below.
  vesselLengthOverallM: number | null
  // The Anchor Watch low-water clearance warning's draft input (ADR 0135).
  // Backend already nils this out when unpublished, same as above.
  vesselDraftM: number | null
  currentDriftKts: number | null
  currentSetDeg: number | null
  currentDriftImpactKts: number | null
  navigationState: string | null
  latitude: number | null
  longitude: number | null
  gnssQualityIndicator: number | null
  gnssHdop: number | null
  gnssSatellites: number | null
  gnssValidationState: string | null
  gnssValidationReason: string | null
  gnssCriticalAlert: boolean
  headingTrue: number | null
  speedOverGroundKts: number | null
  windSpeedApparentKts: number | null
  windAngleApparentDeg: number | null
  windSide: Side
  windAngleRelativeDeg: number | null
  // True wind (ADR 0129 / ADR 0130) — parsed the same -1/null-is-absent way
  // as the apparent fields above, but never derived from them: a boat with
  // no true-wind source reads null here even while apparent is present
  // (no fallback — see AGENTS.md's fallback policy). Feeds the Current
  // Conditions tile (speed/direction, and the "obs" marker, which reads
  // maxGustTrueKts['1h']) and the Wind tile's True mode.
  windSpeedTrueKts: number | null
  windAngleTrueDeg: number | null
  windSideTrue: Side
  windAngleTrueRelativeDeg: number | null
  windDirectionTrueDeg: number | null
  maxGustKts: GustLadder
  // One ladder read by both the Wind tile in True mode and, through its '1h'
  // entry, the Current Conditions tile's "obs" marker. The backend used to
  // send a separate max_true_wind_kts_1h from a different buffer, so the two
  // tiles could show two "last hour" figures at once (see docs/adr/0130's
  // amendment).
  maxGustTrueKts: GustLadder
  generatorState: string | null
  generatorManualStart: boolean
  generatorManualStartTimer: number
  generatorRunningByCondition: string | null
  generatorRuntime: number | null
  /** null: no engine is set up in the roles, or none is reporting. */
  enginesRunning: boolean | null
  // Which source the backend answered from ("signalk", "signalk-unreachable",
  // "backend-fallback"). Surfaced so the app can offer to go looking for a
  // server that has moved, rather than leaving the operator with silent tiles.
  source: string | null
}

export type VesselStateField = keyof VesselStateValue

const EMPTY_LADDER: GustLadder = Object.fromEntries(GUST_WINDOWS.map((window) => [window, null])) as GustLadder

/** Every field unknown: the state before the first vessel-state event. */
export const INITIAL_VESSEL_STATE: VesselStateValue = {
  depth: null,
  depthLastUpdateAgeS: null,
  positionLastUpdateAgeS: null,
  windLastUpdateAgeS: null,
  vesselLengthOverallM: null,
  vesselDraftM: null,
  currentDriftKts: null,
  currentSetDeg: null,
  currentDriftImpactKts: null,
  navigationState: null,
  latitude: null,
  longitude: null,
  gnssQualityIndicator: null,
  gnssHdop: null,
  gnssSatellites: null,
  gnssValidationState: null,
  gnssValidationReason: null,
  gnssCriticalAlert: false,
  headingTrue: null,
  speedOverGroundKts: null,
  windSpeedApparentKts: null,
  windAngleApparentDeg: null,
  windSide: null,
  windAngleRelativeDeg: null,
  windSpeedTrueKts: null,
  windAngleTrueDeg: null,
  windSideTrue: null,
  windAngleTrueRelativeDeg: null,
  windDirectionTrueDeg: null,
  maxGustKts: EMPTY_LADDER,
  maxGustTrueKts: EMPTY_LADDER,
  generatorState: null,
  generatorManualStart: false,
  generatorManualStartTimer: 0,
  generatorRunningByCondition: null,
  generatorRuntime: null,
  enginesRunning: null,
  source: null,
}

const AGE_FIELDS: readonly VesselStateField[] = ['depthLastUpdateAgeS', 'positionLastUpdateAgeS', 'windLastUpdateAgeS']
const LADDER_FIELDS: readonly VesselStateField[] = ['maxGustKts', 'maxGustTrueKts']
const FIELDS = Object.keys(INITIAL_VESSEL_STATE) as VesselStateField[]

const nonNegative = (value: unknown): number | null => (typeof value === 'number' && value >= 0 ? value : null)
const numberOrNull = (value: unknown): number | null => (typeof value === 'number' ? value : null)
const textOrNull = (value: unknown): string | null => (typeof value === 'string' && value !== '' ? value : null)
const sideOrNull = (value: unknown): Side => (value === 'port' || value === 'starboard' ? value : null)

function ladder(raw: Record<string, number> | undefined): GustLadder {
  return Object.fromEntries(GUST_WINDOWS.map((window) => [window, nonNegative(raw?.[window])])) as GustLadder
}

/** One vessel-state event, parsed the way every tile reads it. -1 and absent both read as null. */
function parseVesselState(data: VesselState): VesselStateValue {
  return {
    depth: nonNegative(data.depth),
    vesselLengthOverallM: numberOrNull(data.length_overall_m),
    vesselDraftM: numberOrNull(data.draft_m),
    currentDriftKts: nonNegative(data.current_drift_kts),
    currentSetDeg: nonNegative(data.current_set_deg),
    currentDriftImpactKts: numberOrNull(data.current_drift_impact_kts),
    navigationState: textOrNull(data.status),
    source: textOrNull(data.source),
    latitude: typeof data.latitude === 'number' && data.latitude >= -90 && data.latitude <= 90 ? data.latitude : null,
    longitude: typeof data.longitude === 'number' && data.longitude >= -180 && data.longitude <= 180 ? data.longitude : null,
    gnssQualityIndicator: nonNegative(data.gnss_quality_indicator),
    gnssHdop: nonNegative(data.gnss_hdop),
    gnssSatellites: nonNegative(data.gnss_satellites),
    gnssValidationState: textOrNull(data.gnss_validation_state),
    gnssValidationReason: textOrNull(data.gnss_validation_reason),
    gnssCriticalAlert: data.gnss_critical_alert === true,
    headingTrue: nonNegative(data.heading_true),
    depthLastUpdateAgeS: ageFromPayload(data.depth_last_update_age_s),
    positionLastUpdateAgeS: ageFromPayload(data.position_last_update_age_s),
    windLastUpdateAgeS: ageFromPayload(data.wind_last_update_age_s),
    windSpeedApparentKts: nonNegative(data.wind_speed_apparent_kts),
    windAngleApparentDeg: nonNegative(data.wind_angle_apparent_deg),
    windSide: sideOrNull(data.wind_side),
    windAngleRelativeDeg: nonNegative(data.wind_angle_relative_deg),
    windSpeedTrueKts: nonNegative(data.wind_speed_true_kts),
    windAngleTrueDeg: nonNegative(data.wind_angle_true_deg),
    windSideTrue: sideOrNull(data.wind_side_true),
    windAngleTrueRelativeDeg: nonNegative(data.wind_angle_true_relative_deg),
    windDirectionTrueDeg: nonNegative(data.wind_direction_true_deg),
    maxGustKts: ladder(data.max_gust_kts),
    maxGustTrueKts: ladder(data.max_gust_true_kts),
    speedOverGroundKts: nonNegative(data.speed_over_ground_kts),
    generatorState: textOrNull(data.generator_state),
    generatorManualStart: data.generator_manual_start === true,
    generatorManualStartTimer: typeof data.generator_manual_start_timer === 'number' ? data.generator_manual_start_timer : 0,
    generatorRunningByCondition: textOrNull(data.generator_running_by_condition),
    generatorRuntime: nonNegative(data.generator_runtime),
    enginesRunning: typeof data.engines_running === 'boolean' ? data.engines_running : null,
  }
}

/** Whether a field shows the same: ages by what a tile shows for them, gust ladders window by window. */
function sameField(field: VesselStateField, a: VesselStateValue[VesselStateField], b: VesselStateValue[VesselStateField]): boolean {
  if (AGE_FIELDS.includes(field)) return shownAge(a as number | null) === shownAge(b as number | null)
  if (LADDER_FIELDS.includes(field)) {
    const left = a as GustLadder
    const right = b as GustLadder
    return GUST_WINDOWS.every((window) => left[window] === right[window])
  }
  return a === b
}

/*
 * One store for the `vessel-state` event (ADR 0215; lib/telemetry-store.ts), the same
 * shape as use-gauge-values.ts and use-alarms.ts: one subscription and one
 * parse per tick, however many tiles read it. Each tile selects the fields it
 * shows, so a tick re-renders only the tiles whose own fields changed.
 *
 * A field that shows the same as before keeps its previous value, and a tick
 * in which every field shows the same keeps the snapshot and notifies nobody.
 * The server rounds the three ages to 0.1 s, so they move on almost every
 * tick; an age keeps the number last handed out until what a tile shows for
 * it changes (shownAge). A fresh age can therefore read lower than the true
 * one. Nothing shows a fresh age as a figure.
 */

function applyVesselState(snapshot: VesselStateValue, raw: string): TelemetryUpdate<VesselStateValue> | null {
  let data: VesselState
  try {
    data = JSON.parse(raw) as VesselState
  } catch (err) {
    console.error('Failed to parse vessel state event:', err)
    return null
  }
  const parsed = parseVesselState(data)
  const next = { ...parsed } as Record<VesselStateField, unknown>
  let changed = false
  for (const field of FIELDS) {
    if (sameField(field, snapshot[field], parsed[field])) {
      next[field] = snapshot[field]
    } else {
      changed = true
    }
  }
  return changed ? { state: next as unknown as VesselStateValue } : null
}

// The backend pushes vessel state as it changes (ADR 0037) instead of the
// client sampling on a timer. The connection is shared with every other
// telemetry store, and reconnection on a dropped stream is owned centrally by
// use-telemetry-stream.ts: a non-200/wrong-content-type response is a
// *permanent* EventSource failure per spec, not something the browser retries
// on its own, so that module reconnects and surfaces the outage via
// useTelemetryStatus() rather than leaving this store to notice only through
// `source` going stale.
// Read at start, not at import: a module that only imports this one's
// helpers or types never touches the stream.
const listenToStream: SubscribeTelemetry = (event, onData) => subscribeTelemetry(event, onData)

const store = createTelemetryStore(listenToStream, 'vessel-state', INITIAL_VESSEL_STATE, applyVesselState)

/**
 * Starts the store on mount without re-rendering the caller on any tick.
 * AppShell calls this once.
 */
export const useRetainVesselState = store.useRetain

/**
 * The current vessel state, read once, for an event handler that needs it at
 * the moment of a click (Drop anchor here) rather than on every tick.
 */
export const getVesselStateSnapshot = store.getSnapshot

/** One slice of the vessel state; see TelemetryStore.useSelector. */
export const useVesselStateSelector = store.useSelector

function sameFields<T extends object>(a: T, b: T): boolean {
  const keys = Object.keys(a) as Array<keyof T>
  return keys.length === Object.keys(b).length && keys.every((key) => a[key] === b[key])
}

/**
 * The named fields, as one object that keeps its reference while none of
 * them changed. Pass a constant list: `fields` keys the selection.
 */
export function useVesselFields<K extends VesselStateField>(fields: readonly K[]): Pick<VesselStateValue, K> {
  const select = useCallback((state: VesselStateValue) => {
    const out = {} as Pick<VesselStateValue, K>
    for (const field of fields) out[field] = state[field]
    return out
  }, [fields])
  return useVesselStateSelector(select, sameFields)
}

import { useEffect, useState } from 'react'

import { ageFromPayload } from '@/lib/staleness'

import { GUST_WINDOWS, type GustWindow } from '@/lib/gust-windows'
import { subscribeTelemetry } from '@/hooks/use-telemetry-stream'

interface VesselState {
  status: string
  datetime: string
  depth: number
  length_overall_m: number | null
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
  max_true_wind_kts_1h: number
  generator_state: string
  generator_manual_start: boolean
  generator_manual_start_timer: number
  generator_running_by_condition: string
  generator_runtime: number
  engine_0_rpm: number
  engine_1_rpm: number
  depth_last_update_age_s: number
  position_last_update_age_s: number
  wind_last_update_age_s: number
  source: string
}

// No refresh interval: vessel state is pushed from the backend as it changes
// (ADR 0037), over a stream shared with the other telemetry hooks.
export function useVesselState() {
  const [depth, setDepth] = useState<number | null>(null)
  // The Rode Planner's swing radius fallback source when settings.anchor.loa_m
  // is unset (ADR 0047). Backend already nils this out when unpublished, so
  // no >= 0 guard is needed here, unlike the sentinel-bearing fields below.
  const [vesselLengthOverallM, setVesselLengthOverallM] = useState<number | null>(null)
  const [currentDriftKts, setCurrentDriftKts] = useState<number | null>(null)
  const [currentSetDeg, setCurrentSetDeg] = useState<number | null>(null)
  const [currentDriftImpactKts, setCurrentDriftImpactKts] = useState<number | null>(null)
  const [navigationState, setNavigationState] = useState<string | null>(null)
  const [latitude, setLatitude] = useState<number | null>(null)
  const [longitude, setLongitude] = useState<number | null>(null)
  const [gnssQualityIndicator, setGnssQualityIndicator] = useState<number | null>(null)
  const [gnssHdop, setGnssHdop] = useState<number | null>(null)
  const [gnssSatellites, setGnssSatellites] = useState<number | null>(null)
  const [gnssValidationState, setGnssValidationState] = useState<string | null>(null)
  const [gnssValidationReason, setGnssValidationReason] = useState<string | null>(null)
  const [gnssCriticalAlert, setGnssCriticalAlert] = useState<boolean>(false)
  const [headingTrue, setHeadingTrue] = useState<number | null>(null)
  const [windSpeedApparentKts, setWindSpeedApparentKts] = useState<number | null>(null)
  const [windAngleApparentDeg, setWindAngleApparentDeg] = useState<number | null>(null)
  const [windSide, setWindSide] = useState<'port' | 'starboard' | null>(null)
  const [windAngleRelativeDeg, setWindAngleRelativeDeg] = useState<number | null>(null)
  // True wind (ADR 0129 / ADR 0130) — parsed the same -1/null-is-absent way
  // as the apparent fields above, but never derived from them: a boat with
  // no true-wind source reads null here even while apparent is present.
  // Feeds the Current Conditions tile (ADR 0129: speed/direction/
  // maxTrueWindKts1h) and the Wind tile's True mode (ADR 0130: adds
  // angle/side/relative-angle and maxGustTrueKts above).
  const [windSpeedTrueKts, setWindSpeedTrueKts] = useState<number | null>(null)
  const [windAngleTrueDeg, setWindAngleTrueDeg] = useState<number | null>(null)
  const [windSideTrue, setWindSideTrue] = useState<'port' | 'starboard' | null>(null)
  const [windAngleTrueRelativeDeg, setWindAngleTrueRelativeDeg] = useState<number | null>(null)
  const [windDirectionTrueDeg, setWindDirectionTrueDeg] = useState<number | null>(null)
  const [maxTrueWindKts1h, setMaxTrueWindKts1h] = useState<number | null>(null)
  const [speedOverGroundKts, setSpeedOverGroundKts] = useState<number | null>(null)
  const [maxGustKts, setMaxGustKts] = useState<Record<GustWindow, number | null>>(
    () => Object.fromEntries(GUST_WINDOWS.map((window) => [window, null])) as Record<GustWindow, number | null>,
  )
  const [maxGustTrueKts, setMaxGustTrueKts] = useState<Record<GustWindow, number | null>>(
    () => Object.fromEntries(GUST_WINDOWS.map((window) => [window, null])) as Record<GustWindow, number | null>,
  )
  // Per-source freshness (ADR 0068). Null means the source publishes no
  // timestamp, which reads as 'not stale' rather than as fresh.
  const [depthLastUpdateAgeS, setDepthLastUpdateAgeS] = useState<number | null>(null)
  const [positionLastUpdateAgeS, setPositionLastUpdateAgeS] = useState<number | null>(null)
  const [windLastUpdateAgeS, setWindLastUpdateAgeS] = useState<number | null>(null)
  const [generatorState, setGeneratorState] = useState<string | null>(null)
  const [generatorManualStart, setGeneratorManualStart] = useState<boolean>(false)
  const [generatorManualStartTimer, setGeneratorManualStartTimer] = useState<number>(0)
  const [generatorRunningByCondition, setGeneratorRunningByCondition] = useState<string | null>(null)
  const [generatorRuntime, setGeneratorRuntime] = useState<number | null>(null)
  const [engine0Rpm, setEngine0Rpm] = useState<number | null>(null)
  const [engine1Rpm, setEngine1Rpm] = useState<number | null>(null)
  // Which source the backend answered from ("signalk", "signalk-unreachable",
  // "backend-fallback"). Surfaced so the app can offer to go looking for a
  // server that has moved, rather than leaving the operator with silent tiles.
  const [source, setSource] = useState<string | null>(null)

  useEffect(() => {
    const applyVesselState = (data: VesselState) => {
      if (typeof data.depth === 'number' && data.depth >= 0) {
        setDepth(data.depth)
      } else {
        setDepth(null)
      }

      setVesselLengthOverallM(typeof data.length_overall_m === 'number' ? data.length_overall_m : null)

      setCurrentDriftKts(typeof data.current_drift_kts === 'number' && data.current_drift_kts >= 0 ? data.current_drift_kts : null)
      setCurrentSetDeg(typeof data.current_set_deg === 'number' && data.current_set_deg >= 0 ? data.current_set_deg : null)
      setCurrentDriftImpactKts(typeof data.current_drift_impact_kts === 'number' ? data.current_drift_impact_kts : null)

      setNavigationState(typeof data.status === 'string' && data.status !== '' ? data.status : null)
      setSource(typeof data.source === 'string' && data.source !== '' ? data.source : null)

      setLatitude(typeof data.latitude === 'number' && data.latitude >= -90 && data.latitude <= 90 ? data.latitude : null)
      setLongitude(typeof data.longitude === 'number' && data.longitude >= -180 && data.longitude <= 180 ? data.longitude : null)
      setGnssQualityIndicator(typeof data.gnss_quality_indicator === 'number' && data.gnss_quality_indicator >= 0 ? data.gnss_quality_indicator : null)
      setGnssHdop(typeof data.gnss_hdop === 'number' && data.gnss_hdop >= 0 ? data.gnss_hdop : null)
      setGnssSatellites(typeof data.gnss_satellites === 'number' && data.gnss_satellites >= 0 ? data.gnss_satellites : null)
      setGnssValidationState(typeof data.gnss_validation_state === 'string' && data.gnss_validation_state !== '' ? data.gnss_validation_state : null)
      setGnssValidationReason(typeof data.gnss_validation_reason === 'string' && data.gnss_validation_reason !== '' ? data.gnss_validation_reason : null)
      setGnssCriticalAlert(data.gnss_critical_alert === true)
      setHeadingTrue(typeof data.heading_true === 'number' && data.heading_true >= 0 ? data.heading_true : null)
      setDepthLastUpdateAgeS(ageFromPayload(data.depth_last_update_age_s))
      setPositionLastUpdateAgeS(ageFromPayload(data.position_last_update_age_s))
      setWindLastUpdateAgeS(ageFromPayload(data.wind_last_update_age_s))
      setWindSpeedApparentKts(typeof data.wind_speed_apparent_kts === 'number' && data.wind_speed_apparent_kts >= 0 ? data.wind_speed_apparent_kts : null)
      setWindAngleApparentDeg(typeof data.wind_angle_apparent_deg === 'number' && data.wind_angle_apparent_deg >= 0 ? data.wind_angle_apparent_deg : null)
      setWindSide(data.wind_side === 'port' || data.wind_side === 'starboard' ? data.wind_side : null)
      setWindAngleRelativeDeg(typeof data.wind_angle_relative_deg === 'number' && data.wind_angle_relative_deg >= 0 ? data.wind_angle_relative_deg : null)
      // True wind: same -1-reads-as-null parsing as apparent above, kept in
      // its own state so an absent/stale true-wind source never inherits
      // apparent's numbers (no fallback — see AGENTS.md's fallback policy).
      setWindSpeedTrueKts(typeof data.wind_speed_true_kts === 'number' && data.wind_speed_true_kts >= 0 ? data.wind_speed_true_kts : null)
      setWindAngleTrueDeg(typeof data.wind_angle_true_deg === 'number' && data.wind_angle_true_deg >= 0 ? data.wind_angle_true_deg : null)
      setWindSideTrue(data.wind_side_true === 'port' || data.wind_side_true === 'starboard' ? data.wind_side_true : null)
      setWindAngleTrueRelativeDeg(typeof data.wind_angle_true_relative_deg === 'number' && data.wind_angle_true_relative_deg >= 0 ? data.wind_angle_true_relative_deg : null)
      setWindDirectionTrueDeg(typeof data.wind_direction_true_deg === 'number' && data.wind_direction_true_deg >= 0 ? data.wind_direction_true_deg : null)
      setMaxTrueWindKts1h(typeof data.max_true_wind_kts_1h === 'number' && data.max_true_wind_kts_1h >= 0 ? data.max_true_wind_kts_1h : null)
      // Keeps the previous object when every window's value is unchanged:
      // this arrives on the same 1Hz vessel-state tick as everything else in
      // this hook, and a fresh object literal every second defeats WindTile's
      // own memoization even on a tick where gusts genuinely haven't moved.
      setMaxGustKts((previous) => {
        const next = Object.fromEntries(GUST_WINDOWS.map((window) => {
          const value = data.max_gust_kts?.[window]
          return [window, typeof value === 'number' && value >= 0 ? value : null]
        })) as Record<GustWindow, number | null>
        const unchanged = GUST_WINDOWS.every((window) => previous[window] === next[window])
        return unchanged ? previous : next
      })
      // Same unchanged-object memo trick as maxGustKts above, for the
      // true-wind ladder's own consumer (WindTile in True mode).
      setMaxGustTrueKts((previous) => {
        const next = Object.fromEntries(GUST_WINDOWS.map((window) => {
          const value = data.max_gust_true_kts?.[window]
          return [window, typeof value === 'number' && value >= 0 ? value : null]
        })) as Record<GustWindow, number | null>
        const unchanged = GUST_WINDOWS.every((window) => previous[window] === next[window])
        return unchanged ? previous : next
      })
      setSpeedOverGroundKts(typeof data.speed_over_ground_kts === 'number' && data.speed_over_ground_kts >= 0 ? data.speed_over_ground_kts : null)

      setGeneratorState(typeof data.generator_state === 'string' && data.generator_state !== '' ? data.generator_state : null)
      setGeneratorManualStart(data.generator_manual_start === true)
      setGeneratorManualStartTimer(typeof data.generator_manual_start_timer === 'number' ? data.generator_manual_start_timer : 0)
      setGeneratorRunningByCondition(typeof data.generator_running_by_condition === 'string' && data.generator_running_by_condition !== '' ? data.generator_running_by_condition : null)
      setGeneratorRuntime(typeof data.generator_runtime === 'number' && data.generator_runtime >= 0 ? data.generator_runtime : null)
      setEngine0Rpm(typeof data.engine_0_rpm === 'number' && data.engine_0_rpm >= 0 ? data.engine_0_rpm : null)
      setEngine1Rpm(typeof data.engine_1_rpm === 'number' && data.engine_1_rpm >= 0 ? data.engine_1_rpm : null)
    }

    // The backend pushes vessel state as it changes (ADR 0037) instead of the
    // client sampling on a timer. The connection is shared with every other
    // telemetry hook, and reconnection on a dropped stream is owned centrally
    // by use-telemetry-stream.ts, not per-hook: a non-200/wrong-content-type
    // response is a *permanent* EventSource failure per spec, not something
    // the browser retries on its own, so that module reconnects and surfaces
    // the outage via useTelemetryStatus() rather than leaving this hook to
    // notice only through `source` going stale.
    return subscribeTelemetry('vessel-state', (raw) => {
      try {
        applyVesselState(JSON.parse(raw) as VesselState)
      } catch (err) {
        console.error('Failed to parse vessel state event:', err)
      }
    })
  }, [])

  return {
    depth,
    depthLastUpdateAgeS,
    positionLastUpdateAgeS,
    windLastUpdateAgeS,
    vesselLengthOverallM,
    currentDriftKts,
    currentSetDeg,
    currentDriftImpactKts,
    navigationState,
    latitude,
    longitude,
    gnssQualityIndicator,
    gnssHdop,
    gnssSatellites,
    gnssValidationState,
    gnssValidationReason,
    gnssCriticalAlert,
    headingTrue,
    speedOverGroundKts,
    windSpeedApparentKts,
    windAngleApparentDeg,
    windSide,
    windAngleRelativeDeg,
    windSpeedTrueKts,
    windAngleTrueDeg,
    windSideTrue,
    windAngleTrueRelativeDeg,
    windDirectionTrueDeg,
    maxGustKts,
    maxGustTrueKts,
    maxTrueWindKts1h,
    generatorState,
    generatorManualStart,
    generatorManualStartTimer,
    generatorRunningByCondition,
    generatorRuntime,
    engine0Rpm,
    engine1Rpm,
    source,
  }
}

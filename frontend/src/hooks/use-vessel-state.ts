import { useEffect, useState } from 'react'

interface VesselState {
  status: string
  datetime: string
  depth: number
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
  max_gust_10m_kts: number
  max_gust_1h_kts: number
  generator_state: string
  generator_manual_start: boolean
  generator_manual_start_timer: number
  generator_running_by_condition: string
  generator_runtime: number
  engine_0_rpm: number
  engine_1_rpm: number
  source: string
}

export function useVesselState(refreshInterval: number) {
  const [depth, setDepth] = useState<number | null>(null)
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
  const [speedOverGroundKts, setSpeedOverGroundKts] = useState<number | null>(null)
  const [maxGust10mKts, setMaxGust10mKts] = useState<number | null>(null)
  const [maxGust1hKts, setMaxGust1hKts] = useState<number | null>(null)
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
    const fetchVesselState = async () => {
      try {
        const response = await fetch('/api/vessel-state')

        if (!response.ok) {
          throw new Error('Failed to fetch vessel state')
        }

        const data = (await response.json()) as VesselState

        if (typeof data.depth === 'number' && data.depth >= 0) {
          setDepth(data.depth)
        } else {
          setDepth(null)
        }

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
        setWindSpeedApparentKts(typeof data.wind_speed_apparent_kts === 'number' && data.wind_speed_apparent_kts >= 0 ? data.wind_speed_apparent_kts : null)
        setWindAngleApparentDeg(typeof data.wind_angle_apparent_deg === 'number' && data.wind_angle_apparent_deg >= 0 ? data.wind_angle_apparent_deg : null)
        setWindSide(data.wind_side === 'port' || data.wind_side === 'starboard' ? data.wind_side : null)
        setWindAngleRelativeDeg(typeof data.wind_angle_relative_deg === 'number' && data.wind_angle_relative_deg >= 0 ? data.wind_angle_relative_deg : null)
        setMaxGust10mKts(typeof data.max_gust_10m_kts === 'number' && data.max_gust_10m_kts >= 0 ? data.max_gust_10m_kts : null)
        setMaxGust1hKts(typeof data.max_gust_1h_kts === 'number' && data.max_gust_1h_kts >= 0 ? data.max_gust_1h_kts : null)
        setSpeedOverGroundKts(typeof data.speed_over_ground_kts === 'number' && data.speed_over_ground_kts >= 0 ? data.speed_over_ground_kts : null)

        setGeneratorState(typeof data.generator_state === 'string' && data.generator_state !== '' ? data.generator_state : null)
        setGeneratorManualStart(data.generator_manual_start === true)
        setGeneratorManualStartTimer(typeof data.generator_manual_start_timer === 'number' ? data.generator_manual_start_timer : 0)
        setGeneratorRunningByCondition(typeof data.generator_running_by_condition === 'string' && data.generator_running_by_condition !== '' ? data.generator_running_by_condition : null)
        setGeneratorRuntime(typeof data.generator_runtime === 'number' && data.generator_runtime >= 0 ? data.generator_runtime : null)
        setEngine0Rpm(typeof data.engine_0_rpm === 'number' && data.engine_0_rpm >= 0 ? data.engine_0_rpm : null)
        setEngine1Rpm(typeof data.engine_1_rpm === 'number' && data.engine_1_rpm >= 0 ? data.engine_1_rpm : null)
      } catch (err) {
        console.error('Failed to fetch vessel state:', err)
      }
    }

    void fetchVesselState()
    const timer = window.setInterval(() => {
      void fetchVesselState()
    }, refreshInterval * 1000)

    return () => {
      window.clearInterval(timer)
    }
  }, [refreshInterval])

  return {
    depth,
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
    maxGust10mKts,
    maxGust1hKts,
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

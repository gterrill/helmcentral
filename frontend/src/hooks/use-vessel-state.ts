import { useEffect, useState } from 'react'

interface VesselState {
  status: string
  datetime: string
  depth: number
  latitude: number
  longitude: number
  heading_true: number
  speed_over_ground_kts: number
  wind_speed_apparent_kts: number
  wind_angle_apparent_deg: number
  wind_side: string
  wind_angle_relative_deg: number
  max_gust_10m_kts: number
  max_gust_1h_kts: number
  source: string
}

export function useVesselState(refreshInterval: number) {
  const [depth, setDepth] = useState<number | null>(null)
  const [navigationState, setNavigationState] = useState<string | null>(null)
  const [latitude, setLatitude] = useState<number | null>(null)
  const [longitude, setLongitude] = useState<number | null>(null)
  const [headingTrue, setHeadingTrue] = useState<number | null>(null)
  const [windSpeedApparentKts, setWindSpeedApparentKts] = useState<number | null>(null)
  const [windAngleApparentDeg, setWindAngleApparentDeg] = useState<number | null>(null)
  const [windSide, setWindSide] = useState<'port' | 'starboard' | null>(null)
  const [windAngleRelativeDeg, setWindAngleRelativeDeg] = useState<number | null>(null)
  const [speedOverGroundKts, setSpeedOverGroundKts] = useState<number | null>(null)
  const [maxGust10mKts, setMaxGust10mKts] = useState<number | null>(null)
  const [maxGust1hKts, setMaxGust1hKts] = useState<number | null>(null)

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

        setNavigationState(typeof data.status === 'string' && data.status !== '' ? data.status : null)

        setLatitude(typeof data.latitude === 'number' && data.latitude >= -90 && data.latitude <= 90 ? data.latitude : null)
        setLongitude(typeof data.longitude === 'number' && data.longitude >= -180 && data.longitude <= 180 ? data.longitude : null)
        setHeadingTrue(typeof data.heading_true === 'number' && data.heading_true >= 0 ? data.heading_true : null)
        setWindSpeedApparentKts(typeof data.wind_speed_apparent_kts === 'number' && data.wind_speed_apparent_kts >= 0 ? data.wind_speed_apparent_kts : null)
        setWindAngleApparentDeg(typeof data.wind_angle_apparent_deg === 'number' && data.wind_angle_apparent_deg >= 0 ? data.wind_angle_apparent_deg : null)
        setWindSide(data.wind_side === 'port' || data.wind_side === 'starboard' ? data.wind_side : null)
        setWindAngleRelativeDeg(typeof data.wind_angle_relative_deg === 'number' && data.wind_angle_relative_deg >= 0 ? data.wind_angle_relative_deg : null)
        setMaxGust10mKts(typeof data.max_gust_10m_kts === 'number' && data.max_gust_10m_kts >= 0 ? data.max_gust_10m_kts : null)
        setMaxGust1hKts(typeof data.max_gust_1h_kts === 'number' && data.max_gust_1h_kts >= 0 ? data.max_gust_1h_kts : null)
        setSpeedOverGroundKts(typeof data.speed_over_ground_kts === 'number' && data.speed_over_ground_kts >= 0 ? data.speed_over_ground_kts : null)
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
    navigationState,
    latitude,
    longitude,
    headingTrue,
    speedOverGroundKts,
    windSpeedApparentKts,
    windAngleApparentDeg,
    windSide,
    windAngleRelativeDeg,
    maxGust10mKts,
    maxGust1hKts,
  }
}

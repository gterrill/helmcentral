import { useEffect, useState } from 'react'

interface VesselState {
  status: string
  datetime: string
  depth: number
  latitude: number
  longitude: number
  heading_true: number
  source: string
}

export function useVesselState(refreshInterval: number) {
  const [depth, setDepth] = useState<number | null>(null)
  const [latitude, setLatitude] = useState<number | null>(null)
  const [longitude, setLongitude] = useState<number | null>(null)
  const [headingTrue, setHeadingTrue] = useState<number | null>(null)

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

        setLatitude(typeof data.latitude === 'number' && data.latitude >= -90 && data.latitude <= 90 ? data.latitude : null)
        setLongitude(typeof data.longitude === 'number' && data.longitude >= -180 && data.longitude <= 180 ? data.longitude : null)
        setHeadingTrue(typeof data.heading_true === 'number' && data.heading_true >= 0 ? data.heading_true : null)
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

  return { depth, latitude, longitude, headingTrue }
}

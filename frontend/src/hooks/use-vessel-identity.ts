import { useEffect, useMemo, useState } from 'react'

import { getUiConfig } from '@/config/app-config'

export function formatClock(date: Date) {
  const value = new Intl.DateTimeFormat('en-US', {
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    hour12: true,
  }).format(date)

  const [timePart = '--:--:--', meridiem = ''] = value.toUpperCase().split(/\s+/)
  return { timePart, meridiem }
}

export function formatDate(date: Date) {
  return new Intl.DateTimeFormat('en-US', {
    weekday: 'long',
    month: 'short',
    day: 'numeric',
    year: 'numeric',
  }).format(date)
}

export function useVesselIdentity() {
  const [now, setNow] = useState(() => new Date())
  const [vesselStatus, setVesselStatus] = useState('At Anchor')
  const [boatName, setBoatName] = useState<string | null>(null)
  const [boatModel, setBoatModel] = useState<string | null>(null)
  const [signalkConnected, setSignalkConnected] = useState<boolean | null>(null)
  const apiBaseUrl = import.meta.env.VITE_API_BASE_URL ?? `${window.location.protocol}//${window.location.hostname}:8080`

  useEffect(() => {
    const clockTimer = window.setInterval(() => {
      setNow((current) => new Date(current.getTime() + 1000))
    }, 1000)

    const fetchVesselState = async () => {
      try {
        const response = await fetch(`${apiBaseUrl}/api/vessel-state`)

        if (!response.ok) {
          throw new Error('Failed to fetch vessel state')
        }

        const data = (await response.json()) as {
          status?: string
          datetime?: string
          depth?: number
          name?: string
          vessel_prefix?: string
          source?: string
        }

        if (data.status) {
          setVesselStatus(data.status)
        }

        if (data.name) {
          const prefix = data.vessel_prefix?.trim() ?? 'M/V'
          const vesselName = data.name.trim()
          setBoatName(vesselName ? `${prefix} ${vesselName}`.trim() : null)
        }

        if (data.datetime) {
          const backendTime = new Date(data.datetime)
          if (!Number.isNaN(backendTime.getTime())) {
            setNow(backendTime)
          }
        }

        setSignalkConnected(data.source === 'signalk')
      } catch {
        setSignalkConnected(false)
      }
    }

    const fetchSettings = async () => {
      try {
        const response = await fetch(`${apiBaseUrl}/api/settings`)

        if (!response.ok) {
          throw new Error('Failed to fetch settings')
        }

        const data = (await response.json()) as {
          boat?: {
            model?: string
          }
        }

        const nextModel = data.boat?.model?.trim() ?? ''

        setBoatModel(nextModel.length > 0 ? nextModel : null)
      } catch {
        // Show missing settings explicitly instead of falling back to compiled defaults.
        setBoatModel(null)
      }
    }

    void fetchVesselState()
    void fetchSettings()
    const syncTimer = window.setInterval(() => {
      void fetchVesselState()
      void fetchSettings()
    }, getUiConfig().vesselStateRefreshSeconds * 1000)

    return () => {
      window.clearInterval(clockTimer)
      window.clearInterval(syncTimer)
    }
  }, [apiBaseUrl])

  const currentDate = useMemo(() => formatDate(now).toUpperCase(), [now])
  const clock = useMemo(() => formatClock(now), [now])

  return { now, currentDate, clock, vesselStatus, boatName, boatModel, signalkConnected }
}

import { useEffect, useMemo, useState } from 'react'

import { apiBaseUrl } from '@/config/api'
import { useAppConfig } from '@/hooks/use-app-config'

// The vessel's local zone (backend/weather_tide.go's vesselLocalTimezoneName,
// ADR 0035) can be an IANA name Intl has never heard of only if the backend
// itself starts sending something malformed — that upstream contract is
// worth failing loudly for, but not by crashing the wall display's clock.
// Warn once per bad zone name rather than throwing, and rather than warning
// on every per-second tick.
const warnedInvalidTimeZones = new Set<string>()

function dateTimeFormat(options: Intl.DateTimeFormatOptions, timeZone?: string): Intl.DateTimeFormat {
  if (timeZone) {
    try {
      return new Intl.DateTimeFormat('en-US', { ...options, timeZone })
    } catch (error) {
      if (!warnedInvalidTimeZones.has(timeZone)) {
        warnedInvalidTimeZones.add(timeZone)
        console.warn(`use-vessel-identity: unknown timezone "${timeZone}", falling back to the browser zone`, error)
      }
    }
  }
  return new Intl.DateTimeFormat('en-US', options)
}

export function formatClock(date: Date, timeZone?: string) {
  const value = dateTimeFormat(
    {
      hour: '2-digit',
      minute: '2-digit',
      second: '2-digit',
      hour12: true,
    },
    timeZone,
  ).format(date)

  const [timePart = '--:--:--', meridiem = ''] = value.toUpperCase().split(/\s+/)
  return { timePart, meridiem }
}

export function formatDate(date: Date, options?: { compact?: boolean; timeZone?: string }) {
  return dateTimeFormat(
    {
      weekday: options?.compact ? 'short' : 'long',
      month: 'short',
      day: 'numeric',
      year: 'numeric',
    },
    options?.timeZone,
  ).format(date)
}

export function useVesselIdentity() {
  const [now, setNow] = useState(() => new Date())
  const [vesselStatus, setVesselStatus] = useState('At Anchor')
  const [boatName, setBoatName] = useState<string | null>(null)
  const [boatModel, setBoatModel] = useState<string | null>(null)
  const [signalkConnected, setSignalkConnected] = useState<boolean | null>(null)
  const [timeZone, setTimeZone] = useState<string | undefined>(undefined)
  const { ui: uiConfig } = useAppConfig()
  const refreshSeconds = uiConfig.vesselStateRefreshSeconds

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
          timezone?: string
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

        if (data.timezone) {
          setTimeZone(data.timezone)
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
    }, refreshSeconds * 1000)

    return () => {
      window.clearInterval(clockTimer)
      window.clearInterval(syncTimer)
    }
    // Re-arms the poll when the operator changes the refresh interval.
  }, [refreshSeconds])

  const currentDate = useMemo(() => formatDate(now, { timeZone }).toUpperCase(), [now, timeZone])
  const clock = useMemo(() => formatClock(now, timeZone), [now, timeZone])

  return { now, currentDate, clock, vesselStatus, boatName, boatModel, signalkConnected, timeZone }
}

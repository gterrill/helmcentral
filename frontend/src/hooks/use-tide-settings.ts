import { useCallback, useEffect, useState } from 'react'

export interface TideSettings {
  tideProvider: string
  tideStationId: string
  tideStationName: string
  tideAutoStation: boolean
}

// tideProvider starts empty, not a hardcoded provider id: tides are
// WASM-plugin-only with no built-in default (see backend/tide_providers.go),
// so silently substituting a fake "default" here would mask an unconfigured
// install exactly the way backend/weather_tide.go's tideToday deliberately
// does not - it surfaces "no tide provider configured" instead. Consumers
// (e.g. ForecastTideSection) already gate on `loading` before reading
// tideProvider, so an empty initial/fallback value is safe.
const defaultTideSettings: TideSettings = {
  tideProvider: '',
  tideStationId: '',
  tideStationName: '',
  tideAutoStation: false,
}

export function useTideSettings() {
  const [settings, setSettings] = useState<TideSettings>(defaultTideSettings)
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const fetchSettings = useCallback(async () => {
    try {
      setLoading(true)
      const response = await fetch('/api/settings')
      if (!response.ok) {
        throw new Error(`HTTP error! status: ${response.status}`)
      }

      const data = await response.json()
      setSettings({
        tideProvider: typeof data.ui?.tide_provider === 'string' ? data.ui.tide_provider : defaultTideSettings.tideProvider,
        tideStationId: typeof data.ui?.tide_station_id === 'string' ? data.ui.tide_station_id : '',
        tideStationName: typeof data.ui?.tide_station_name === 'string' ? data.ui.tide_station_name : '',
        tideAutoStation: typeof data.ui?.tide_auto_station === 'boolean' ? data.ui.tide_auto_station : false,
      })
      setError(null)
    } catch (err) {
      console.error('Error fetching tide settings:', err)
      setError(err instanceof Error ? err.message : 'Failed to load tide settings')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void fetchSettings()
  }, [fetchSettings])

  // When auto-update is on, re-fetch settings every 30 min so the UI picks up
  // any station changes written by the backend's background auto-updater.
  useEffect(() => {
    if (!settings.tideAutoStation) return
    const id = setInterval(() => void fetchSettings(), 30 * 60 * 1000)
    return () => clearInterval(id)
  }, [settings.tideAutoStation, fetchSettings])

  const saveStation = useCallback(async (stationId: string, stationName: string) => {
    setSaving(true)
    setError(null)
    try {
      const current = await fetch('/api/settings')
      if (!current.ok) {
        throw new Error(`HTTP error! status: ${current.status}`)
      }
      const data = await current.json()

      const payload = {
        ...data,
        ui: {
          ...data.ui,
          tide_station_id: stationId,
          tide_station_name: stationName,
        },
      }

      const response = await fetch('/api/settings', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload),
      })
      if (!response.ok) {
        const errorPayload = (await response.json().catch(() => null)) as { error?: string } | null
        throw new Error(errorPayload?.error ?? 'Unable to save tide station')
      }

      setSettings((previous) => ({ ...previous, tideStationId: stationId, tideStationName: stationName }))
    } catch (err) {
      console.error('Error saving tide station:', err)
      setError(err instanceof Error ? err.message : 'Failed to save tide station')
      throw err
    } finally {
      setSaving(false)
    }
  }, [])

  return { ...settings, loading, saving, error, refetch: fetchSettings, saveStation }
}

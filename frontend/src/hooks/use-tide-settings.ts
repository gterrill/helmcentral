import { useCallback, useMemo, useState } from 'react'
import { apiRequest, setQueryData, useApiQuery } from '@/lib/api-query'

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

// The full settings document, of which tide settings are a slice under `ui`.
interface SettingsDocument {
  ui?: {
    tide_provider?: unknown
    tide_station_id?: unknown
    tide_station_name?: unknown
    tide_auto_station?: unknown
  } & Record<string, unknown>
}

const SETTINGS_KEY = ['/api/settings'] as const
const AUTO_STATION_REFRESH_MS = 30 * 60 * 1000

export function useTideSettings() {
  const [saving, setSaving] = useState(false)
  const [saveError, setSaveError] = useState<string | null>(null)

  const { data, isLoading, error: loadError, refetch } = useApiQuery<SettingsDocument>({
    queryKey: SETTINGS_KEY,
    path: '/api/settings',
  })
  const settings = useMemo<TideSettings>(
    () => ({
      tideProvider: typeof data?.ui?.tide_provider === 'string' ? data.ui.tide_provider : defaultTideSettings.tideProvider,
      tideStationId: typeof data?.ui?.tide_station_id === 'string' ? data.ui.tide_station_id : '',
      tideStationName: typeof data?.ui?.tide_station_name === 'string' ? data.ui.tide_station_name : '',
      tideAutoStation: typeof data?.ui?.tide_auto_station === 'boolean' ? data.ui.tide_auto_station : false,
    }),
    [data],
  )

  // When auto-update is on, re-fetch settings every 30 min so the UI picks up
  // any station changes written by the backend's background auto-updater. The
  // interval is a second reader of the same entry, since whether auto-update
  // is on only shows once the first has loaded.
  useApiQuery<SettingsDocument>({
    queryKey: SETTINGS_KEY,
    path: '/api/settings',
    refetchInterval: settings.tideAutoStation ? AUTO_STATION_REFRESH_MS : undefined,
  })

  const saveStation = useCallback(async (stationId: string, stationName: string) => {
    setSaving(true)
    setSaveError(null)
    try {
      const current = await apiRequest<SettingsDocument>('/api/settings')
      await apiRequest<unknown>('/api/settings', {
        method: 'POST',
        json: { ...current, ui: { ...current.ui, tide_station_id: stationId, tide_station_name: stationName } },
      })
      // The saved document is the one just read plus the two fields, so the
      // shared settings entry takes the same patch without a re-read.
      setQueryData<SettingsDocument>(SETTINGS_KEY, (prev) => ({
        ...prev,
        ui: { ...prev?.ui, tide_station_id: stationId, tide_station_name: stationName },
      }))
    } catch (err) {
      console.error('Error saving tide station:', err)
      setSaveError(err instanceof Error ? err.message : 'Failed to save tide station')
      throw err
    } finally {
      setSaving(false)
    }
  }, [])

  const error = saveError ?? loadError?.message ?? null
  return { ...settings, loading: isLoading, saving, error, refetch, saveStation }
}

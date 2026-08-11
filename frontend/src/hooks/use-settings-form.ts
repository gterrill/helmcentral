import { useCallback, useEffect, useRef, useState } from 'react'

import { apiBaseUrl } from '@/config/api'
import { publishAppConfigSettings } from '@/hooks/use-app-config'

export type SettingsPayload = {
  signalk?: {
    address?: string
    port?: number
  }
  boat?: {
    vessel_prefix?: string
    model?: string
    house_battery_capacity_ah?: number
  }
  ui?: {
    vessel_state_refresh_seconds?: number
    tank_labels?: Record<string, string>
    tide_provider?: string
    tide_station_id?: string
    tide_station_name?: string
    tide_auto_station?: boolean
    weather_provider?: string
    wave_provider?: string
    forecast_warnings_provider?: string
  }
  anchor?: {
    bow_roller_height_m?: number
    chain_size_mm?: number
    chain_onboard_m?: number
    hull_type?: string
    windage_area_m2?: number
  }
  influxdb?: {
    enabled?: boolean
    url?: string
    org?: string
    bucket?: string
  }
  units?: string
}

// Deep-partial utility, local to this hook per the task's guidance to check
// `@/lib` first (no existing DeepPartial there) before defining a new one.
export type DeepPartial<T> = T extends (infer U)[]
  ? DeepPartial<U>[]
  : T extends object
    ? { [K in keyof T]?: DeepPartial<T[K]> }
    : T

/**
 * Merges `patch` into `current`, one level deep for each top-level
 * sub-object (`signalk`/`boat`/`ui`/`anchor`/`influxdb`). This is
 * deliberately shallow *within* each sub-object (not a recursive deep
 * merge) — e.g. `save({ ui: { tide_provider: 'bom' } })` must not wipe
 * `ui.tank_labels`, but nothing in this payload nests deeper than one
 * level under a top-level key, so a one-level merge per sub-object is
 * sufficient and avoids surprising array/object-merge semantics for
 * `ui.tank_labels` (a flat string map, merged key-by-key which is also
 * the desired behavior there).
 */
export function deepMergeSettings(
  current: SettingsPayload,
  patch: DeepPartial<SettingsPayload>,
): SettingsPayload {
  const merged: SettingsPayload = { ...current }

  for (const key of Object.keys(patch) as Array<keyof SettingsPayload>) {
    const patchValue = patch[key]
    if (patchValue === undefined) continue

    if (key === 'units') {
      merged.units = patchValue as SettingsPayload['units']
      continue
    }

    const currentSub = (current[key] ?? {}) as Record<string, unknown>
    const patchSub = patchValue as Record<string, unknown>
    merged[key] = { ...currentSub, ...patchSub } as never
  }

  return merged
}

export interface UseSettingsFormResult {
  settings: SettingsPayload
  setSettings: React.Dispatch<React.SetStateAction<SettingsPayload>>
  loading: boolean
  saving: boolean
  error: string | null
  save: (patch: DeepPartial<SettingsPayload>) => Promise<SettingsPayload>
}

export function useSettingsForm(): UseSettingsFormResult {
  const [settings, setSettings] = useState<SettingsPayload>({})
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)

  // Kept in sync with `settings` via effect below so `save()` always reads
  // the latest in-memory state without needing `settings` in its own
  // dependency array (which would otherwise recreate the callback, and thus
  // any effect/handler depending on it, on every settings change).
  const settingsRef = useRef(settings)
  useEffect(() => {
    settingsRef.current = settings
  }, [settings])

  useEffect(() => {
    const fetchSettings = async () => {
      try {
        const response = await fetch(`${apiBaseUrl}/api/settings`)
        if (!response.ok) {
          throw new Error('Failed to fetch settings')
        }
        const data = (await response.json()) as SettingsPayload
        setSettings(data)
      } catch {
        try {
          const response = await fetch(`${apiBaseUrl}/api/settings/signalk`)
          if (!response.ok) {
            throw new Error('Failed to fetch SignalK settings')
          }
          const data = (await response.json()) as { address?: string; port?: number }
          setSettings((previous) => ({ ...previous, signalk: data }))
        } catch {
          // Keep defaults if settings endpoint is unavailable.
        }
      } finally {
        setLoading(false)
      }
    }

    void fetchSettings()
  }, [])

  const save = useCallback(async (patch: DeepPartial<SettingsPayload>) => {
    setSaving(true)
    setError(null)

    const merged = deepMergeSettings(settingsRef.current, patch)

    try {
      const response = await fetch(`${apiBaseUrl}/api/settings`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
        },
        body: JSON.stringify(merged),
      })

      if (!response.ok) {
        const errorPayload = (await response.json().catch(() => null)) as { error?: string } | null
        throw new Error(errorPayload?.error ?? 'Unable to save settings')
      }

      const data = (await response.json()) as SettingsPayload
      setSettings(data)
      // Units and anchor geometry drive rendering across the dashboard, not
      // just this form, so push the authoritative response to those consumers
      // rather than making the operator reload.
      publishAppConfigSettings(data)
      return data
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Unable to save settings'
      setError(message)
      throw err
    } finally {
      setSaving(false)
    }
  }, [])

  return { settings, setSettings, loading, saving, error, save }
}

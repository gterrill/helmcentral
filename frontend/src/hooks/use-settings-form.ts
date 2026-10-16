import { useCallback, useEffect, useRef, useState } from 'react'

import { apiRequest, invalidateQueries, setQueryData } from '@/lib/api-query'
import { publishAppConfigSettings } from '@/hooks/use-app-config'
import type { VesselRoles } from '@/lib/vessel-settings'

export type SettingsPayload = {
  signalk?: {
    address?: string
    port?: number
  }
  boat?: {
    vessel_prefix?: string
    model?: string
  }
  ui?: {
    tide_provider?: string
    tide_station_id?: string
    tide_station_name?: string
    tide_auto_station?: boolean
    weather_provider?: string
    wave_provider?: string
    poi_provider?: string
    // ADR 0101: which installed POI plugin resolves place names - separate
    // from poi_provider (the Nearby widget), though the two commonly name
    // the same plugin.
    place_name_provider?: string
    forecast_warnings_provider?: string
  }
  anchor?: {
    bow_roller_height_m?: number
    gps_from_bow_m?: number
    loa_m?: number
    chain_size_mm?: number
    chain_onboard_m?: number
    hull_type?: string
    scope_method?: string
    windage_area_m2?: number
    // ADR 0099: gates the server-side anchor auto-raise watcher.
    auto_raise_on_motoring?: boolean
    // ADR 0135: water the operator wants under the keel at the next low
    // tide before Anchor Watch's low-water clearance warning fires.
    min_clearance_at_low_m?: number
  }
  influxdb?: {
    enabled?: boolean
    url?: string
    org?: string
    bucket?: string
  }
  mayara?: {
    address?: string
    port?: number
  }
  auth?: {
    mode?: string
  }
  // ADR 0093: the onboard assistant. `notes` are the operator's standing
  // notes, injected verbatim into every system prompt. The voice phase adds
  // three switches, all default false: `voice_input` (a microphone button
  // in the header, https-only), `read_aloud` (speak the spoken summary of
  // each reply), and `wake_word` ("Hey Mate" always-listening).
  assistant?: {
    enabled?: boolean
    model?: string
    document_model?: string
    notes?: string
    allowed_models?: string[]
    excluded_models?: string[]
    cost_tier?: string
    voice_input?: boolean
    read_aloud?: boolean
    wake_word?: boolean
    web_search?: boolean
    web_search_model?: string
  }
  // Which equipment item plays each single-choice part (house bank, port
  // alternator, ...). Saved with the rest of Settings. A payload that omits it
  // leaves the stored block alone on the server.
  roles?: VesselRoles
  // Set by the server instead of `roles` when the stored block cannot be read.
  roles_error?: string
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
 * `ui.tide_station_id`, but nothing in this payload nests deeper than one
 * level under a top-level key, so a one-level merge per sub-object is
 * sufficient and avoids surprising array/object-merge semantics.
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

  // The form edits a local draft seeded from the server, so this read stays
  // out of the shared cache.
  useEffect(() => {
    const fetchSettings = async () => {
      try {
        setSettings(await apiRequest<SettingsPayload>('/api/settings'))
      } catch {
        try {
          const data = await apiRequest<{ address?: string; port?: number }>('/api/settings/signalk')
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
      const data = await apiRequest<SettingsPayload>('/api/settings', { method: 'POST', json: merged })
      setSettings(data)
      // The response is the whole document, the shape other readers of the
      // shared entry (the tide settings) take, so they follow the save.
      setQueryData(['/api/settings'], data)
      // Saved roles (and a changed SignalK address) move the setup steps.
      void invalidateQueries(['/api/setup/status'])
      void invalidateQueries(['/api/vessel/discovery'])
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

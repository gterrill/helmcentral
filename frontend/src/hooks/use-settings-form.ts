import { useCallback, useEffect, useRef, useState } from 'react'

import { ApiError, apiRequest, invalidateQueries, setQueryData } from '@/lib/api-query'
import { publishAppConfigSettings } from '@/hooks/use-app-config'
import type { VesselRoles } from '@/lib/vessel-settings'

/** A sector's no-go figures; null/absent is unset (never 0). Marginal and gust limits are worked out, not stored. */
export interface PassageSectorLimitsPayload {
  wind_kn_no_go?: number | null
  wave_m_no_go?: number | null
}

/** Mate's go/no-go limits (assistant.passage_limits). Sectors are true wind angle off the course. */
export interface PassageLimitsPayload {
  ahead?: PassageSectorLimitsPayload
  abeam?: PassageSectorLimitsPayload
  astern?: PassageSectorLimitsPayload
  /** Knots to take off the no-go wind while the stream runs against the wind. */
  wind_against_tide_offset_kn?: number | null
  daylight_arrival?: boolean
}

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
    routines_enabled?: boolean
    routines_daily_cap_usd?: number
    passage_limits?: PassageLimitsPayload
  }
  // The on-board coastline Mate's shelter estimate reads. Off by default.
  offline_coastline?: {
    enabled?: boolean
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
  /** True once a settings read succeeded. False while loading, and if every read failed. */
  loaded: boolean
  saving: boolean
  error: string | null
  save: (patch: DeepPartial<SettingsPayload>) => Promise<SettingsPayload>
}

const RETRY_DELAYS_MS = [5_000, 15_000, 60_000]

export function useSettingsForm(): UseSettingsFormResult {
  const [settings, setSettings] = useState<SettingsPayload>({})
  const [loading, setLoading] = useState(true)
  const [loaded, setLoaded] = useState(false)
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

  const loadedRef = useRef(false)
  const markLoaded = useCallback(() => {
    loadedRef.current = true
    setLoaded(true)
  }, [])

  // The form edits a local draft seeded from the server, so this read stays
  // out of the shared cache. A failed read is retried (5 s, 15 s, then every
  // 60 s) until one succeeds or a save does, since reads fail while the
  // backend restarts and `loaded` would otherwise stay false until a reload.
  // The attempts run one after another from a single timer, so two reads are
  // never in flight together, and only the first flips `loading`.
  useEffect(() => {
    let cancelled = false
    let timer: ReturnType<typeof setTimeout> | undefined
    let denied = false

    const fetchSettings = async (attempt: number) => {
      // A save may have landed while this retry was waiting.
      if (attempt > 0 && loadedRef.current) return
      try {
        const data = await apiRequest<SettingsPayload>('/api/settings')
        if (cancelled) return
        setSettings(data)
        markLoaded()
      } catch (err) {
        // A session that may not read settings will not be allowed on a retry
        // either, and a signalk-only read would not be the whole document, so
        // `loaded` stays false and nothing can be saved over unread settings.
        if (err instanceof ApiError && (err.status === 401 || err.status === 403)) denied = true
      } finally {
        if (!cancelled && attempt === 0) setLoading(false)
      }
      if (cancelled || denied || loadedRef.current) return
      timer = setTimeout(() => void fetchSettings(attempt + 1), RETRY_DELAYS_MS[Math.min(attempt, RETRY_DELAYS_MS.length - 1)])
    }

    void fetchSettings(0)
    return () => {
      cancelled = true
      if (timer) clearTimeout(timer)
    }
  }, [markLoaded])

  const save = useCallback(async (patch: DeepPartial<SettingsPayload>) => {
    setSaving(true)
    setError(null)

    const merged = deepMergeSettings(settingsRef.current, patch)

    try {
      const data = await apiRequest<SettingsPayload>('/api/settings', { method: 'POST', json: merged })
      setSettings(data)
      // The response is the whole document, so a save counts as a load.
      markLoaded()
      // The response is the whole document, the shape other readers of the
      // shared entry (the tide settings) take, so they follow the save.
      setQueryData(['/api/settings'], data)
      // Saved roles (and a changed SignalK address) move the setup steps.
      void invalidateQueries(['/api/setup/status'])
      void invalidateQueries(['/api/vessel/discovery'])
      // The Offline coastline switch is part of this save; its status line
      // must not keep saying "Off" until the next poll.
      void invalidateQueries(['/api/offline-coastline'])
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
  }, [markLoaded])

  return { settings, setSettings, loading, loaded, saving, error, save }
}

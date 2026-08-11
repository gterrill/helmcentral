import { useEffect, useState } from 'react'

import { apiBaseUrl } from '@/config/api'
import {
  fallbackAnchorConfig,
  fallbackUiConfig,
  normalizeAnchorConfig,
  normalizeUiConfig,
  type AnchorConfig,
  type AppConfigSettings,
  type UiConfig,
} from '@/config/app-config'

export type AppConfig = {
  ui: UiConfig
  anchor: AnchorConfig
  /** False until the backend's settings have been applied (or have failed). */
  loaded: boolean
}

const defaultAppConfig: AppConfig = {
  ui: fallbackUiConfig,
  anchor: fallbackAnchorConfig,
  loaded: false,
}

// Module-level rather than a context provider: this is read from App and from
// hooks that aren't rendered underneath it, and every consumer wants the same
// answer. The single-flight promise keeps a dozen consumers from issuing a
// dozen identical GETs on mount.
let current: AppConfig = defaultAppConfig
let inflight: Promise<AppConfig> | null = null
const listeners = new Set<(config: AppConfig) => void>()

function publish(config: AppConfig) {
  current = config
  for (const listener of listeners) listener(config)
}

function toAppConfig(settings: AppConfigSettings | null): AppConfig {
  return {
    ui: normalizeUiConfig(settings),
    anchor: normalizeAnchorConfig(settings),
    loaded: true,
  }
}

async function load(): Promise<AppConfig> {
  try {
    const response = await fetch(`${apiBaseUrl}/api/settings`)
    if (!response.ok) {
      throw new Error('Failed to fetch settings')
    }
    const settings = (await response.json()) as AppConfigSettings
    return toAppConfig(settings)
  } catch {
    // Defaults are the documented behaviour when settings are unavailable —
    // the dashboard still renders, just without operator overrides. Marked
    // loaded so consumers stop waiting on a value that isn't coming.
    return { ...defaultAppConfig, loaded: true }
  }
}

/**
 * Operator settings that affect rendering: distance units and the anchor
 * geometry used for rode/scope calculations.
 *
 * These live in settings.yaml and the Settings UI, and used to be compiled
 * into the bundle at build time — which meant a released binary ignored them
 * entirely and an operator who chose imperial still saw metric. They are now
 * read from the backend at runtime, starting from the defaults so nothing
 * blocks on the request.
 */
export function useAppConfig(): AppConfig {
  const [config, setConfig] = useState<AppConfig>(current)

  useEffect(() => {
    listeners.add(setConfig)

    // Re-sync on mount in case the value landed between render and effect.
    if (current !== config) setConfig(current)

    if (!current.loaded) {
      inflight ??= load()
      void inflight.then((next) => {
        // A save that lands mid-flight is newer than this response; don't
        // clobber it.
        if (!current.loaded) publish(next)
      })
    }

    return () => {
      listeners.delete(setConfig)
    }
    // Subscribe once per consumer; `config` is intentionally not a dependency.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  return config
}

/**
 * Applies a settings payload the app already has in hand — the response to a
 * successful `POST /api/settings` — so a save takes effect immediately rather
 * than after a reload. Avoids a redundant GET for data we were just handed.
 */
export function publishAppConfigSettings(settings: AppConfigSettings | null) {
  inflight = null
  publish(toAppConfig(settings))
}

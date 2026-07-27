import { useCallback, useEffect, useRef, useState } from 'react'

export type SecretKey =
  | 'SIGNALK_USERNAME'
  | 'SIGNALK_PASSWORD'
  | 'INFLUXDB_TOKEN'
  | 'GEONAMES_USERNAME'
  | 'WEATHERKIT_KEY_ID'
  | 'WEATHERKIT_TEAM_ID'
  | 'WEATHERKIT_SERVICE_ID'
  | 'WEATHERKIT_PRIVATE_KEY'

export const SECRET_KEYS: SecretKey[] = [
  'SIGNALK_USERNAME',
  'SIGNALK_PASSWORD',
  'INFLUXDB_TOKEN',
  'GEONAMES_USERNAME',
  'WEATHERKIT_KEY_ID',
  'WEATHERKIT_TEAM_ID',
  'WEATHERKIT_SERVICE_ID',
  'WEATHERKIT_PRIVATE_KEY',
]

const apiBaseUrl = import.meta.env.VITE_API_BASE_URL ?? `${window.location.protocol}//${window.location.hostname}:8080`

const emptyValues = (): Record<SecretKey, string> =>
  Object.fromEntries(SECRET_KEYS.map((key) => [key, ''])) as Record<SecretKey, string>

const emptyTouched = (): Record<SecretKey, boolean> =>
  Object.fromEntries(SECRET_KEYS.map((key) => [key, false])) as Record<SecretKey, boolean>

export interface UseSecretsStatusResult {
  status: Record<SecretKey, boolean>
  values: Record<SecretKey, string>
  touched: Record<SecretKey, boolean>
  loading: boolean
  error: string | null
  setFieldValue: (key: SecretKey, value: string) => void
  saveTouchedKeys: (keys: SecretKey[]) => Promise<Record<SecretKey, boolean>>
  clearKey: (key: SecretKey, label?: string) => Promise<Record<SecretKey, boolean> | null>
}

/**
 * Fetches `/api/settings/secrets` is-set status ONCE and owns the shared
 * in-progress edit state (`values`/`touched`) plus save/clear actions for
 * every secret field across the app. Meant to be instantiated once behind a
 * context provider (see settings-form-context.tsx / secrets-status-context)
 * so the 5+ consumers across the settings page (SignalK, InfluxDB, GeoNames
 * sections, and up to two provider-settings modals at once) don't each
 * independently refetch or track their own local edit state — callers
 * (the page-level "Save Settings" button, or a provider modal's own Save
 * button) decide WHEN a given set of touched keys actually gets persisted
 * via `saveTouchedKeys`.
 */
export function useSecretsStatus(): UseSecretsStatusResult {
  const [status, setStatus] = useState<Record<SecretKey, boolean>>(
    Object.fromEntries(SECRET_KEYS.map((key) => [key, false])) as Record<SecretKey, boolean>,
  )
  const [values, setValues] = useState<Record<SecretKey, string>>(emptyValues)
  const [touched, setTouched] = useState<Record<SecretKey, boolean>>(emptyTouched)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  // Ref-mirrored so saveTouchedKeys (a useCallback with empty deps, per the
  // same stale-closure-avoidance pattern as use-settings-form.ts's save())
  // always reads the latest in-memory values/touched without needing to be
  // recreated on every keystroke.
  const valuesRef = useRef(values)
  useEffect(() => {
    valuesRef.current = values
  }, [values])

  const touchedRef = useRef(touched)
  useEffect(() => {
    touchedRef.current = touched
  }, [touched])

  // Mirrors `status` too, so saveTouchedKeys's harmless-no-op return path
  // can read the latest status without needing `status` in its own
  // dependency array (which would otherwise recreate the callback on every
  // status change).
  const statusRef = useRef(status)
  useEffect(() => {
    statusRef.current = status
  }, [status])

  useEffect(() => {
    const fetchStatus = async () => {
      try {
        const response = await fetch(`${apiBaseUrl}/api/settings/secrets`)
        if (!response.ok) {
          throw new Error('Failed to fetch secrets')
        }
        const data = (await response.json()) as Record<SecretKey, boolean>
        setStatus(data)
      } catch (err) {
        setError(err instanceof Error ? err.message : 'Unable to load secrets')
      } finally {
        setLoading(false)
      }
    }

    void fetchStatus()
  }, [])

  // Shared POST logic used by both saveTouchedKeys and clearKey.
  const postSecretsPatch = useCallback(async (patch: Partial<Record<SecretKey, string>>) => {
    const response = await fetch(`${apiBaseUrl}/api/settings/secrets`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
      },
      body: JSON.stringify(patch),
    })

    if (!response.ok) {
      const errorPayload = (await response.json().catch(() => null)) as { error?: string } | null
      throw new Error(errorPayload?.error ?? 'Unable to save secrets')
    }

    const data = (await response.json()) as Record<SecretKey, boolean>
    setStatus(data)
    return data
  }, [])

  const setFieldValue = useCallback((key: SecretKey, value: string) => {
    setValues((previous) => ({ ...previous, [key]: value }))
    setTouched((previous) => ({ ...previous, [key]: true }))
  }, [])

  const saveTouchedKeys = useCallback(
    async (keys: SecretKey[]) => {
      const patch: Partial<Record<SecretKey, string>> = {}
      for (const key of keys) {
        if (touchedRef.current[key]) {
          patch[key] = valuesRef.current[key]
        }
      }

      if (Object.keys(patch).length === 0) {
        // Nothing in this key list is touched — harmless no-op, no network
        // call. Mirrors SecretFieldGroup's previous "nothing touched, don't
        // save" behavior, generalized to an explicit key list.
        return statusRef.current
      }

      const data = await postSecretsPatch(patch)

      const savedKeys = Object.keys(patch) as SecretKey[]
      setValues((previous) => {
        const next = { ...previous }
        for (const key of savedKeys) next[key] = ''
        return next
      })
      setTouched((previous) => {
        const next = { ...previous }
        for (const key of savedKeys) next[key] = false
        return next
      })

      return data
    },
    [postSecretsPatch],
  )

  const clearKey = useCallback(
    async (key: SecretKey, label?: string) => {
      const humanName = label ?? key
      if (!window.confirm(`Clear ${humanName}? This cannot be undone.`)) {
        return null
      }

      const data = await postSecretsPatch({ [key]: '' })

      setValues((previous) => ({ ...previous, [key]: '' }))
      setTouched((previous) => ({ ...previous, [key]: false }))

      return data
    },
    [postSecretsPatch],
  )

  return { status, values, touched, loading, error, setFieldValue, saveTouchedKeys, clearKey }
}

import { useCallback, useEffect, useRef, useState } from 'react'
import { apiRequest, setQueryData, useApiQuery } from '@/lib/api-query'

export type SecretKey =
  | 'SIGNALK_USERNAME'
  | 'SIGNALK_PASSWORD'
  | 'INFLUXDB_TOKEN'
  | 'WEATHERKIT_KEY_ID'
  | 'WEATHERKIT_TEAM_ID'
  | 'WEATHERKIT_SERVICE_ID'
  | 'WEATHERKIT_PRIVATE_KEY'
  | 'GOOGLE_PLACES_API_KEY'
  | 'OPENROUTER_API_KEY'

export const SECRET_KEYS: SecretKey[] = [
  'SIGNALK_USERNAME',
  'SIGNALK_PASSWORD',
  'INFLUXDB_TOKEN',
  'WEATHERKIT_KEY_ID',
  'WEATHERKIT_TEAM_ID',
  'WEATHERKIT_SERVICE_ID',
  'WEATHERKIT_PRIVATE_KEY',
  'GOOGLE_PLACES_API_KEY',
  'OPENROUTER_API_KEY',
]

const SECRETS_PATH = '/api/settings/secrets'
const NO_STATUS = Object.fromEntries(SECRET_KEYS.map((key) => [key, false])) as Record<SecretKey, boolean>

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
  /** Drops every in-progress edit (values and touched flags) without writing anything - the Save bar's Discard. */
  resetTouched: () => void
}

/**
 * Fetches `/api/settings/secrets` is-set status ONCE and owns the shared
 * in-progress edit state (`values`/`touched`) plus save/clear actions for
 * every secret field across the app. Meant to be instantiated once behind a
 * context provider (see settings-form-context.tsx / secrets-status-context)
 * so the several consumers across the settings page (SignalK, InfluxDB
 * sections, and up to two provider-settings modals at once) don't each
 * independently refetch or track their own local edit state — callers
 * (the page-level Save bar, or a provider modal's own Save
 * button) decide WHEN a given set of touched keys actually gets persisted
 * via `saveTouchedKeys`.
 */
export function useSecretsStatus(): UseSecretsStatusResult {
  const { data, isLoading: loading, error: queryError } = useApiQuery<Record<SecretKey, boolean>>({
    queryKey: [SECRETS_PATH],
    path: SECRETS_PATH,
  })
  const status = data ?? NO_STATUS
  const [values, setValues] = useState<Record<SecretKey, string>>(emptyValues)
  const [touched, setTouched] = useState<Record<SecretKey, boolean>>(emptyTouched)
  const error = queryError?.message ?? null

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

  // Shared POST logic used by both saveTouchedKeys and clearKey.
  const postSecretsPatch = useCallback(async (patch: Partial<Record<SecretKey, string>>) => {
    const next = await apiRequest<Record<SecretKey, boolean>>(SECRETS_PATH, { method: 'POST', json: patch })
    setQueryData<Record<SecretKey, boolean>>([SECRETS_PATH], next)
    return next
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

  const resetTouched = useCallback(() => {
    setValues(emptyValues())
    setTouched(emptyTouched())
  }, [])

  return { status, values, touched, loading, error, setFieldValue, saveTouchedKeys, clearKey, resetTouched }
}

import { useCallback, useEffect, useState } from 'react'

/** Secrets the transports need. Stored encrypted server-side, never echoed back. */
export const TRANSPORT_SECRET_KEYS = ['NTFY_TOKEN', 'SMTP_PASSWORD'] as const
export type TransportSecretKey = (typeof TRANSPORT_SECRET_KEYS)[number]

export interface AlarmTransportConfig {
  ntfy: { enabled: boolean; server: string; topic: string }
  smtp: { enabled: boolean; host: string; port: number; username: string; from: string; to: string[] | null }
  webhook: { enabled: boolean; url: string }
  signalk: { enabled: boolean }
  watchdog: { stream_silence_seconds: number; heartbeat_minutes: number }
}

export function emptyTransportConfig(): AlarmTransportConfig {
  return {
    ntfy: { enabled: false, server: '', topic: '' },
    smtp: { enabled: false, host: '', port: 587, username: '', from: '', to: [] },
    webhook: { enabled: false, url: '' },
    signalk: { enabled: false },
    watchdog: { stream_silence_seconds: 0, heartbeat_minutes: 0 },
  }
}

export function useAlarmTransports() {
  const [config, setConfig] = useState<AlarmTransportConfig>(emptyTransportConfig)
  const [secretsPresent, setSecretsPresent] = useState<Record<string, boolean>>({})
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [testResults, setTestResults] = useState<Record<string, string> | null>(null)
  const [testing, setTesting] = useState(false)

  const refresh = useCallback(async () => {
    try {
      const [configResponse, secretsResponse] = await Promise.all([
        fetch('/api/alarm-transports'),
        fetch('/api/settings/secrets'),
      ])
      if (configResponse.ok) {
        const payload = (await configResponse.json()) as AlarmTransportConfig
        setConfig({ ...emptyTransportConfig(), ...payload })
      }
      if (secretsResponse.ok) {
        setSecretsPresent((await secretsResponse.json()) as Record<string, boolean>)
      }
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { void refresh() }, [refresh])

  const save = useCallback(async (next: AlarmTransportConfig, secrets: Partial<Record<TransportSecretKey, string>>) => {
    // Secrets first: saving a transport that needs a token before the token
    // exists would leave a window where it is enabled and cannot authenticate.
    const entries = Object.entries(secrets).filter(([, value]) => value.trim() !== '')
    if (entries.length > 0) {
      const response = await fetch('/api/settings/secrets', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(Object.fromEntries(entries)),
      })
      if (!response.ok) {
        const payload = (await response.json().catch(() => ({}))) as { error?: string }
        throw new Error(payload.error ?? 'Could not save credentials')
      }
    }

    const response = await fetch('/api/alarm-transports', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(next),
    })
    if (!response.ok) {
      const payload = (await response.json().catch(() => ({}))) as { error?: string }
      throw new Error(payload.error ?? `HTTP ${response.status}`)
    }
    await refresh()
  }, [refresh])

  /**
   * Probes every enabled transport. Discovering at 3am that the ntfy topic was
   * mistyped is the failure this exists to prevent.
   */
  const test = useCallback(async () => {
    setTesting(true)
    setTestResults(null)
    try {
      const response = await fetch('/api/alarm-transports/test', { method: 'POST' })
      const payload = (await response.json().catch(() => ({}))) as { results?: Record<string, string>; error?: string }
      if (!response.ok) {
        setTestResults({ error: payload.error ?? `HTTP ${response.status}` })
        return
      }
      setTestResults(payload.results ?? {})
    } catch (err) {
      setTestResults({ error: err instanceof Error ? err.message : String(err) })
    } finally {
      setTesting(false)
    }
  }, [])

  return { config, secretsPresent, loading, error, save, test, testResults, testing }
}

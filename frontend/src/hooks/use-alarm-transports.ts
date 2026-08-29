import { useCallback, useEffect, useState } from 'react'

/** Secrets the transports need. Stored encrypted server-side, never echoed back. */
export const TRANSPORT_SECRET_KEYS = ['NTFY_TOKEN', 'SMTP_PASSWORD'] as const
export type TransportSecretKey = (typeof TRANSPORT_SECRET_KEYS)[number]

export interface AlarmTransportConfig {
  ntfy: { enabled: boolean; server: string; topic: string }
  smtp: { enabled: boolean; host: string; port: number; username: string; from: string; to: string[] | null }
  webhook: { enabled: boolean; url: string }
  signalk: { enabled: boolean }
  webpush: { enabled: boolean }
  watchdog: { stream_silence_seconds: number; heartbeat_minutes: number }
}

export function emptyTransportConfig(): AlarmTransportConfig {
  return {
    ntfy: { enabled: false, server: '', topic: '' },
    smtp: { enabled: false, host: '', port: 587, username: '', from: '', to: [] },
    webhook: { enabled: false, url: '' },
    signalk: { enabled: false },
    webpush: { enabled: false },
    watchdog: { stream_silence_seconds: 0, heartbeat_minutes: 0 },
  }
}

/**
 * Field-by-field so an added key is a type error rather than a silently
 * missed comparison. Drives the settings page's dirty indicator, which is
 * what gates its navigation guard — a wrong answer here either strands an
 * edit or nags about one that was never made.
 */
export function transportConfigsEqual(a: AlarmTransportConfig, b: AlarmTransportConfig): boolean {
  if (a.ntfy.enabled !== b.ntfy.enabled) return false
  if (a.ntfy.server !== b.ntfy.server) return false
  if (a.ntfy.topic !== b.ntfy.topic) return false
  if (a.smtp.enabled !== b.smtp.enabled) return false
  if (a.smtp.host !== b.smtp.host) return false
  if (a.smtp.port !== b.smtp.port) return false
  if (a.smtp.username !== b.smtp.username) return false
  if (a.smtp.from !== b.smtp.from) return false
  const aTo = a.smtp.to ?? []
  const bTo = b.smtp.to ?? []
  if (aTo.length !== bTo.length || aTo.some((value, i) => value !== bTo[i])) return false
  if (a.webhook.enabled !== b.webhook.enabled) return false
  if (a.webhook.url !== b.webhook.url) return false
  if (a.signalk.enabled !== b.signalk.enabled) return false
  if (a.webpush.enabled !== b.webpush.enabled) return false
  if (a.watchdog.stream_silence_seconds !== b.watchdog.stream_silence_seconds) return false
  if (a.watchdog.heartbeat_minutes !== b.watchdog.heartbeat_minutes) return false
  return true
}

export function useAlarmTransports() {
  const [config, setConfig] = useState<AlarmTransportConfig>(emptyTransportConfig)
  const [secretsPresent, setSecretsPresent] = useState<Record<string, boolean>>({})
  const [loading, setLoading] = useState(true)
  // "Is there a server copy under this config", which is not the same
  // question as "did the last request work". A refresh that fails after a
  // good load leaves the last good config in place and stays loaded, so the
  // settings page can still save against it; a load that never succeeded
  // must not be saved from, because the draft would be an empty config
  // standing in for whatever the server actually holds.
  const [loaded, setLoaded] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [testResults, setTestResults] = useState<Record<string, string> | null>(null)
  const [testing, setTesting] = useState(false)

  const refresh = useCallback(async () => {
    try {
      const [configResponse, secretsResponse] = await Promise.all([
        fetch('/api/alarm-transports'),
        fetch('/api/settings/secrets'),
      ])
      // A non-ok response used to be dropped: config stayed at
      // emptyTransportConfig() with error null, which reads as "loaded, and
      // nothing is configured" and is indistinguishable from that being the
      // truth. The fallback policy rules that out.
      if (!configResponse.ok) throw new Error(`alarm transports: HTTP ${configResponse.status}`)
      if (!secretsResponse.ok) throw new Error(`secrets status: HTTP ${secretsResponse.status}`)
      const payload = (await configResponse.json()) as AlarmTransportConfig
      setConfig({ ...emptyTransportConfig(), ...payload })
      setSecretsPresent((await secretsResponse.json()) as Record<string, boolean>)
      setLoaded(true)
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

  return { config, secretsPresent, loading, loaded, error, save, test, testResults, testing }
}

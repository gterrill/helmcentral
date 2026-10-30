import { useCallback, useState } from 'react'

import { apiRequest, useApiQuery } from '@/lib/api-query'
import { errorMessage } from '@/lib/errors'

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
  /** Where Helmcentral is reached from off the boat; notifications about a watch link to it. Empty means no link. */
  link_address: string
}

export function emptyTransportConfig(): AlarmTransportConfig {
  return {
    ntfy: { enabled: false, server: '', topic: '' },
    smtp: { enabled: false, host: '', port: 587, username: '', from: '', to: [] },
    webhook: { enabled: false, url: '' },
    signalk: { enabled: false },
    webpush: { enabled: false },
    watchdog: { stream_silence_seconds: 0, heartbeat_minutes: 0 },
    link_address: '',
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
  if (a.link_address !== b.link_address) return false
  return true
}

const CONFIG_PATH = '/api/alarm-transports'
const SECRETS_PATH = '/api/settings/secrets'
const EMPTY_CONFIG = emptyTransportConfig()
const NO_SECRETS: Record<string, boolean> = {}

export function useAlarmTransports() {
  const configQuery = useApiQuery<AlarmTransportConfig>({
    queryKey: [CONFIG_PATH],
    path: CONFIG_PATH,
    select: (payload: AlarmTransportConfig) => ({ ...emptyTransportConfig(), ...payload }),
  })
  const secretsQuery = useApiQuery<Record<string, boolean>>({
    queryKey: [SECRETS_PATH],
    path: SECRETS_PATH,
  })
  const [testResults, setTestResults] = useState<Record<string, string> | null>(null)
  const [testing, setTesting] = useState(false)

  // "Is there a server copy under this config", which is not the same
  // question as "did the last request work". A refresh that fails after a
  // good load leaves the last good config in place and stays loaded, so the
  // settings page can still save against it; a load that never succeeded
  // must not be saved from, because the draft would be an empty config
  // standing in for whatever the server actually holds.
  const loaded = configQuery.data !== undefined && secretsQuery.data !== undefined
  const loading = configQuery.isLoading || secretsQuery.isLoading
  const error = (configQuery.error ?? secretsQuery.error)?.message ?? null

  const { refetch: refetchConfig } = configQuery
  const { refetch: refetchSecrets } = secretsQuery

  const save = useCallback(async (next: AlarmTransportConfig, secrets: Partial<Record<TransportSecretKey, string>>) => {
    // Secrets first: saving a transport that needs a token before the token
    // exists would leave a window where it is enabled and cannot authenticate.
    const entries = Object.entries(secrets).filter(([, value]) => value.trim() !== '')
    if (entries.length > 0) {
      await apiRequest(SECRETS_PATH, { method: 'POST', json: Object.fromEntries(entries) })
    }
    await apiRequest(CONFIG_PATH, { method: 'POST', json: next })
    await Promise.all([refetchConfig(), refetchSecrets()])
  }, [refetchConfig, refetchSecrets])

  /**
   * Probes every enabled transport. Discovering at 3am that the ntfy topic was
   * mistyped is the failure this exists to prevent.
   */
  const test = useCallback(async () => {
    setTesting(true)
    setTestResults(null)
    try {
      const payload = await apiRequest<{ results?: Record<string, string> }>(`${CONFIG_PATH}/test`, { method: 'POST' })
      setTestResults(payload.results ?? {})
    } catch (err) {
      setTestResults({ error: errorMessage(err) })
    } finally {
      setTesting(false)
    }
  }, [])

  return {
    config: configQuery.data ?? EMPTY_CONFIG,
    secretsPresent: secretsQuery.data ?? NO_SECRETS,
    loading,
    loaded,
    error,
    save,
    test,
    testResults,
    testing,
  }
}

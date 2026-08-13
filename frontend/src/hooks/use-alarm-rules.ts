import { useCallback, useEffect, useState } from 'react'

import type { AlarmState } from '@/hooks/use-alarms'

export const ALARM_OPERATORS = ['above', 'below', 'equal', 'notEqual', 'stale'] as const
export type AlarmOperator = (typeof ALARM_OPERATORS)[number]

/** Severities a rule can raise. `normal` is the cleared state, so it is absent. */
export const RAISABLE_ALARM_STATES: AlarmState[] = ['alert', 'warn', 'alarm', 'emergency']

export interface AlarmRule {
  id: string
  enabled: boolean
  path: string
  label: string
  op: AlarmOperator
  value: number
  hysteresis: number
  dwell_seconds: number
  stale_after_seconds: number
  state: AlarmState
  methods: string[]
  notify: string[] | null
  escalate_after_seconds: number
  created_at?: string
  updated_at?: string
}

export type AlarmRuleDraft = Omit<AlarmRule, 'id' | 'created_at' | 'updated_at'>

export function newAlarmRuleDraft(): AlarmRuleDraft {
  return {
    enabled: true,
    path: '',
    label: '',
    op: 'below',
    value: 0,
    // Defaults that stop a threshold-hugging value producing an alarm storm,
    // which is the most common reason people switch marine alarms off.
    hysteresis: 0,
    dwell_seconds: 10,
    stale_after_seconds: 0,
    state: 'alarm',
    methods: ['visual', 'sound'],
    notify: null,
    escalate_after_seconds: 0,
  }
}

export function useAlarmRules() {
  const [rules, setRules] = useState<AlarmRule[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const refresh = useCallback(async () => {
    try {
      const response = await fetch('/api/alarm-rules')
      if (!response.ok) throw new Error(`HTTP ${response.status}`)
      const payload = (await response.json()) as { rules?: AlarmRule[] }
      setRules(Array.isArray(payload.rules) ? payload.rules : [])
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { void refresh() }, [refresh])

  // The backend validates and is the authority; surface its message verbatim
  // rather than guessing at a friendlier one that might not be true.
  const submit = useCallback(async (method: string, url: string, body?: unknown) => {
    const response = await fetch(url, {
      method,
      headers: body ? { 'Content-Type': 'application/json' } : undefined,
      body: body ? JSON.stringify(body) : undefined,
    })
    if (!response.ok) {
      const payload = (await response.json().catch(() => ({}))) as { error?: string }
      throw new Error(payload.error ?? `HTTP ${response.status}`)
    }
    await refresh()
  }, [refresh])

  const createRule = useCallback((draft: AlarmRuleDraft) => submit('POST', '/api/alarm-rules', draft), [submit])
  const updateRule = useCallback((id: string, draft: AlarmRuleDraft) =>
    submit('PUT', `/api/alarm-rules/${encodeURIComponent(id)}`, draft), [submit])
  const deleteRule = useCallback((id: string) =>
    submit('DELETE', `/api/alarm-rules/${encodeURIComponent(id)}`), [submit])

  return { rules, loading, error, refresh, createRule, updateRule, deleteRule }
}

export interface AlarmLogEntry {
  id: string
  rule_id: string
  source: string
  label: string
  path: string
  state: AlarmState
  message: string
  value_at_raise: number
  raised_at: string
  acked_at?: string
  cleared_at?: string
}

export function useAlarmLog(enabled: boolean) {
  const [entries, setEntries] = useState<AlarmLogEntry[]>([])

  const refresh = useCallback(async () => {
    try {
      const response = await fetch('/api/alarms/log')
      if (!response.ok) return
      const payload = (await response.json()) as { entries?: AlarmLogEntry[] }
      setEntries(Array.isArray(payload.entries) ? payload.entries : [])
    } catch {
      // History is a nice-to-have; a failure here must not blank the page.
    }
  }, [])

  useEffect(() => {
    if (!enabled) return
    void refresh()
  }, [enabled, refresh])

  return { entries, refresh }
}

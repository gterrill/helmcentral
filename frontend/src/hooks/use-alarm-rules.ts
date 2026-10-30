import { useCallback } from 'react'

import { apiRequest, declareQuery, useApiQuery, useApiQuerySelector } from '@/lib/api-query'
import { sameJSON } from '@/lib/same-value'
import type { AlarmState } from '@/hooks/use-alarms'

export const ALARM_OPERATORS = ['above', 'below', 'equal', 'notEqual', 'stale'] as const
export type AlarmOperator = (typeof ALARM_OPERATORS)[number]

/** Severities a rule can raise. `normal` is the cleared state, so it is absent. */
export const RAISABLE_ALARM_STATES: AlarmState[] = ['alert', 'warn', 'alarm', 'emergency']

export interface AlarmRule {
  id: string
  enabled: boolean
  path: string
  /** 'house_bank_soc' when the server keeps `path` on the house bank's state of charge; absent for a literal path. */
  path_role?: string
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
  /** Whether Mate reviews the alarm when it raises. Off unless the operator turns it on. */
  mate_reviews?: boolean
  /**
   * True for a rule derived from a gauge zone (ADR 0050) or a tank's levels (ADR 0214). It has no stored
   * counterpart, so it cannot be edited or deleted here — the threshold lives
   * on the gauge that owns the zone.
   */
  derived?: boolean
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
    mate_reviews: false,
  }
}

export const ALARM_RULES_PATH = '/api/alarm-rules'
const RULES_PATH = ALARM_RULES_PATH
const NO_RULES: AlarmRule[] = []

// A poll that answers the same rules keeps the same object, for the editor and the list.
declareQuery([RULES_PATH], { structuralSharing: true })

const selectRuleList = (payload: { rules?: AlarmRule[] } | undefined) => (Array.isArray(payload?.rules) ? payload.rules : NO_RULES)

/**
 * The rule list alone, for a reader that shows something derived from it (the
 * battery tile's SoC bands). It shares the Alarms page's query, so a saved rule
 * reaches it without a reload, and keeps its list while a refetch answers with
 * equal rules, which every revalidation on mount does.
 */
export function useAlarmRuleList(): AlarmRule[] {
  return useApiQuerySelector({ queryKey: [RULES_PATH], path: RULES_PATH }, selectRuleList, sameJSON)
}

export function useAlarmRules() {
  const { data, isLoading, error, refetch } = useApiQuery<AlarmRule[]>({
    queryKey: [RULES_PATH],
    path: RULES_PATH,
    select: (payload: { rules?: AlarmRule[] }) => (Array.isArray(payload.rules) ? payload.rules : NO_RULES),
  })

  // The backend validates and is the authority; its message surfaces verbatim
  // rather than a friendlier guess that might not be true.
  const submit = useCallback(async (method: string, url: string, json?: unknown) => {
    await apiRequest(url, { method, json })
    await refetch()
  }, [refetch])

  const createRule = useCallback((draft: AlarmRuleDraft) => submit('POST', RULES_PATH, draft), [submit])
  const updateRule = useCallback((id: string, draft: AlarmRuleDraft) =>
    submit('PUT', `${RULES_PATH}/${encodeURIComponent(id)}`, draft), [submit])
  const deleteRule = useCallback((id: string) =>
    submit('DELETE', `${RULES_PATH}/${encodeURIComponent(id)}`), [submit])

  return {
    rules: data ?? NO_RULES,
    loading: isLoading,
    error: error?.message ?? null,
    refresh: refetch,
    createRule,
    updateRule,
    deleteRule,
  }
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

const NO_ENTRIES: AlarmLogEntry[] = []

export function useAlarmLog(enabled: boolean) {
  // History is a nice-to-have; a failure here must not blank the page, so the
  // error is not surfaced.
  const { data, refetch } = useApiQuery<AlarmLogEntry[]>({
    queryKey: ['/api/alarms/log'],
    path: '/api/alarms/log',
    enabled,
    select: (payload: { entries?: AlarmLogEntry[] }) => (Array.isArray(payload.entries) ? payload.entries : NO_ENTRIES),
  })

  return { entries: data ?? NO_ENTRIES, refresh: refetch }
}

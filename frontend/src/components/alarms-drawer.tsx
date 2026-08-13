import { BellRing, Pencil, Plus, Trash2, TriangleAlert } from 'lucide-react'
import { memo, useCallback, useEffect, useState } from 'react'

import { Button } from '@/components/ui/button'
import { Field, FieldError, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Tile } from '@/components/ui/tile'
import {
  ALARM_OPERATORS,
  RAISABLE_ALARM_STATES,
  newAlarmRuleDraft,
  useAlarmLog,
  useAlarmRules,
  type AlarmOperator,
  type AlarmRule,
  type AlarmRuleDraft,
} from '@/hooks/use-alarm-rules'
import type { ActiveAlarm, AlarmState } from '@/hooks/use-alarms'

/**
 * Severity colours. Raw palette colours are reserved for alert semantics
 * (AGENTS.md), which is exactly what these are.
 */
function stateClass(state: AlarmState | string): string {
  switch (state) {
    case 'emergency':
      return 'text-red-600'
    case 'alarm':
      return 'text-red-500'
    case 'warn':
      return 'text-amber-500'
    case 'alert':
      return 'text-amber-400'
    default:
      return 'text-muted-foreground'
  }
}

function formatTime(value?: string): string {
  if (!value) return '--'
  const parsed = new Date(value)
  return Number.isNaN(parsed.getTime()) ? '--' : parsed.toLocaleString()
}

interface AlarmsDrawerProps {
  alarms: ActiveAlarm[]
  onAcknowledge: (ruleId: string) => Promise<void>
}

export const AlarmsDrawer = memo(function AlarmsDrawer({ alarms, onAcknowledge }: AlarmsDrawerProps) {
  const { rules, loading, error, createRule, updateRule, deleteRule } = useAlarmRules()
  const { entries, refresh: refreshLog } = useAlarmLog(true)
  const [draft, setDraft] = useState<AlarmRuleDraft | null>(null)
  const [editingId, setEditingId] = useState<string | null>(null)
  const [formError, setFormError] = useState<string | null>(null)
  const [ackError, setAckError] = useState<string | null>(null)

  // Acknowledging or clearing changes history, so keep the log in step.
  useEffect(() => { void refreshLog() }, [alarms.length, refreshLog])

  const startCreate = useCallback(() => {
    setEditingId(null)
    setFormError(null)
    setDraft(newAlarmRuleDraft())
  }, [])

  const startEdit = useCallback((rule: AlarmRule) => {
    setEditingId(rule.id)
    setFormError(null)
    // The server owns id and timestamps; the draft carries only editable fields.
    setDraft({
      enabled: rule.enabled,
      path: rule.path,
      label: rule.label,
      op: rule.op,
      value: rule.value,
      hysteresis: rule.hysteresis,
      dwell_seconds: rule.dwell_seconds,
      stale_after_seconds: rule.stale_after_seconds,
      state: rule.state,
      methods: rule.methods,
      notify: rule.notify,
      escalate_after_seconds: rule.escalate_after_seconds,
    })
  }, [])

  const save = useCallback(async () => {
    if (!draft) return
    setFormError(null)
    try {
      if (editingId) {
        await updateRule(editingId, draft)
      } else {
        await createRule(draft)
      }
      setDraft(null)
      setEditingId(null)
    } catch (err) {
      setFormError(err instanceof Error ? err.message : String(err))
    }
  }, [draft, editingId, createRule, updateRule])

  const acknowledge = useCallback(async (ruleId: string) => {
    setAckError(null)
    try {
      await onAcknowledge(ruleId)
    } catch (err) {
      setAckError(err instanceof Error ? err.message : String(err))
    }
  }, [onAcknowledge])

  return (
    <div className="mx-auto flex w-full max-w-5xl flex-col gap-4">
      <Tile title="Active Alarms" icon={<BellRing className="h-3.5 w-3.5 text-gauge-secondary" />}>
        {ackError && (
          <p className="mb-2 text-[11px] text-destructive">{ackError}</p>
        )}
        {alarms.length === 0 ? (
          <p className="text-[11px] uppercase tracking-[0.16em] text-muted-foreground">All clear</p>
        ) : (
          <div className="flex flex-col gap-2">
            {alarms.map((alarm) => (
              <div key={alarm.rule_id} className="rounded-md border bg-background/60 px-3 py-3">
                <div className="flex min-w-0 items-start justify-between gap-3">
                  <div className="min-w-0">
                    <p className={`font-display text-lg leading-none ${stateClass(alarm.state)}`}>
                      <span className="truncate">{alarm.label}</span>
                    </p>
                    <p className="mt-1 truncate text-[11px] text-muted-foreground">{alarm.message}</p>
                    <p className="mt-1 truncate text-[10px] uppercase tracking-[0.16em] text-muted-foreground">
                      {alarm.state} · {alarm.path} · {formatTime(alarm.raised_at)}
                    </p>
                  </div>
                  <div className="shrink-0">
                    {alarm.phase === 'acknowledged' ? (
                      <span className="text-[10px] uppercase tracking-[0.16em] text-muted-foreground">Acknowledged</span>
                    ) : (
                      <Button size="sm" variant="outline" onClick={() => void acknowledge(alarm.rule_id)}>
                        Acknowledge
                      </Button>
                    )}
                  </div>
                </div>
              </div>
            ))}
          </div>
        )}
      </Tile>

      <Tile
        title="Rules"
        icon={<TriangleAlert className="h-3.5 w-3.5 text-gauge-secondary" />}
        titleExtra={
          <Button size="sm" variant="outline" onClick={startCreate}>
            <Plus className="size-3.5" /> Add Rule
          </Button>
        }
      >
        {error && <p className="mb-2 text-[11px] text-destructive">{error}</p>}
        {loading ? (
          <p className="text-[11px] uppercase tracking-[0.16em] text-muted-foreground">Loading…</p>
        ) : rules.length === 0 ? (
          <p className="text-[11px] uppercase tracking-[0.16em] text-muted-foreground">
            No rules configured
          </p>
        ) : (
          <div className="flex flex-col gap-2">
            {rules.map((rule) => (
              <div key={rule.id} className="flex min-w-0 items-center justify-between gap-3 rounded-md border bg-background/60 px-3 py-2">
                <div className="min-w-0">
                  <p className="truncate text-sm">
                    {rule.label}
                    {!rule.enabled && (
                      <span className="ml-2 text-[10px] uppercase tracking-[0.16em] text-muted-foreground">Disabled</span>
                    )}
                  </p>
                  <p className="truncate text-[10px] uppercase tracking-[0.16em] text-muted-foreground">
                    {rule.path} · {rule.op} {rule.op === 'stale' ? `${rule.stale_after_seconds}s` : rule.value} ·{' '}
                    <span className={stateClass(rule.state)}>{rule.state}</span>
                  </p>
                </div>
                <div className="flex shrink-0 gap-1">
                  <Button size="sm" variant="ghost" onClick={() => startEdit(rule)} aria-label={`Edit ${rule.label}`}>
                    <Pencil className="size-3.5" />
                  </Button>
                  <Button size="sm" variant="ghost" onClick={() => void deleteRule(rule.id)} aria-label={`Delete ${rule.label}`}>
                    <Trash2 className="size-3.5" />
                  </Button>
                </div>
              </div>
            ))}
          </div>
        )}

        {draft && (
          <RuleForm
            draft={draft}
            error={formError}
            isEditing={editingId !== null}
            onChange={setDraft}
            onCancel={() => { setDraft(null); setEditingId(null); setFormError(null) }}
            onSave={() => void save()}
          />
        )}
      </Tile>

      <Tile title="History" icon={<BellRing className="h-3.5 w-3.5 text-gauge-secondary" />}>
        {entries.length === 0 ? (
          <p className="text-[11px] uppercase tracking-[0.16em] text-muted-foreground">No alarms recorded</p>
        ) : (
          <div className="flex flex-col gap-1">
            {entries.slice(0, 25).map((entry) => (
              <div key={entry.id} className="flex min-w-0 items-baseline justify-between gap-3 border-b py-1 last:border-b-0">
                <span className="min-w-0 truncate text-[11px]">
                  <span className={stateClass(entry.state)}>{entry.state}</span>
                  {' · '}
                  {entry.label}
                  {entry.source === 'signalk' && (
                    <span className="ml-1 text-[10px] uppercase tracking-[0.16em] text-muted-foreground">signalk</span>
                  )}
                </span>
                <span className="shrink-0 text-[10px] tabular-nums text-muted-foreground">
                  {formatTime(entry.raised_at)}
                  {entry.cleared_at ? ' → cleared' : ''}
                </span>
              </div>
            ))}
          </div>
        )}
      </Tile>
    </div>
  )
})

interface RuleFormProps {
  draft: AlarmRuleDraft
  error: string | null
  isEditing: boolean
  onChange: (draft: AlarmRuleDraft) => void
  onCancel: () => void
  onSave: () => void
}

function RuleForm({ draft, error, isEditing, onChange, onCancel, onSave }: RuleFormProps) {
  const set = <K extends keyof AlarmRuleDraft>(key: K, value: AlarmRuleDraft[K]) =>
    onChange({ ...draft, [key]: value })

  const isStale = draft.op === 'stale'

  return (
    <div className="mt-3 rounded-md border bg-background/60 p-3">
      <div className="grid gap-3 sm:grid-cols-2">
        <Field>
          <FieldLabel htmlFor="alarm-label">Label</FieldLabel>
          <Input id="alarm-label" value={draft.label} onChange={(e) => set('label', e.target.value)} placeholder="House bank low" />
        </Field>
        <Field>
          <FieldLabel htmlFor="alarm-path">SignalK path</FieldLabel>
          <Input
            id="alarm-path"
            value={draft.path}
            onChange={(e) => set('path', e.target.value)}
            placeholder="electrical.batteries.house.voltage"
          />
        </Field>
        <Field>
          <FieldLabel htmlFor="alarm-op">Condition</FieldLabel>
          <select
            id="alarm-op"
            className="h-9 w-full rounded-md border bg-transparent px-3 text-sm"
            value={draft.op}
            onChange={(e) => set('op', e.target.value as AlarmOperator)}
          >
            {ALARM_OPERATORS.map((op) => <option key={op} value={op}>{op}</option>)}
          </select>
        </Field>
        {isStale ? (
          <Field>
            <FieldLabel htmlFor="alarm-stale">No data for (seconds)</FieldLabel>
            <Input
              id="alarm-stale"
              type="number"
              value={draft.stale_after_seconds}
              onChange={(e) => set('stale_after_seconds', Number(e.target.value))}
            />
          </Field>
        ) : (
          <Field>
            <FieldLabel htmlFor="alarm-value">Threshold</FieldLabel>
            <Input id="alarm-value" type="number" step="any" value={draft.value} onChange={(e) => set('value', Number(e.target.value))} />
          </Field>
        )}
        <Field>
          <FieldLabel htmlFor="alarm-state">Severity</FieldLabel>
          <select
            id="alarm-state"
            className="h-9 w-full rounded-md border bg-transparent px-3 text-sm"
            value={draft.state}
            onChange={(e) => set('state', e.target.value as AlarmRuleDraft['state'])}
          >
            {RAISABLE_ALARM_STATES.map((state) => <option key={state} value={state}>{state}</option>)}
          </select>
        </Field>
        {!isStale && (
          <Field>
            <FieldLabel htmlFor="alarm-hysteresis">Deadband</FieldLabel>
            <Input
              id="alarm-hysteresis"
              type="number"
              step="any"
              value={draft.hysteresis}
              onChange={(e) => set('hysteresis', Number(e.target.value))}
            />
            <p className="text-[10px] text-muted-foreground">How far back past the threshold before it clears.</p>
          </Field>
        )}
        <Field>
          <FieldLabel htmlFor="alarm-dwell">Must hold for (seconds)</FieldLabel>
          <Input
            id="alarm-dwell"
            type="number"
            value={draft.dwell_seconds}
            onChange={(e) => set('dwell_seconds', Number(e.target.value))}
          />
        </Field>
        <Field>
          <FieldLabel htmlFor="alarm-escalate">Escalate after (seconds, 0 = never)</FieldLabel>
          <Input
            id="alarm-escalate"
            type="number"
            value={draft.escalate_after_seconds}
            onChange={(e) => set('escalate_after_seconds', Number(e.target.value))}
          />
        </Field>
      </div>

      <label className="mt-3 flex items-center gap-2 text-[11px] uppercase tracking-[0.16em] text-muted-foreground">
        <input type="checkbox" checked={draft.enabled} onChange={(e) => set('enabled', e.target.checked)} />
        Enabled
      </label>

      {error && <FieldError className="mt-2">{error}</FieldError>}

      <div className="mt-3 flex justify-end gap-2">
        <Button size="sm" variant="ghost" onClick={onCancel}>Cancel</Button>
        <Button size="sm" onClick={onSave}>{isEditing ? 'Save Rule' : 'Create Rule'}</Button>
      </div>
    </div>
  )
}

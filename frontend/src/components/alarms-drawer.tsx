import { BellRing, Pencil, Plus, Trash2, TriangleAlert } from 'lucide-react'
import { memo, useCallback, useEffect, useMemo, useState } from 'react'

import { Button } from '@/components/ui/button'
import { Field, FieldError, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Tile } from '@/components/ui/tile'
import { alarmConditionSentence, formatAlarmReading, formatAlarmTime } from '@/lib/alarm-display'
import { groupRulesByDomain } from '@/lib/alarm-rules-view'
import { severityClass } from '@/lib/severity'
import {
  ALARM_OPERATORS,
  RAISABLE_ALARM_STATES,
  newAlarmRuleDraft,
  useAlarmLog,
  type AlarmOperator,
  type AlarmRule,
  type AlarmRuleDraft,
} from '@/hooks/use-alarm-rules'
import { COLLISION_ALARM_RULE_PREFIX, type ActiveAlarm } from '@/hooks/use-alarms'
import { useSignalKPaths } from '@/hooks/use-signalk-paths'

function formatTime(value?: string): string {
  if (!value) return '--'
  const parsed = new Date(value)
  return Number.isNaN(parsed.getTime()) ? '--' : parsed.toLocaleString()
}

// The dwell field is stored in seconds but read on a phone screen, so it
// gets the coarsest unit that keeps it a whole number: seconds under a
// minute, minutes under an hour, hours beyond that.
function formatDwell(seconds: number): string {
  if (seconds < 60) return `${seconds}s`
  if (seconds < 3600) return `${Math.round(seconds / 60)}m`
  return `${Math.round(seconds / 3600)}h`
}

interface AlarmsDrawerProps {
  alarms: ActiveAlarm[]
  onAcknowledge: (ruleId: string) => Promise<void>
  onSilence: (ruleId: string) => Promise<void>
  // Rules are lifted into App and passed down (rather than the drawer
  // calling useAlarmRules() itself) so a rule saved here is immediately
  // visible to other consumers of the same list, such as the battery tile's
  // SoC bands, without a reload.
  rules: AlarmRule[]
  loading: boolean
  error: string | null
  createRule: (draft: AlarmRuleDraft) => Promise<void>
  updateRule: (id: string, draft: AlarmRuleDraft) => Promise<void>
  deleteRule: (id: string) => Promise<void>
  // Computed in App from the configured SignalK address (ADR 0090); null
  // when unconfigured, in which case no tuning link renders.
  collisionTuningUrl: string | null
}

export const AlarmsDrawer = memo(function AlarmsDrawer({
  alarms,
  onAcknowledge,
  onSilence,
  rules,
  loading,
  error,
  createRule,
  updateRule,
  deleteRule,
  collisionTuningUrl,
}: AlarmsDrawerProps) {
  const { entries, refresh: refreshLog } = useAlarmLog(true)
  const { paths: signalKPaths } = useSignalKPaths(true)
  const [draft, setDraft] = useState<AlarmRuleDraft | null>(null)
  const [editingId, setEditingId] = useState<string | null>(null)
  const [formError, setFormError] = useState<string | null>(null)
  const [ackError, setAckError] = useState<string | null>(null)

  // Acknowledging or clearing changes history, so keep the log in step.
  useEffect(() => { void refreshLog() }, [alarms.length, refreshLog])

  // A rule's threshold is stored in SI, same as a gauge binding; this reuses
  // the same path->units lookup the gauge picker already fetches, so a rule
  // row and the edit form can both show the reading in operator units.
  const unitsByPath = useMemo(() => {
    const map = new Map<string, string | undefined>()
    for (const path of signalKPaths) map.set(path.path, path.units)
    return map
  }, [signalKPaths])

  // A rule is "firing" when it currently has a live alarm on the board,
  // acknowledged or not (an acknowledged alarm is still live, ADR 0038); the
  // pill ties the rule list back to the Active Alarms tile above it.
  const firingRuleIds = useMemo(() => {
    const ids = new Set<string>()
    for (const alarm of alarms) {
      if (alarm.phase === 'active' || alarm.phase === 'acknowledged') ids.add(alarm.rule_id)
    }
    return ids
  }, [alarms])

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

  const silence = useCallback(async (ruleId: string) => {
    setAckError(null)
    try {
      await onSilence(ruleId)
    } catch (err) {
      setAckError(err instanceof Error ? err.message : String(err))
    }
  }, [onSilence])

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
            {alarms.map((alarm) => {
              // Raised/acknowledged times and the path are the only "when
              // and where" facts on the card; a missing time is omitted
              // rather than rendered as "--" (formatAlarmTime already
              // returns null for that).
              const raised = formatAlarmTime(alarm.raised_at)
              const acked = formatAlarmTime(alarm.acked_at)
              const timeParts = [raised && `Raised ${raised}`, acked && `acknowledged ${acked}`].filter(Boolean)
              // The path is a SignalK bus topic, and notifications all live
              // under one namespace on it, so stripping that prefix leaves
              // the part that actually identifies the source.
              const displayPath = alarm.path.replace(/^notifications\./, '')
              // A bus notification has no rule label of its own, so its
              // label IS the path already (see use-alarms). Showing it again
              // on the meta line would just repeat the headline.
              const showPath = displayPath !== alarm.label
              const hasMeta = timeParts.length > 0 || showPath

              return (
                <div key={alarm.rule_id} className="rounded-md border bg-background/60 px-3 py-3">
                  <div className="flex flex-col gap-2 sm:flex-row sm:items-start sm:justify-between">
                    <div className="min-w-0">
                      <p className={`break-words font-display text-lg leading-none sm:truncate ${severityClass(alarm.state)}`}>
                        {alarm.label}
                      </p>
                      <p className="mt-1.5 text-sm text-foreground/90">{alarmConditionSentence(alarm)}</p>
                      {hasMeta && (
                        <p className="mt-1 truncate text-[11px] text-muted-foreground">
                          {timeParts.length > 0 && timeParts.join(' · ')}
                          {timeParts.length > 0 && showPath && ' · '}
                          {showPath && <span className="font-display">{displayPath}</span>}
                        </p>
                      )}
                      {/*
                        A collision alarm's CPA/TCPA thresholds live in the
                        AIS Target Prioritizer plugin, not in a Helmcentral
                        rule, so the card points at the one place they can
                        actually be changed (ADR 0090). No other bus
                        producer has a tuning page to send anyone to, so
                        only collision alarms get this link.
                      */}
                      {alarm.rule_id.startsWith(COLLISION_ALARM_RULE_PREFIX) && collisionTuningUrl && (
                        <a
                          href={collisionTuningUrl}
                          target="_blank"
                          rel="noreferrer"
                          className="mt-1.5 inline-block rounded border px-2 py-1 text-[11px] hover:bg-muted"
                        >
                          Adjust thresholds in AIS Target Prioritizer
                        </a>
                      )}
                    </div>
                    {/*
                      The pill describes state (still live after silencing or
                      acknowledging); it does not replace the actions. SignalK
                      reports per notification which of those it still
                      offers, so the buttons render independently, driven
                      only by can_silence/can_acknowledge. An acknowledged
                      alarm already gets both false from the server, so no
                      special-casing is needed to hide them there (ADR 0038:
                      silencing is not acknowledging, and the drawer renders
                      exactly what the server advertises).

                      Below sm this sits in its own row under the text
                      instead of squeezing it into a narrow column (a phone
                      at 390px was truncating the headline to a few
                      characters and wrapping the sentence word by word).
                    */}
                    <div className="flex shrink-0 items-center gap-2 sm:flex-col sm:items-end sm:gap-1">
                      <span className={`text-[10px] uppercase tracking-[0.16em] ${severityClass(alarm.state)}`}>
                        {alarm.state}
                      </span>
                      {alarm.phase === 'acknowledged' ? (
                        <span className="rounded-sm border px-1.5 py-0.5 text-[10px] uppercase tracking-[0.16em] text-muted-foreground">
                          Acknowledged · still live
                        </span>
                      ) : alarm.silenced ? (
                        <span className="rounded-sm border px-1.5 py-0.5 text-[10px] uppercase tracking-[0.16em] text-muted-foreground">
                          Silenced · still live
                        </span>
                      ) : null}
                      <div className="flex items-center gap-2">
                        {alarm.can_silence && (
                          <Button size="sm" variant="ghost" onClick={() => void silence(alarm.rule_id)}>
                            Silence
                          </Button>
                        )}
                        {alarm.can_acknowledge && (
                          <Button size="sm" variant="outline" onClick={() => void acknowledge(alarm.rule_id)}>
                            Acknowledge
                          </Button>
                        )}
                      </div>
                    </div>
                  </div>
                </div>
              )
            })}
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
          <div className="flex flex-col gap-4">
            {/* Grouped by the domain segment already in the path (electrical,
                environment, radar, ...) rather than a second taxonomy the
                operator would have to maintain; see lib/alarm-rules-view. */}
            {groupRulesByDomain(rules).map((group) => (
              <div key={group.domain}>
                <p className="mb-1.5 text-[10px] uppercase tracking-[0.16em] text-muted-foreground">{group.domain}</p>
                <div className="flex flex-col gap-2">
                  {group.rules.map((rule) => (
                    <RuleRow
                      key={rule.id}
                      rule={rule}
                      unit={unitsByPath.get(rule.path)}
                      firing={firingRuleIds.has(rule.id)}
                      onEdit={startEdit}
                      onDelete={(id) => void deleteRule(id)}
                    />
                  ))}
                </div>
              </div>
            ))}
          </div>
        )}

        {draft && (
          <RuleForm
            key={editingId ?? 'new'}
            draft={draft}
            error={formError}
            isEditing={editingId !== null}
            unitsByPath={unitsByPath}
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
                  <span className={severityClass(entry.state)}>{entry.state}</span>
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

/**
 * The line-2 condition text for a rule row: what it watches, what clears
 * it, how long it must hold, its severity, and finally its path. The path
 * is the last segment (in its own font-display span) because two derived
 * rules can share a label ("RPM" per engine) and the path is the only
 * thing on the row that tells them apart.
 */
function RuleCondition({ rule, unit }: { rule: AlarmRule; unit?: string }) {
  if (rule.op === 'stale') {
    return (
      <>
        no data for {rule.stale_after_seconds}s · <span className={severityClass(rule.state)}>{rule.state}</span>
        {' · '}<span className="font-display">{rule.path}</span>
      </>
    )
  }

  let line = `${rule.op} ${formatAlarmReading(rule.value, unit)}`

  // Only above/below have a clear point; equal/notEqual clear the instant
  // the value stops matching, so there is nothing extra to say.
  if (rule.hysteresis > 0 && (rule.op === 'above' || rule.op === 'below')) {
    const clear = rule.op === 'below' ? rule.value + rule.hysteresis : rule.value - rule.hysteresis
    line += ` · clears at ${formatAlarmReading(clear, unit)}`
  }

  if (rule.dwell_seconds > 0) {
    line += ` · for ${formatDwell(rule.dwell_seconds)}`
  }

  return (
    <>
      {line} · <span className={severityClass(rule.state)}>{rule.state}</span>
      {' · '}<span className="font-display">{rule.path}</span>
    </>
  )
}

interface RuleRowProps {
  rule: AlarmRule
  unit?: string
  firing: boolean
  onEdit: (rule: AlarmRule) => void
  onDelete: (id: string) => void
}

function RuleRow({ rule, unit, firing, onEdit, onDelete }: RuleRowProps) {
  // Delete confirms inline rather than as a modal, so a mis-tap on the trash
  // icon isn't the fastest way to lose a rule (Thaler & Sunstein 2008).
  const [pendingDelete, setPendingDelete] = useState(false)

  const rowClassName = `flex min-w-0 items-center justify-between gap-3 rounded-md border bg-background/60 px-3 py-2 ${firing ? 'border-amber-300' : ''}`

  if (pendingDelete) {
    return (
      <div className={rowClassName}>
        <p className="min-w-0 truncate text-sm">
          Delete {rule.label}?{firing ? ' It is firing now.' : ''}
        </p>
        <div className="flex shrink-0 gap-2">
          <Button size="sm" variant="ghost" onClick={() => setPendingDelete(false)}>Keep</Button>
          <Button size="sm" className="bg-red-600 text-white hover:bg-red-700" onClick={() => onDelete(rule.id)}>
            Delete rule
          </Button>
        </div>
      </div>
    )
  }

  return (
    <div className={rowClassName}>
      <div className="min-w-0">
        <p className="truncate text-sm">
          {rule.label}
          {!rule.enabled && (
            <span className="ml-2 text-[10px] uppercase tracking-[0.16em] text-muted-foreground">Disabled</span>
          )}
          {rule.derived && (
            <span className="ml-2 text-[10px] uppercase tracking-[0.16em] text-muted-foreground">
              From a gauge zone
            </span>
          )}
          {firing && (
            <span className="ml-2 rounded-sm bg-amber-100 px-1.5 py-0.5 text-[10px] uppercase tracking-[0.16em] text-amber-700">
              firing
            </span>
          )}
        </p>
        <p className="break-words text-[11px] text-muted-foreground">
          <RuleCondition rule={rule} unit={unit} />
        </p>
      </div>
      {/* A derived rule is edited by editing the gauge zone it comes from, so
          offering controls that would only 400 is worse than offering none. */}
      {rule.derived ? (
        <span className="shrink-0 text-[10px] uppercase tracking-[0.16em] text-muted-foreground">
          Edit on the gauge
        </span>
      ) : (
        <div className="flex shrink-0 gap-1">
          <Button size="sm" variant="ghost" onClick={() => onEdit(rule)} aria-label={`Edit ${rule.label}`}>
            <Pencil className="size-3.5" />
          </Button>
          <Button size="sm" variant="ghost" onClick={() => setPendingDelete(true)} aria-label={`Delete ${rule.label}`}>
            <Trash2 className="size-3.5" />
          </Button>
        </div>
      )}
    </div>
  )
}

interface RuleFormProps {
  draft: AlarmRuleDraft
  error: string | null
  isEditing: boolean
  unitsByPath: Map<string, string | undefined>
  onChange: (draft: AlarmRuleDraft) => void
  onCancel: () => void
  onSave: () => void
}

function RuleForm({ draft, error, isEditing, unitsByPath, onChange, onCancel, onSave }: RuleFormProps) {
  const set = <K extends keyof AlarmRuleDraft>(key: K, value: AlarmRuleDraft[K]) =>
    onChange({ ...draft, [key]: value })

  const isStale = draft.op === 'stale'
  const unit = unitsByPath.get(draft.path)
  const hasUnit = typeof unit === 'string' && unit.trim() !== ''

  // Advanced starts open only when the rule already departs from the plain
  // defaults (an operator editing a tuned rule should see the tuning), never
  // just because a rule is being edited, since most rules never touch these three.
  const [advancedOpen, setAdvancedOpen] = useState(() => {
    const defaults = newAlarmRuleDraft()
    return draft.hysteresis !== defaults.hysteresis
      || draft.dwell_seconds !== defaults.dwell_seconds
      || draft.escalate_after_seconds !== defaults.escalate_after_seconds
  })

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
            <FieldLabel htmlFor="alarm-value">{hasUnit ? `Threshold (${unit})` : 'Threshold'}</FieldLabel>
            <Input id="alarm-value" type="number" step="any" value={draft.value} onChange={(e) => set('value', Number(e.target.value))} />
            {hasUnit && (
              <p className="text-[10px] text-muted-foreground">= {formatAlarmReading(draft.value, unit)}</p>
            )}
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
      </div>

      <button
        type="button"
        className="mt-3 text-[11px] uppercase tracking-[0.16em] text-muted-foreground"
        aria-expanded={advancedOpen}
        onClick={() => setAdvancedOpen((open) => !open)}
      >
        Advanced
      </button>

      {advancedOpen && (
        <div className="mt-2 grid gap-3 sm:grid-cols-2">
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
      )}

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

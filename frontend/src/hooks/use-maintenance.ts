import { useCallback, useEffect, useRef, useState } from 'react'
import { apiBaseUrl } from '@/config/api'
import { readErrorMessage } from '@/lib/api-error'
import { InventoryValidationError, type InventoryFieldError } from '@/hooks/use-inventory'
import { todayISO } from '@/lib/local-date'

// ADR 0138's 2026-09-27 amendment: every endpoint below that returns a rule
// view with a computed status requires the operator's own local calendar
// date as a `today` query param (maintenance_handlers.go's
// requireTodayParam) - never left for the server to guess at from its own
// clock. todayFromClock() is the one place this file reads it, from
// lib/local-date.ts's todayISO (the browser's own local date, never UTC).
function todayFromClock(): string {
  return todayISO()
}

// withToday appends `today=<local date>` to url, using `&` when url already
// carries a query string and `?` otherwise - every write below that answers
// with a freshly recomputed rule view needs this.
function withToday(url: string): string {
  const separator = url.includes('?') ? '&' : '?'
  return `${url}${separator}today=${encodeURIComponent(todayFromClock())}`
}

// ADR 0138: the Maintenance section's data layer - service rules and the
// log they're completed into. Same idiom as use-inventory.ts throughout: no
// react-query, no shared store, one instance per caller, a plain
// fetch-and-throw-the-server's-own-message write helper (AGENTS.md fallback
// policy). submitJSON is copied rather than imported from use-inventory.ts -
// that file's own comment on its identical copy from use-documents.ts
// explains why: a two-line helper is cheaper to duplicate a third time than
// to thread a shared-module dependency between features that otherwise
// don't know about each other. InventoryValidationError IS imported and
// reused as-is, though - maintenance's own validation errors come back in
// exactly the same {field, message} shape (backend/maintenance_handlers.go
// reuses inventoryValidationError directly, since this is a sub-feature of
// Inventory, not a separate one), so a second class carrying the identical
// fields would be pure duplication with no behavioural difference.

export type MaintenanceStatus = 'overdue' | 'due_soon' | 'ok' | 'never_recorded' | 'interval_not_set' | 'hours_unknown'

export type MaintenanceLogKind = 'maintenance' | 'repair' | 'improvement'

/** maintenanceRuleView, backend/maintenance_handlers.go - a rule's own
 * stored fields plus a status computed fresh on every response. No field is
 * ever omitted server-side (ADR 0115 §7's "the browser binds these through
 * a TypeScript interface that declares every key" reasoning), so every
 * field here is required, never optional, even where blank/null is the
 * common case. */
export interface MaintenanceRule {
  id: string
  equipment_id: string | null
  equipment_name: string
  system: string
  description: string
  interval_hours: number | null
  interval_months: number | null
  due_soon_hours: number | null
  due_soon_months: number | null
  fixed_due_date: string
  last_done_at: string
  last_done_hours: number | null
  profile_service_id: string
  procedure_note_id: string
  ack_reason: string
  acknowledged: boolean
  created_at: string
  updated_at: string
  status: MaintenanceStatus
  remaining_hours: number | null
  remaining_days: number | null
  hours_unknown: boolean
  has_hour_meter_path: boolean
  hours_stale_since: string | null
  current_hours: number | null
}

/** The body POST/PUT /api/inventory/maintenance/rules(/:id) accept - a
 * rule's CORE fields only. last_done_at/last_done_hours (setMaintenanceRuleLastDone
 * or completeMaintenanceRule), procedure_note_id (setMaintenanceRuleProcedureNote/
 * createMaintenanceProcedureNote) and the acknowledgement
 * (acknowledgeMaintenanceRule) each have their own dedicated write below -
 * see backend/maintenance_store.go's maintenanceRuleInput doc comment for
 * why an ordinary edit must never touch any of those as a side effect. */
export interface MaintenanceRuleInput {
  equipment_id: string | null
  description: string
  interval_hours: number | null
  interval_months: number | null
  due_soon_hours: number | null
  due_soon_months: number | null
  fixed_due_date: string
  profile_service_id: string
}

/** A brand new, unsaved rule draft - every field at its default. */
export const BLANK_RULE_DRAFT: MaintenanceRuleInput = {
  equipment_id: null,
  description: '',
  interval_hours: null,
  interval_months: null,
  due_soon_hours: null,
  due_soon_months: null,
  fixed_due_date: '',
  profile_service_id: '',
}

export interface MaintenanceLogPart {
  equipment_id: string
  equipment_name: string
  quantity: number
}

export interface MaintenanceLogEntry {
  id: string
  equipment_id: string | null
  rule_id: string | null
  performed_at: string
  hours: number | null
  kind: MaintenanceLogKind
  description: string
  who: string
  cost: number | null
  currency: string
  created_at: string
  updated_at: string
  parts: MaintenanceLogPart[]
  photo_ids: string[]
}

export interface MaintenanceLogPartInput {
  equipment_id: string
  quantity: number
}

/** The body every service-log write (standalone create, update, and
 * completeMaintenanceRule below) sends. equipment_id/kind are required by
 * createMaintenanceLogEntry (a standalone entry has no rule to source them
 * from); completeMaintenanceRule ignores both fields entirely - the backend
 * fixes them from the rule itself (kind is always 'maintenance'). */
export interface MaintenanceLogEntryInput {
  equipment_id?: string | null
  performed_at: string
  hours: number | null
  kind: MaintenanceLogKind
  description: string
  who: string
  cost: number | null
  currency: string
  parts: MaintenanceLogPartInput[]
  /** completeMaintenanceRule's own field, meaningless to the standalone
   * create/update calls: the next fixed_due_date for a rule that has one
   * and no interval_months to compute it from - required in that case
   * (backend/maintenance_handlers.go's completeMaintenanceRuleHandler
   * 400s without it), ignored when interval_months lets the server
   * compute it instead. */
  new_due_date?: string
}

export interface HourMeterReset {
  old_reading: number
  new_reading: number
  changed_at: string
  created_at: string
}

// Copied from use-inventory.ts's own submitJSON - see this file's header
// comment for why.
async function submitJSON<T>(url: string, method: string, body?: unknown): Promise<T> {
  const response = await fetch(url, {
    method,
    headers: body !== undefined ? { 'Content-Type': 'application/json' } : undefined,
    body: body !== undefined ? JSON.stringify(body) : undefined,
  })
  if (!response.ok) {
    const payload = (await response.json().catch(() => ({}))) as {
      error?: string
      errors?: InventoryFieldError[]
      field?: string
      message?: string
    }
    if (Array.isArray(payload.errors) && payload.errors.length > 0) {
      throw new InventoryValidationError(payload.errors)
    }
    if (typeof payload.field === 'string' && typeof payload.message === 'string') {
      throw new InventoryValidationError([{ field: payload.field, message: payload.message }])
    }
    throw new Error(payload.error ?? `HTTP ${response.status}`)
  }
  if (response.status === 204) return undefined as T
  return (await response.json()) as T
}

export interface MaintenanceRuleFilter {
  equipment?: string
  system?: string
  includeStored?: boolean
}

/**
 * The Maintenance section's own filtered rule listing -
 * `?equipment=&system=&include_stored=`. Mirrors useEquipment(filter)'s own
 * contract exactly (use-inventory.ts): `filter === null` fetches nothing at
 * all, for a caller with no scope yet to filter by.
 */
export function useMaintenanceRules(filter: MaintenanceRuleFilter | null) {
  const [rules, setRules] = useState<MaintenanceRule[]>([])
  const [loading, setLoading] = useState(filter !== null)
  const [error, setError] = useState<string | null>(null)
  const seqRef = useRef(0)

  const filterKey = filter === null
    ? null
    : JSON.stringify([filter.equipment ?? '', filter.system ?? '', filter.includeStored ?? false])

  const refresh = useCallback(async () => {
    if (filter === null) {
      seqRef.current += 1
      setRules([])
      setError(null)
      setLoading(false)
      return
    }
    const seq = (seqRef.current += 1)
    setLoading(true)
    try {
      const params = new URLSearchParams()
      if (filter.equipment) params.set('equipment', filter.equipment)
      if (filter.system) params.set('system', filter.system)
      if (filter.includeStored) params.set('include_stored', 'true')
      params.set('today', todayFromClock())
      const qs = params.toString()
      const res = await fetch(`${apiBaseUrl}/api/inventory/maintenance/rules${qs ? `?${qs}` : ''}`)
      if (!res.ok) throw new Error(await readErrorMessage(res))
      const data = (await res.json()) as { rules?: MaintenanceRule[] }
      if (seq !== seqRef.current) return
      setRules(data.rules ?? [])
      setError(null)
    } catch (err) {
      if (seq !== seqRef.current) return
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      if (seq === seqRef.current) setLoading(false)
    }
    // filterKey stands in for filter's actual fields - the same idiom
    // useEquipment's own refresh() uses and explains (use-inventory.ts).
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [filterKey])

  useEffect(() => { void refresh() }, [refresh])

  return { rules, loading, error, refresh }
}

/** POST /api/inventory/maintenance/rules */
export async function createMaintenanceRule(input: MaintenanceRuleInput): Promise<MaintenanceRule> {
  const data = await submitJSON<{ rule: MaintenanceRule }>(withToday(`${apiBaseUrl}/api/inventory/maintenance/rules`), 'POST', input)
  return data.rule
}

/** PUT /api/inventory/maintenance/rules/:id */
export async function updateMaintenanceRule(id: string, input: MaintenanceRuleInput): Promise<MaintenanceRule> {
  const data = await submitJSON<{ rule: MaintenanceRule }>(withToday(`${apiBaseUrl}/api/inventory/maintenance/rules/${encodeURIComponent(id)}`), 'PUT', input)
  return data.rule
}

/** DELETE /api/inventory/maintenance/rules/:id */
export async function deleteMaintenanceRule(id: string): Promise<void> {
  await submitJSON<void>(`${apiBaseUrl}/api/inventory/maintenance/rules/${encodeURIComponent(id)}`, 'DELETE')
}

/** POST /api/inventory/maintenance/rules/:id/acknowledge - a blank reason
 * clears the acknowledgement. */
export async function acknowledgeMaintenanceRule(id: string, reason: string): Promise<MaintenanceRule> {
  const data = await submitJSON<{ rule: MaintenanceRule }>(
    withToday(`${apiBaseUrl}/api/inventory/maintenance/rules/${encodeURIComponent(id)}/acknowledge`), 'POST', { reason },
  )
  return data.rule
}

/** POST /api/inventory/maintenance/rules/:id/last-done - the onboarding-only
 * baseline write that writes NO log entry. Either argument may be omitted
 * to leave that half of the baseline untouched. */
export async function setMaintenanceRuleLastDone(id: string, lastDoneAt?: string, lastDoneHours?: number): Promise<MaintenanceRule> {
  const body: { last_done_at?: string; last_done_hours?: number } = {}
  if (lastDoneAt !== undefined) body.last_done_at = lastDoneAt
  if (lastDoneHours !== undefined) body.last_done_hours = lastDoneHours
  const data = await submitJSON<{ rule: MaintenanceRule }>(
    withToday(`${apiBaseUrl}/api/inventory/maintenance/rules/${encodeURIComponent(id)}/last-done`), 'POST', body,
  )
  return data.rule
}

/** POST /api/inventory/maintenance/rules/:id/complete - writes a
 * kind='maintenance' log entry, resets the rule's own baseline to it, and
 * clears any acknowledgement. */
export async function completeMaintenanceRule(id: string, input: Omit<MaintenanceLogEntryInput, 'equipment_id' | 'kind'>): Promise<{ rule: MaintenanceRule; entry: MaintenanceLogEntry }> {
  return submitJSON<{ rule: MaintenanceRule; entry: MaintenanceLogEntry }>(
    withToday(`${apiBaseUrl}/api/inventory/maintenance/rules/${encodeURIComponent(id)}/complete`), 'POST', input,
  )
}

/** PUT /api/inventory/maintenance/rules/:id/procedure-note - {note_id},
 * blank clears the link. */
export async function setMaintenanceRuleProcedureNote(id: string, noteId: string): Promise<MaintenanceRule> {
  const data = await submitJSON<{ rule: MaintenanceRule }>(
    withToday(`${apiBaseUrl}/api/inventory/maintenance/rules/${encodeURIComponent(id)}/procedure-note`), 'PUT', { note_id: noteId },
  )
  return data.rule
}

/** POST /api/inventory/maintenance/rules/:id/procedure-note - creates a
 * fresh Procedure note titled from the rule and links it. */
export async function createMaintenanceProcedureNote(id: string): Promise<{ rule: MaintenanceRule; note: { id: string; title: string } }> {
  return submitJSON<{ rule: MaintenanceRule; note: { id: string; title: string } }>(
    withToday(`${apiBaseUrl}/api/inventory/maintenance/rules/${encodeURIComponent(id)}/procedure-note`), 'POST',
  )
}

/** POST /api/inventory/equipment/:id/maintenance/copy-profile-schedule -
 * spec's "Use profile schedule" one-action copy. Idempotent: pressing it
 * again returns an empty array rather than duplicating anything. */
export async function copyMaintenanceProfileSchedule(equipmentId: string): Promise<MaintenanceRule[]> {
  const data = await submitJSON<{ rules: MaintenanceRule[] }>(
    withToday(`${apiBaseUrl}/api/inventory/equipment/${encodeURIComponent(equipmentId)}/maintenance/copy-profile-schedule`), 'POST',
  )
  return data.rules
}

/** POST /api/inventory/equipment/:id/maintenance/meter-reset - records a
 * meter replacement (old reading, new reading, date) so true hours keep
 * counting across it. */
export async function recordHourMeterReset(equipmentId: string, oldReading: number, newReading: number, changedAt: string): Promise<HourMeterReset> {
  const data = await submitJSON<{ reset: HourMeterReset }>(
    `${apiBaseUrl}/api/inventory/equipment/${encodeURIComponent(equipmentId)}/maintenance/meter-reset`, 'POST',
    { old_reading: oldReading, new_reading: newReading, changed_at: changedAt },
  )
  return data.reset
}

/** GET /api/inventory/equipment/:id/maintenance/meter-resets - the
 * equipment editor's own display of past meter replacements. */
export function useHourMeterResets(equipmentId: string | null) {
  const [resets, setResets] = useState<HourMeterReset[]>([])
  const [loading, setLoading] = useState(equipmentId !== null)
  const [error, setError] = useState<string | null>(null)
  const seqRef = useRef(0)

  const refresh = useCallback(async () => {
    if (equipmentId === null) {
      seqRef.current += 1
      setResets([])
      setError(null)
      setLoading(false)
      return
    }
    const seq = (seqRef.current += 1)
    setLoading(true)
    try {
      const res = await fetch(`${apiBaseUrl}/api/inventory/equipment/${encodeURIComponent(equipmentId)}/maintenance/meter-resets`)
      if (!res.ok) throw new Error(await readErrorMessage(res))
      const data = (await res.json()) as { resets?: HourMeterReset[] }
      if (seq !== seqRef.current) return
      setResets(data.resets ?? [])
      setError(null)
    } catch (err) {
      if (seq !== seqRef.current) return
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      if (seq === seqRef.current) setLoading(false)
    }
  }, [equipmentId])

  useEffect(() => { void refresh() }, [refresh])

  return { resets, loading, error, refresh }
}

export interface MaintenanceLogFilter {
  equipment?: string
}

/** GET /api/inventory/maintenance/log?equipment= - the item's own recent
 * log entries (the equipment editor's Maintenance block) or, with no
 * filter, the whole log. */
export function useMaintenanceLogEntries(filter: MaintenanceLogFilter | null) {
  const [entries, setEntries] = useState<MaintenanceLogEntry[]>([])
  const [loading, setLoading] = useState(filter !== null)
  const [error, setError] = useState<string | null>(null)
  const seqRef = useRef(0)
  const filterKey = filter === null ? null : (filter.equipment ?? '')

  const refresh = useCallback(async () => {
    if (filter === null) {
      seqRef.current += 1
      setEntries([])
      setError(null)
      setLoading(false)
      return
    }
    const seq = (seqRef.current += 1)
    setLoading(true)
    try {
      const params = new URLSearchParams()
      if (filter.equipment) params.set('equipment', filter.equipment)
      const qs = params.toString()
      const res = await fetch(`${apiBaseUrl}/api/inventory/maintenance/log${qs ? `?${qs}` : ''}`)
      if (!res.ok) throw new Error(await readErrorMessage(res))
      const data = (await res.json()) as { entries?: MaintenanceLogEntry[] }
      if (seq !== seqRef.current) return
      setEntries(data.entries ?? [])
      setError(null)
    } catch (err) {
      if (seq !== seqRef.current) return
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      if (seq === seqRef.current) setLoading(false)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [filterKey])

  useEffect(() => { void refresh() }, [refresh])

  return { entries, loading, error, refresh }
}

/** POST /api/inventory/maintenance/log - a standalone entry (repair,
 * improvement) against an item with no rule. */
export async function createMaintenanceLogEntry(input: MaintenanceLogEntryInput): Promise<MaintenanceLogEntry> {
  const data = await submitJSON<{ entry: MaintenanceLogEntry }>(`${apiBaseUrl}/api/inventory/maintenance/log`, 'POST', input)
  return data.entry
}

/** PUT /api/inventory/maintenance/log/:id */
export async function updateMaintenanceLogEntry(id: string, input: MaintenanceLogEntryInput): Promise<MaintenanceLogEntry> {
  const data = await submitJSON<{ entry: MaintenanceLogEntry }>(`${apiBaseUrl}/api/inventory/maintenance/log/${encodeURIComponent(id)}`, 'PUT', input)
  return data.entry
}

/** DELETE /api/inventory/maintenance/log/:id */
export async function deleteMaintenanceLogEntry(id: string): Promise<void> {
  await submitJSON<void>(`${apiBaseUrl}/api/inventory/maintenance/log/${encodeURIComponent(id)}`, 'DELETE')
}

/** POST /api/inventory/maintenance/log/:id/photos - one multipart upload,
 * linked at the end of the entry's photo order. Mirrors
 * uploadEquipmentPhoto's own contract (use-inventory.ts) exactly. */
export async function uploadMaintenanceLogPhoto(logEntryId: string, file: Blob, filename: string): Promise<MaintenanceLogEntry> {
  const form = new FormData()
  form.append('file', file, filename)
  const response = await fetch(`${apiBaseUrl}/api/inventory/maintenance/log/${encodeURIComponent(logEntryId)}/photos`, {
    method: 'POST',
    body: form,
  })
  if (!response.ok) throw new Error(await readErrorMessage(response))
  const data = (await response.json()) as { entry: MaintenanceLogEntry }
  return data.entry
}

/** DELETE /api/inventory/maintenance/log/:id/photos/:documentId - unlink
 * only by default; deletePhotos additionally deletes the document once
 * nothing else (another log entry, or an equipment item's own strip)
 * still shows it. */
export async function deleteMaintenanceLogPhoto(logEntryId: string, documentId: string, deletePhoto = false): Promise<MaintenanceLogEntry> {
  const qs = deletePhoto ? '?delete=true' : ''
  const data = await submitJSON<{ entry: MaintenanceLogEntry }>(
    `${apiBaseUrl}/api/inventory/maintenance/log/${encodeURIComponent(logEntryId)}/photos/${encodeURIComponent(documentId)}${qs}`, 'DELETE',
  )
  return data.entry
}

/** GET /api/inventory/maintenance/log/export.csv[?equipment=] - a plain
 * URL for a download link/button, not a fetch wrapper: the server sets
 * Content-Disposition itself, so an ordinary `<a href download>` is the
 * whole implementation. */
export function maintenanceLogExportURL(equipmentId?: string): string {
  const qs = equipmentId ? `?equipment=${encodeURIComponent(equipmentId)}` : ''
  return `${apiBaseUrl}/api/inventory/maintenance/log/export.csv${qs}`
}

// ── due-soon defaults (mirrors backend/maintenance_status.go's own
// constants, spec's own "make defaults constants" - shown in the rule
// dialog as placeholder text, never sent unless the operator overrides
// them) ──────────────────────────────────────────────────────────────────
export const MAINTENANCE_DEFAULT_DUE_SOON_HOURS = 50
export const MAINTENANCE_DEFAULT_DUE_SOON_MONTHS = 1

export const MAINTENANCE_STATUS_LABELS: Record<MaintenanceStatus, string> = {
  overdue: 'Overdue',
  due_soon: 'Due soon',
  never_recorded: 'Never recorded',
  hours_unknown: 'Hours unknown',
  interval_not_set: 'Interval not set',
  ok: 'OK',
}

// Display order (spec §9): Overdue, Due soon, Never recorded, Hours
// unknown, Interval not set, OK.
export const MAINTENANCE_STATUS_ORDER: MaintenanceStatus[] = [
  'overdue', 'due_soon', 'never_recorded', 'hours_unknown', 'interval_not_set', 'ok',
]

import { apiBaseUrl } from '@/config/api'
import { readErrorMessage } from '@/lib/api-error'

// The import wizard's data layer: the run object the server keeps for one
// uploaded export, the operator's decisions on it, and the six routes that
// read and change it. Shapes follow backend/import_staged.go; a record
// with no decision entry is treated as "skip" by the server.

export interface StagedParticular {
  key: string
  label: string
  field: string
  value: string
  signalk: boolean
}

export interface StagedLocation {
  key: string
  name: string
  count: number
}

export interface StagedEquipment {
  key: string
  kind: 'engine' | 'equipment'
  name: string
  type: string
  category: string
  manufacturer: string
  serial: string
  installed: string
  location: string
  detail: string
  notes: string
  hours: string
}

export interface StagedSpare {
  key: string
  kind: 'item' | 'bin'
  name: string
  part_number: string
  on_hand: number
  required: number | null
  location: string
  detail: string
  notes: string
  category: string
  items: string[]
}

export interface StagedLogEntry {
  key: string
  date: string
  title: string
  body: string
  type: string
  equipment_name: string
  equipment_key: string
  candidates: string[]
  hours: number | null
}

export interface StagedNote {
  key: string
  kind: 'note' | 'task'
  title: string
  date: string
  body: string
  skip: boolean
  skip_reason: string
}

export interface StagedFile {
  key: string
  kind: 'document' | 'photo'
  label: string
  type: string
  attached_to: string
  date: string
  size: string
  url: string
  equipment_key: string
}

export interface StagedIssue {
  code: string
  severity: 'warning' | 'info'
  section: string
  key: string
  message: string
}

export interface StagedSection {
  name: string
  listed: number
  imported: number
}

export interface StagedImport {
  source: string
  vessel: { name: string; generated_at: string }
  sections: StagedSection[]
  particulars: StagedParticular[]
  locations: StagedLocation[]
  equipment: StagedEquipment[]
  spares: StagedSpare[]
  log_entries: StagedLogEntry[]
  notes: StagedNote[]
  files: StagedFile[]
  issues: StagedIssue[]
}

export type ParticularAction = 'apply' | 'skip'
export type RecordAction = 'create' | 'match' | 'skip'

export interface ZoneDecision {
  action: RecordAction
  zone_id: string
}

export interface RecordDecision {
  action: RecordAction
  target_id: string
}

export interface LogEquipmentDecision {
  equipment_key: string
  equipment_id: string
}

export interface FileDecision {
  document_id: string
  skipped: boolean
  equipment_key: string
}

export interface ImportDecisions {
  particulars: Record<string, ParticularAction>
  zones: Record<string, ZoneDecision>
  records: Record<string, RecordDecision>
  log_equipment: Record<string, LogEquipmentDecision>
  files: Record<string, FileDecision>
}

/** Any subset of the decisions, as PATCH takes it. */
export type ImportDecisionsPatch = { [K in keyof ImportDecisions]?: ImportDecisions[K] }

export interface ImportRun {
  id: string
  source: string
  source_label: string
  file_sha256: string
  status: 'draft' | 'committed' | 'abandoned'
  staged: StagedImport
  decisions: ImportDecisions
  already_imported: string[]
  created_at: string
  updated_at: string
  committed_at: string | null
}

export interface ImportCount {
  created: number
  matched: number
  skipped: number
}

export interface ImportSummaryRecord {
  kind: 'zone' | 'bin' | 'equipment' | 'log_entry' | 'note' | 'document'
  key: string
  id: string
  label: string
  action: 'created' | 'matched'
}

export interface ImportSummary {
  counts: Record<string, ImportCount>
  records: ImportSummaryRecord[]
}

export interface ImportCommitResult {
  run: ImportRun
  summary: ImportSummary
}

/** A non-2xx answer from an import route. `message` is the server's own
 * wording, shown to the operator as it came; `status` lets the wizard treat
 * a 409 (something outstanding) differently from a 400. */
export class ImportApiError extends Error {
  status: number
  constructor(status: number, message: string) {
    super(message)
    this.name = 'ImportApiError'
    this.status = status
  }
}

async function readRun<T>(res: Response): Promise<T> {
  if (!res.ok) throw new ImportApiError(res.status, await readErrorMessage(res))
  return (await res.json()) as T
}

function runUrl(id: string, suffix = ''): string {
  return `${apiBaseUrl}/api/import/runs/${encodeURIComponent(id)}${suffix}`
}

export async function createImportRun(source: string, file: File): Promise<ImportRun> {
  const form = new FormData()
  form.append('source', source)
  form.append('file', file, file.name)
  return readRun<ImportRun>(await fetch(`${apiBaseUrl}/api/import/runs`, { method: 'POST', body: form }))
}

export async function fetchImportRun(id: string): Promise<ImportRun> {
  return readRun<ImportRun>(await fetch(runUrl(id)))
}

export async function patchImportDecisions(id: string, decisions: ImportDecisionsPatch): Promise<ImportRun> {
  return readRun<ImportRun>(
    await fetch(runUrl(id), {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ decisions }),
    }),
  )
}

export interface ImportFileUpload {
  document_id: string
  duplicate: boolean
  run: ImportRun
}

export async function uploadImportFile(runId: string, fileKey: string, file: Blob, filename: string): Promise<ImportFileUpload> {
  const form = new FormData()
  form.append('file', file, filename)
  return readRun<ImportFileUpload>(
    await fetch(runUrl(runId, `/files/${encodeURIComponent(fileKey)}`), { method: 'POST', body: form }),
  )
}

export async function commitImportRun(id: string): Promise<ImportCommitResult> {
  return readRun<ImportCommitResult>(await fetch(runUrl(id, '/commit'), { method: 'POST' }))
}

export async function abandonImportRun(id: string): Promise<ImportRun> {
  return readRun<ImportRun>(await fetch(runUrl(id), { method: 'DELETE' }))
}

// --- Derived facts the wizard pages share ---------------------------------

/** Every record the export asks the operator to decide on, by staged key:
 * equipment, spares (items and bins), log entries and notes. */
export function recordAction(decisions: ImportDecisions, key: string): RecordAction {
  return decisions.records[key]?.action ?? 'skip'
}

export function zoneAction(decisions: ImportDecisions, key: string): RecordAction {
  return decisions.zones[key]?.action ?? 'skip'
}

/** A file is settled once it has an uploaded document or was skipped. */
export function fileSettled(decision: FileDecision | undefined): boolean {
  return decision !== undefined && (decision.skipped || decision.document_id !== '')
}

/** An ambiguous entry is one the export matched to two or more engines.
 * Only an entry being created needs the operator to pick. */
export function ambiguousLogEntries(run: ImportRun, decisions: ImportDecisions): StagedLogEntry[] {
  return run.staged.log_entries.filter((e) => e.candidates.length > 1 && recordAction(decisions, e.key) === 'create')
}

export function unchosenLogEntries(run: ImportRun, decisions: ImportDecisions): StagedLogEntry[] {
  return ambiguousLogEntries(run, decisions).filter((e) => decisions.log_equipment[e.key] === undefined)
}

export function unsettledFiles(run: ImportRun, decisions: ImportDecisions): StagedFile[] {
  return run.staged.files.filter((f) => !fileSettled(decisions.files[f.key]))
}

/** How the operator's decisions add up, per section, for the confirm page. */
export interface DecisionTally {
  create: number
  match: number
  skip: number
}

function tally(actions: RecordAction[]): DecisionTally {
  return {
    create: actions.filter((a) => a === 'create').length,
    match: actions.filter((a) => a === 'match').length,
    skip: actions.filter((a) => a === 'skip').length,
  }
}

export function tallyRun(run: ImportRun, decisions: ImportDecisions) {
  const { staged } = run
  const particulars = staged.particulars.filter((p) => p.field !== '' && !p.signalk && p.value !== '')
  return {
    particulars: {
      create: particulars.filter((p) => decisions.particulars[p.key] === 'apply').length,
      match: 0,
      skip: particulars.filter((p) => decisions.particulars[p.key] !== 'apply').length,
    },
    zones: tally(staged.locations.map((l) => zoneAction(decisions, l.key))),
    equipment: tally(staged.equipment.map((e) => recordAction(decisions, e.key))),
    spares: tally(staged.spares.filter((s) => s.kind === 'item').map((s) => recordAction(decisions, s.key))),
    bins: tally(staged.spares.filter((s) => s.kind === 'bin').map((s) => recordAction(decisions, s.key))),
    log_entries: tally(staged.log_entries.map((e) => recordAction(decisions, e.key))),
    notes: tally(staged.notes.filter((n) => !n.skip).map((n) => recordAction(decisions, n.key))),
    files: {
      create: staged.files.filter((f) => (decisions.files[f.key]?.document_id ?? '') !== '' && !decisions.files[f.key]?.skipped).length,
      match: 0,
      skip: staged.files.filter((f) => decisions.files[f.key]?.skipped).length,
    },
  }
}

/** Merges a one-entry change into a decisions object without mutating it. */
export function withDecision<K extends keyof ImportDecisions>(
  decisions: ImportDecisions,
  category: K,
  key: string,
  value: ImportDecisions[K][string],
): ImportDecisions {
  return { ...decisions, [category]: { ...decisions[category], [key]: value } }
}

/** The name the operator knows a staged record by, for issue lines. */
export function stagedLabel(staged: StagedImport, key: string): string {
  const name =
    staged.locations.find((l) => l.key === key)?.name ??
    staged.equipment.find((e) => e.key === key)?.name ??
    staged.spares.find((s) => s.key === key)?.name ??
    staged.log_entries.find((e) => e.key === key)?.title ??
    staged.notes.find((n) => n.key === key)?.title ??
    staged.files.find((f) => f.key === key)?.label ??
    staged.particulars.find((p) => p.key === key)?.label
  return name ?? ''
}

/** Issues that concern one record, by its staged key. */
export function issuesFor(staged: StagedImport, key: string): StagedIssue[] {
  return staged.issues.filter((i) => i.key === key)
}

/** Only the existing records that a skipped-or-created decision leaves usable
 * as a home for other records: the equipment being created or matched. */
export function usableEquipment(run: ImportRun, decisions: ImportDecisions): StagedEquipment[] {
  return run.staged.equipment.filter((e) => recordAction(decisions, e.key) !== 'skip')
}

/** Why the operator cannot leave this page yet, or null. The same facts the
 * server would refuse the commit for, caught a page earlier. */
export function stepBlocker(stepId: string, run: ImportRun, decisions: ImportDecisions): string | null {
  if (stepId === 'log') {
    const n = unchosenLogEntries(run, decisions).length
    if (n > 0) return `Choose the equipment for ${n} service ${n === 1 ? 'entry' : 'entries'} before going on, or untick ${n === 1 ? 'it' : 'them'}.`
  }
  if (stepId.startsWith('file:')) {
    const key = stepId.slice('file:'.length)
    if (!fileSettled(decisions.files[key])) return 'Hand over the file or skip it to go on.'
  }
  return null
}

// The import section offers to resume the draft you were last working on.
// There is no list route, so the browser remembers the one id. It is a
// convenience pointer only: the run itself always lives on the server.
const DRAFT_POINTER_KEY = 'helmcentral.import.draft-run'

export function rememberDraftRun(id: string | null): void {
  try {
    if (id === null) globalThis.localStorage?.removeItem(DRAFT_POINTER_KEY)
    else globalThis.localStorage?.setItem(DRAFT_POINTER_KEY, id)
  } catch {
    // Storage can be unavailable (private window, blocked site data).
  }
}

export function recalledDraftRun(): string | null {
  try {
    return globalThis.localStorage?.getItem(DRAFT_POINTER_KEY) ?? null
  } catch {
    return null
  }
}

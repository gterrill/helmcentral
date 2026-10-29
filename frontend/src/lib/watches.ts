// The Watches REST API's shapes and the small display rules the pages share
// (ADR 0227). The server owns every figure and sentence; nothing here guesses
// one. The names follow the JSON.

export const WATCH_SECTIONS = [
  { id: 'power', label: 'Power' },
  { id: 'anchor', label: 'Anchor' },
  { id: 'alarms', label: 'Alarms' },
  { id: 'tanks', label: 'Tanks' },
  { id: 'weather', label: 'Weather' },
  { id: 'instruments', label: 'Instruments' },
] as const

export type WatchSectionId = (typeof WATCH_SECTIONS)[number]['id']

export interface WatchThresholds {
  alarms_raised: number
  instrument_issues: number
  tank_change_points: number
  pressure_fall_hpa: number
}

export const DEFAULT_WATCH_THRESHOLDS: WatchThresholds = {
  alarms_raised: 1,
  instrument_issues: 1,
  tank_change_points: 10,
  pressure_fall_hpa: 3,
}

export const MAX_WATCH_NAME = 60
export const MAX_LOOK_OUT_FOR = 500

/** Where a watch can send its report. SignalK is never one: it would put the report on the bus as an alarm. */
export const WATCH_CHANNELS = [
  { id: 'ntfy', label: 'ntfy' },
  { id: 'smtp', label: 'Email' },
  { id: 'webhook', label: 'Webhook' },
  { id: 'webpush', label: 'Phone and browser push' },
] as const

export type WatchSend = 'findings' | 'every'

export const WATCH_SEND_LABELS: Record<WatchSend, string> = {
  findings: 'Only when it finds something',
  every: 'Every report',
}

export const channelLabel = (id: string) => WATCH_CHANNELS.find((c) => c.id === id)?.label ?? id

export interface WatchRunSummary {
  id: string
  started_at: string
  outcome: 'ran' | 'skipped'
  skip_reason?: string
  finding_count: number
  review_problem: string | null
}

export interface WatchRecord {
  id: string
  name: string
  kind: 'scheduled' | 'one_off'
  schedule?: string
  duration_minutes?: number
  started_at?: string
  ends_at?: string
  sections: string[]
  thresholds: WatchThresholds
  look_out_for: string
  enabled: boolean
  /** The channels a report is sent to, and when. */
  notify: string[]
  send: WatchSend
  last_run_at?: string
  updated_at: string
  /** A one-off watch's start and end on the boat's clock (or UTC, said to be UTC). */
  started_boat_time?: string
  ends_boat_time?: string
  schedule_sentence: string
  last_run: WatchRunSummary | null
  has_finding: boolean
  running?: boolean
}

export interface WatchFinding {
  section: string
  rule: string
  text: string
  by: 'rules' | 'mate'
}

export interface WatchReportSection {
  name: string
  title: string
  unavailable?: string
  lines?: string[]
}

export interface WatchSectionsReport {
  kind: string
  generated_at: string
  boat_time: string
  since: string
  since_note?: string
  sections: WatchReportSection[]
}

export interface WatchSeriesReport {
  label: string
  units?: string
  samples: number
  min?: number
  mean?: number
  max?: number
  first?: number
  last?: number
  gap_count?: number
  gap_seconds?: number
  excursion_count: number
}

export interface WatchSamplesReport {
  reason: string
  timezone: string
  started: string
  ended: string
  minutes: number
  series: WatchSeriesReport[]
  difference?: WatchSeriesReport
  difference_note?: string
  truncated?: boolean
  note: string
}

export interface WatchRun {
  id: string
  watch_id: string
  started_at: string
  finished_at: string
  outcome: 'ran' | 'skipped'
  skip_reason?: string
  report_kind: 'sections' | 'samples'
  report?: WatchSectionsReport | WatchSamplesReport
  findings: WatchFinding[]
  review: string | null
  review_problem: string | null
  cost_usd: number | null
  cost_known: boolean
  model?: string
  /** When the run started on the boat's clock, or UTC said to be UTC. Never a device time. */
  boat_time: string
  timezone: string
  /** The channels this run's report reached. */
  notified: string[]
  /** Why a report that was due went to no channel at all, such as none being switched on. */
  not_sent?: string
  /** Deliveries still being retried, with why the last attempt failed. */
  notify_pending?: Array<{ channel: string; reason: string }>
}

export interface WatchList {
  watches: WatchRecord[]
  max: number
  cost_24h: { usd: number; unknown_runs: number }
  storage_bytes: number
  problem?: string
}

export interface WatchDetail {
  watch: WatchRecord
  runs: WatchRun[]
  has_older: boolean
}

export interface WatchSchedulePreview {
  valid: boolean
  error?: string
  sentence?: string
  boat_time_known: boolean
  next_problem?: string
  next: Array<{ at: string; local: string }>
}

/** What the form edits and what create and change send. */
export interface WatchDraft {
  name: string
  schedule: string
  sections: string[]
  look_out_for: string
  enabled: boolean
  thresholds: WatchThresholds
  /**
   * The channels chosen. null on a new watch the operator has not chosen for:
   * the page shows the channels that are on, the request leaves the field out,
   * and the server applies the same default.
   */
  notify: string[] | null
  send: WatchSend
}

/** The body of a create or change: a null notify is left out so the server's default applies. */
export function draftBody(d: WatchDraft): Omit<WatchDraft, 'notify'> & { notify?: string[] } {
  const { notify, ...rest } = d
  return notify === null ? rest : { ...rest, notify }
}

export function draftOf(w: WatchRecord): WatchDraft {
  return {
    name: w.name,
    schedule: w.schedule ?? '',
    sections: [...w.sections],
    look_out_for: w.look_out_for,
    enabled: w.enabled,
    thresholds: { ...w.thresholds },
    notify: [...w.notify],
    send: w.send,
  }
}

function sameChannels(a: string[] | null, b: string[] | null): boolean {
  if (a === null || b === null) return a === b
  return a.length === b.length && a.every((c) => b.includes(c))
}

export function sameDraft(a: WatchDraft, b: WatchDraft): boolean {
  return (
    a.name === b.name &&
    a.schedule === b.schedule &&
    a.look_out_for === b.look_out_for &&
    a.enabled === b.enabled &&
    a.send === b.send &&
    sameChannels(a.notify, b.notify) &&
    a.sections.length === b.sections.length &&
    a.sections.every((s) => b.sections.includes(s)) &&
    a.thresholds.alarms_raised === b.thresholds.alarms_raised &&
    a.thresholds.instrument_issues === b.thresholds.instrument_issues &&
    a.thresholds.tank_change_points === b.thresholds.tank_change_points &&
    a.thresholds.pressure_fall_hpa === b.thresholds.pressure_fall_hpa
  )
}

export interface WatchTemplate {
  id: 'morning' | 'overnight' | 'anchor'
  title: string
  detail: string
  draft: Pick<WatchDraft, 'name' | 'schedule' | 'sections'>
}

const ALL_SECTIONS: string[] = WATCH_SECTIONS.map((s) => s.id)

export const WATCH_TEMPLATES: WatchTemplate[] = [
  {
    id: 'morning',
    title: 'Morning report',
    detail: 'Daily 06:00',
    draft: { name: 'Morning report', schedule: '0 6 * * *', sections: ALL_SECTIONS },
  },
  {
    id: 'overnight',
    title: 'Overnight check',
    detail: 'Hourly 22:00–06:00',
    draft: { name: 'Overnight check', schedule: '0 22-23,0-6 * * *', sections: ['power', 'anchor', 'alarms', 'instruments'] },
  },
  {
    id: 'anchor',
    title: 'At anchor',
    detail: 'Every 2 hours',
    draft: { name: 'At anchor', schedule: '0 */2 * * *', sections: ['power', 'anchor', 'alarms', 'weather'] },
  },
]

/** A blank watch: every section, the server's default thresholds, switched on, sending wherever the server's default says. */
export function newWatchDraft(template?: WatchTemplate | null): WatchDraft {
  return {
    name: template?.draft.name ?? '',
    schedule: template?.draft.schedule ?? '0 6 * * *',
    sections: template ? [...template.draft.sections] : [...ALL_SECTIONS],
    look_out_for: '',
    enabled: true,
    thresholds: { ...DEFAULT_WATCH_THRESHOLDS },
    notify: null,
    send: 'findings',
  }
}

// The same limits the server enforces (record_type_watch.go, watches_report.go),
// so Save is blocked on what it would refuse. The server stays the authority.
export interface WatchDraftErrors {
  name?: string
  schedule?: string
  sections?: string
  look_out_for?: string
  alarms_raised?: string
  instrument_issues?: string
  tank_change_points?: string
  pressure_fall_hpa?: string
}

const isWhole = (n: number) => Number.isInteger(n)

export function validateWatchDraft(d: WatchDraft): WatchDraftErrors {
  const e: WatchDraftErrors = {}
  const name = d.name.trim()
  if (name === '') e.name = 'A watch needs a name.'
  else if ([...name].length > MAX_WATCH_NAME) e.name = `Keep the name to ${MAX_WATCH_NAME} characters.`
  if ([...d.look_out_for.trim()].length > MAX_LOOK_OUT_FOR) e.look_out_for = `Keep this to ${MAX_LOOK_OUT_FOR} characters.`
  if (d.sections.length === 0) e.sections = 'Pick at least one section.'
  const t = d.thresholds
  if (!isWhole(t.alarms_raised) || t.alarms_raised < 1 || t.alarms_raised > 100) e.alarms_raised = 'A whole number from 1 to 100.'
  if (!isWhole(t.instrument_issues) || t.instrument_issues < 1 || t.instrument_issues > 100) e.instrument_issues = 'A whole number from 1 to 100.'
  if (!(t.tank_change_points >= 1 && t.tank_change_points <= 100)) e.tank_change_points = 'From 1 to 100 points.'
  if (!(t.pressure_fall_hpa >= 0.5 && t.pressure_fall_hpa <= 30)) e.pressure_fall_hpa = 'From 0.5 to 30 hPa.'
  return e
}

export function hasDraftErrors(e: WatchDraftErrors): boolean {
  return Object.values(e).some(Boolean)
}

/** "$0.04", or "less than $0.01" for a real but tiny charge. */
export function formatCost(usd: number): string {
  if (usd > 0 && usd < 0.005) return 'less than $0.01'
  return `$${usd.toFixed(2)}`
}

/** The cost of a run: "cost not known" when Mate was asked and the charge could not be read, nothing when no model was asked. */
export function runCostLabel(run: Pick<WatchRun, 'cost_usd' | 'cost_known'>): string | null {
  if (!run.cost_known) return 'cost not known'
  if (run.cost_usd === null || run.cost_usd === 0) return null
  return formatCost(run.cost_usd)
}

export function relativeTime(iso: string, now: number = Date.now()): string {
  const then = new Date(iso).getTime()
  if (Number.isNaN(then)) return '--'
  const minutes = Math.round((now - then) / 60_000)
  if (minutes < 1) return 'just now'
  if (minutes < 60) return `${minutes} min ago`
  const hours = Math.round(minutes / 60)
  if (hours < 48) return `${hours} h ago`
  return `${Math.round(hours / 24)} days ago`
}

/** The boat-local time of a run, as the server worked it out when the run was made. Never the clock of the device reading it. */
export function runTimeLabel(run: Pick<WatchRun, 'boat_time'>): string {
  return run.boat_time
}

/** A one-off watch's start or end on the boat's clock, the same way. */
export function oneOffTimeLabel(w: Pick<WatchRecord, 'started_boat_time' | 'ends_boat_time'>, which: 'started' | 'ends'): string {
  return (which === 'started' ? w.started_boat_time : w.ends_boat_time) ?? '--'
}

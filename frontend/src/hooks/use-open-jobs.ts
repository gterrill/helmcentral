import { apiRequest, invalidateQueries, useApiQuery } from '@/lib/api-query'
import { todayISO } from '@/lib/local-date'
import { maintenanceLogRecordApplied, type MaintenanceLogEntry, type MaintenanceLogEntryInput } from '@/hooks/use-maintenance'

// ADR 0269: Open jobs - a known problem that is not fixed yet. One-off, with
// a done column, unlike a maintenance rule. Never called a "job" alone in
// code: use-maintenance.ts's "profile job" is a recurring service.
//
// The server never guesses the operator's date: opening and dropping a job
// carry `?today=` from the browser's local clock, the way every maintenance
// write does.

export type OpenJobStatus = 'open' | 'done' | 'dropped'
export type OpenJobSource = 'manual' | 'mate' | 'watch_finding' | 'alarm' | 'checklist_line'

/** openJob, backend/open_jobs.go. Every key is always present. */
export interface OpenJob {
  id: string
  equipment_id: string | null
  equipment_name: string
  title: string
  notes: string
  /** YYYY-MM-DD */
  opened_on: string
  /** YYYY-MM-DD, '' when none */
  before_on: string
  status: OpenJobStatus
  /** YYYY-MM-DD, '' while open */
  closed_on: string
  drop_reason: string
  /** The service log line Done wrote. */
  log_entry_id: string | null
  source: OpenJobSource
  /** An in-app path back to where it came from, '' when there is none. */
  source_link: string
  source_label: string
  created_at: string
  updated_at: string
  photo_ids: string[]
}

/** The body POST creates and PUT replaces. The three source fields count on
 * create only; an edit never moves where a job came from. */
export interface OpenJobInput {
  equipment_id: string | null
  title: string
  notes: string
  before_on: string
  source?: OpenJobSource
  source_link?: string
  source_label?: string
}

/** What "Add to jobs" starts a new job from: the words, the item if known, and
 * where it came from with a link back to it. */
export interface OpenJobDraft {
  title: string
  notes?: string
  equipment_id?: string | null
  source: OpenJobSource
  /** An in-app path (the server refuses anything else). */
  source_link?: string
  source_label?: string
}

/** The longest a title may be, in characters; the server refuses longer. */
export const OPEN_JOB_TITLE_MAX = 200

/** A title that fits, and the notes that keep the whole text when it did not. */
export function fitJobTitle(text: string, notes = ''): { title: string; notes: string } {
  const chars = Array.from(text.trim())
  if (chars.length <= OPEN_JOB_TITLE_MAX) return { title: chars.join(''), notes }
  return { title: `${chars.slice(0, OPEN_JOB_TITLE_MAX - 1).join('').trimEnd()}…`, notes: notes || text.trim() }
}

export interface OpenJobFilter {
  equipment?: string
  system?: string
  /** 'any' lists the closed jobs with the open ones. Default: open only. */
  status?: 'any'
}

const JOBS = '/api/inventory/open-jobs'
const NO_JOBS: OpenJob[] = []

function withToday(url: string): string {
  return `${url}${url.includes('?') ? '&' : '?'}today=${encodeURIComponent(todayISO())}`
}

/** GET /api/inventory/open-jobs. `filter === null` fetches nothing. */
export function useOpenJobs(filter: OpenJobFilter | null) {
  const params = new URLSearchParams()
  if (filter?.equipment) params.set('equipment', filter.equipment)
  if (filter?.system) params.set('system', filter.system)
  if (filter?.status) params.set('status', filter.status)
  const qs = params.toString()

  const { data, error, isLoading, refetch } = useApiQuery<OpenJob[]>({
    queryKey: [JOBS, { equipment: filter?.equipment ?? '', system: filter?.system ?? '', status: filter?.status ?? '' }],
    path: `${JOBS}${qs ? `?${qs}` : ''}`,
    enabled: filter !== null,
    select: (raw: { jobs?: OpenJob[] }) => raw.jobs ?? NO_JOBS,
  })

  return { jobs: data ?? NO_JOBS, loading: isLoading, error: error?.message ?? null, refresh: refetch }
}

/** POST /api/inventory/open-jobs */
export async function createOpenJob(input: OpenJobInput): Promise<OpenJob> {
  const data = await apiRequest<{ job: OpenJob }>(withToday(JOBS), { method: 'POST', json: input })
  invalidateQueries([JOBS])
  return data.job
}

/** PUT /api/inventory/open-jobs/:id */
export async function updateOpenJob(id: string, input: OpenJobInput): Promise<OpenJob> {
  const data = await apiRequest<{ job: OpenJob }>(`${JOBS}/${encodeURIComponent(id)}`, { method: 'PUT', json: input })
  invalidateQueries([JOBS])
  return data.job
}

/** POST /api/inventory/open-jobs/:id/done - the service log form. Writes one
 * log line and closes the job. The server checks the line's date against the
 * operator's today, so it is sent like every other dated write. */
export async function completeOpenJob(
  id: string,
  input: Omit<MaintenanceLogEntryInput, 'equipment_id'>,
): Promise<{ job: OpenJob; entry: MaintenanceLogEntry }> {
  const data = await apiRequest<{ job: OpenJob; entry: MaintenanceLogEntry }>(
    withToday(`${JOBS}/${encodeURIComponent(id)}/done`), { method: 'POST', json: input },
  )
  invalidateQueries([JOBS])
  // The new log line can move a rule (hand-read hours now come from log hours).
  maintenanceLogRecordApplied()
  return data
}

/** POST /api/inventory/open-jobs/:id/drop - closes the job with no log line.
 * The reason is optional. */
export async function dropOpenJob(id: string, reason: string): Promise<OpenJob> {
  const data = await apiRequest<{ job: OpenJob }>(
    withToday(`${JOBS}/${encodeURIComponent(id)}/drop`), { method: 'POST', json: { reason } },
  )
  invalidateQueries([JOBS])
  return data.job
}

/** POST /api/inventory/open-jobs/:id/photos - one multipart upload. */
export async function uploadOpenJobPhoto(id: string, file: Blob, filename: string): Promise<OpenJob> {
  const form = new FormData()
  form.append('file', file, filename)
  const data = await apiRequest<{ job: OpenJob }>(`${JOBS}/${encodeURIComponent(id)}/photos`, { method: 'POST', body: form })
  invalidateQueries([JOBS])
  return data.job
}

/** DELETE /api/inventory/open-jobs/:id/photos/:documentId - unlink, and with
 * deletePhoto also delete the document once nothing else shows it. */
export async function deleteOpenJobPhoto(id: string, documentId: string, deletePhoto = false): Promise<OpenJob> {
  const qs = deletePhoto ? '?delete=true' : ''
  const data = await apiRequest<{ job: OpenJob }>(
    `${JOBS}/${encodeURIComponent(id)}/photos/${encodeURIComponent(documentId)}${qs}`, { method: 'DELETE' },
  )
  invalidateQueries([JOBS])
  return data.job
}

/** Mate's Apply added a job. */
export function openJobRecordApplied(): void {
  invalidateQueries([JOBS])
}

/** Whole days from the day the job opened to `today` (or to the day it was
 * closed, for a closed job). Never negative. Both are YYYY-MM-DD, compared as
 * calendar days so no zone or daylight-saving shift moves the count. */
export function openJobAgeDays(job: Pick<OpenJob, 'opened_on' | 'closed_on' | 'status'>, today: string): number {
  const end = job.status !== 'open' && job.closed_on ? job.closed_on : today
  const days = (Date.parse(`${end}T00:00:00Z`) - Date.parse(`${job.opened_on}T00:00:00Z`)) / 86_400_000
  return Number.isFinite(days) ? Math.max(0, Math.round(days)) : 0
}

/** "1 Dec" from YYYY-MM-DD; read as UTC so no zone shifts the day. */
export function formatJobDate(ymd: string): string {
  const parts = new Intl.DateTimeFormat('en-GB', { timeZone: 'UTC', day: 'numeric', month: 'short' }).formatToParts(new Date(`${ymd}T00:00:00Z`))
  const get = (t: string) => parts.find((p) => p.type === t)?.value ?? ''
  return `${get('day')} ${get('month')}`
}

/** Where a job came from, in the operator's words. */
export const OPEN_JOB_SOURCE_LABELS: Record<OpenJobSource, string> = {
  manual: 'Added by hand',
  mate: 'Added by Mate',
  watch_finding: 'From a watch report',
  alarm: 'From an alarm',
  checklist_line: 'From a checklist',
}

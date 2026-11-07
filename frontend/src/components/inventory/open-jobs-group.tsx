import { useMemo, useState } from 'react'
import { Plus } from 'lucide-react'
import type { ColumnDef } from '@tanstack/react-table'

import { Button } from '@/components/ui/button'
import { IndexTable } from '@/components/patterns'
import { OpenJobDialog, OpenJobDoneDialog, OpenJobDropDialog } from '@/components/inventory/open-job-dialogs'
import {
  OPEN_JOB_SOURCE_LABELS,
  formatJobDate,
  openJobAgeDays,
  useOpenJobs,
  type OpenJob,
} from '@/hooks/use-open-jobs'
import type { EquipmentSystem } from '@/hooks/use-inventory'
import { todayISO } from '@/lib/local-date'

// ADR 0269: the Open jobs group. A list of known problems that are not fixed
// yet, at the top of Inventory, Maintenance (above Overdue) and on each
// item's own page. A list, not a board: no assignment, no comments, no
// priority. Each row shows its age and the date it should be done before.
// Done writes the service log; Drop closes it with an optional reason. Jobs
// are not alarms and are never counted as alarms.

interface OpenJobsGroupProps {
  /** Narrows to one item's jobs (the item's own page). */
  equipmentId?: string
  /** Narrows to one system's gear (the Maintenance list's filter). */
  system?: EquipmentSystem | ''
  /** Opens the item a job is on. Left out on an item's own page, where the item is not repeated. */
  onOpenEquipment?: (id: string) => void
  canWrite?: boolean
}

function sourceLine(job: OpenJob) {
  if (job.source === 'manual') return null
  const text = job.source_label ? `${OPEN_JOB_SOURCE_LABELS[job.source]}: ${job.source_label}` : OPEN_JOB_SOURCE_LABELS[job.source]
  if (!job.source_link) return <span className="truncate text-xs text-muted-foreground">{text}</span>
  return (
    <a href={job.source_link} className="truncate text-xs text-muted-foreground underline-offset-2 hover:underline">
      {text}
    </a>
  )
}

function closedLine(job: OpenJob): string {
  if (job.status === 'done') return `Done ${job.closed_on ? formatJobDate(job.closed_on) : ''}`.trim()
  const when = `Dropped ${job.closed_on ? formatJobDate(job.closed_on) : ''}`.trim()
  return job.drop_reason ? `${when}: ${job.drop_reason}` : when
}

export function OpenJobsGroup({ equipmentId, system = '', onOpenEquipment, canWrite = true }: OpenJobsGroupProps) {
  const [showClosed, setShowClosed] = useState(false)
  const { jobs, loading, error, refresh } = useOpenJobs({
    equipment: equipmentId,
    system: system || undefined,
    status: showClosed ? 'any' : undefined,
  })

  const [adding, setAdding] = useState(false)
  const [editing, setEditing] = useState<OpenJob | null>(null)
  const [finishing, setFinishing] = useState<OpenJob | null>(null)
  const [dropping, setDropping] = useState<OpenJob | null>(null)

  const today = todayISO()
  const open = useMemo(() => jobs.filter((j) => j.status === 'open'), [jobs])
  const closed = useMemo(() => jobs.filter((j) => j.status !== 'open').sort((a, b) => b.closed_on.localeCompare(a.closed_on)), [jobs])

  const columns = useMemo<ColumnDef<OpenJob, unknown>[]>(
    () => [
      {
        id: 'job',
        header: 'Job',
        enableSorting: false,
        cell: ({ row }) => {
          const job = row.original
          return (
            <div className="flex min-w-0 flex-col">
              {job.equipment_id && onOpenEquipment && (
                <button type="button" onClick={() => onOpenEquipment(job.equipment_id!)} className="truncate text-left text-xs font-medium text-muted-foreground hover:underline">
                  {job.equipment_name}
                </button>
              )}
              {canWrite ? (
                <button type="button" onClick={() => setEditing(job)} className="truncate text-left font-medium hover:underline">
                  {job.title}
                </button>
              ) : (
                <span className="truncate font-medium">{job.title}</span>
              )}
              {sourceLine(job)}
            </div>
          )
        },
      },
      {
        id: 'age',
        header: 'Age',
        enableSorting: false,
        accessorFn: (job) => `${openJobAgeDays(job, today)} d`,
        cell: ({ row }) => <span className="tabular-nums text-muted-foreground">{openJobAgeDays(row.original, today)} d</span>,
      },
      {
        id: 'before',
        header: 'Before',
        enableSorting: false,
        accessorFn: (job) => job.before_on,
        cell: ({ row }) => {
          const { before_on: before } = row.original
          if (!before) return <span className="text-muted-foreground">--</span>
          return (
            <span className="tabular-nums text-muted-foreground">
              {formatJobDate(before)}{before < today ? ' (passed)' : ''}
            </span>
          )
        },
      },
      ...(canWrite
        ? [{
            id: 'actions',
            header: '',
            enableSorting: false,
            cell: ({ row }) => (
              <div className="flex justify-end gap-1">
                <Button type="button" variant="ghost" size="sm" onClick={() => setDropping(row.original)}>
                  Drop
                </Button>
                <Button type="button" size="sm" onClick={() => setFinishing(row.original)}>
                  Done
                </Button>
              </div>
            ),
          } satisfies ColumnDef<OpenJob, unknown>]
        : []),
    ],
    [canWrite, onOpenEquipment, today],
  )

  return (
    <section aria-labelledby="open-jobs-heading" className="flex flex-col gap-2">
      <div className="flex items-center gap-2">
        <h3 id="open-jobs-heading" className="text-sm font-semibold">
          Open jobs{open.length > 0 ? <span className="ml-1.5 font-normal tabular-nums text-muted-foreground">{open.length}</span> : null}
        </h3>
        <div className="ml-auto flex items-center gap-1">
          <Button type="button" variant="ghost" size="sm" onClick={() => setShowClosed((v) => !v)}>
            {showClosed ? 'Hide closed' : 'Show closed'}
          </Button>
          {canWrite && (
            <Button type="button" variant="outline" size="sm" className="gap-1.5" onClick={() => setAdding(true)}>
              <Plus className="h-3.5 w-3.5" aria-hidden="true" />
              Add job
            </Button>
          )}
        </div>
      </div>

      <IndexTable
        className="flex-none"
        columns={columns}
        rows={open}
        getRowId={(job) => job.id}
        loading={loading}
        error={error}
        onRetry={() => { void refresh() }}
        empty={<p className="px-4 py-3 text-sm text-muted-foreground">No open jobs.</p>}
      />

      {showClosed && closed.length > 0 && (
        <ul aria-label="Closed jobs" className="flex flex-col divide-y divide-border rounded-md border border-border bg-card text-sm">
          {closed.map((job) => (
            <li key={job.id} className="flex flex-col gap-0.5 px-3 py-2">
              <span className="font-medium">{job.title}</span>
              <span className="text-xs text-muted-foreground">
                {job.equipment_name && !equipmentId ? `${job.equipment_name} · ` : ''}
                {closedLine(job)} · open {openJobAgeDays(job, today)} d
              </span>
            </li>
          ))}
        </ul>
      )}

      <OpenJobDialog
        open={adding || editing !== null}
        job={adding ? null : editing}
        presetEquipmentId={equipmentId}
        onClose={() => { setAdding(false); setEditing(null) }}
      />
      <OpenJobDoneDialog job={finishing} onClose={() => setFinishing(null)} />
      <OpenJobDropDialog job={dropping} onClose={() => setDropping(null)} />
    </section>
  )
}

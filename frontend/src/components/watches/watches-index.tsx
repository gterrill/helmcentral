import { useMemo } from 'react'
import { Eye, Plus } from 'lucide-react'
import type { ColumnDef } from '@tanstack/react-table'

import { EmptyState, IndexTable, Page } from '@/components/patterns'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { formatBytes } from '@/lib/document-display'
import {
  formatCost,
  relativeTime,
  WATCH_TEMPLATES,
  type WatchList,
  type WatchRecord,
  type WatchTemplate,
} from '@/lib/watches'

// ADR 0227: the Watches index. A row per watch: what it is called, when it
// runs, when it last ran and whether that run found anything. Colour marks
// only a watch that found something.

export interface WatchesIndexProps {
  watches: WatchRecord[]
  /** The most scheduled watches allowed; null until the server has said. */
  max: number | null
  cost24h: WatchList['cost_24h'] | null
  storageBytes: number | null
  /** Why the scheduler is stopped, in the server's words. */
  problem: string | null
  loading: boolean
  error: string | null
  canWrite: boolean
  onRetry: () => void
  onOpen: (id: string) => void
  onNew: (template: WatchTemplate | null) => void
}

function stateOf(w: WatchRecord): { label: string; found: boolean } {
  if (w.running) return { label: 'Running', found: false }
  if (!w.last_run) return { label: '--', found: false }
  if (w.last_run.outcome === 'skipped') return { label: 'Skipped', found: false }
  if (w.has_finding) return { label: 'Found something', found: true }
  return { label: 'Nothing found', found: false }
}

/** The line under the table: space used, and what Mate's reviews cost in the last day. */
export function watchesFooter(storageBytes: number | null, cost: WatchList['cost_24h'] | null): string[] {
  const lines: string[] = []
  if (storageBytes !== null) lines.push(`Watch history uses ${formatBytes(storageBytes)}.`)
  if (cost) {
    if (cost.unknown_runs > 0 && cost.usd > 0) {
      lines.push(`Watches cost at least ${formatCost(cost.usd)} in the last 24 hours. Cost not known for ${cost.unknown_runs} ${cost.unknown_runs === 1 ? 'run' : 'runs'}.`)
    } else if (cost.unknown_runs > 0) {
      lines.push('Watch cost in the last 24 hours: cost not known.')
    } else if (cost.usd > 0) {
      lines.push(`Watches cost ${formatCost(cost.usd)} in the last 24 hours.`)
    }
  }
  return lines
}

export function WatchesIndex({
  watches,
  max,
  cost24h,
  storageBytes,
  problem,
  loading,
  error,
  canWrite,
  onRetry,
  onOpen,
  onNew,
}: WatchesIndexProps) {
  const scheduledCount = watches.filter((w) => w.kind === 'scheduled').length
  const atLimit = max !== null && scheduledCount >= max

  const columns = useMemo<ColumnDef<WatchRecord, unknown>[]>(
    () => [
      {
        id: 'name',
        header: 'Name',
        accessorFn: (w) => w.name,
        cell: ({ row }) => (
          <span className="flex min-w-0 items-center gap-2">
            <span className="min-w-0 truncate font-medium text-foreground">{row.original.name}</span>
            {row.original.kind === 'scheduled' && !row.original.enabled && <Badge variant="outline">Off</Badge>}
          </span>
        ),
      },
      {
        id: 'schedule',
        header: 'Runs',
        accessorFn: (w) => w.schedule_sentence,
        cell: ({ row }) => <span className="text-muted-foreground">{row.original.schedule_sentence}</span>,
      },
      {
        id: 'last-run',
        header: 'Last run',
        accessorFn: (w) => (w.last_run ? relativeTime(w.last_run.started_at) : 'Not yet'),
        enableSorting: false,
        cell: ({ row }) => {
          const run = row.original.last_run
          if (!run) return <span className="text-muted-foreground">Not yet</span>
          return (
            <span className="text-muted-foreground tabular-nums">
              {relativeTime(run.started_at)}
              {run.outcome === 'skipped' ? ' · skipped' : ''}
            </span>
          )
        },
      },
      {
        id: 'state',
        header: 'Result',
        accessorFn: (w) => stateOf(w).label,
        enableSorting: false,
        cell: ({ row }) => {
          const s = stateOf(row.original)
          return s.found ? (
            <Badge variant="outline" className="border-amber-500/50 text-amber-700 dark:text-amber-400">{s.label}</Badge>
          ) : (
            <span className="text-muted-foreground">{s.label}</span>
          )
        },
      },
    ],
    [],
  )

  const templates = (
    <EmptyState
      icon={<Eye className="h-8 w-8 text-muted-foreground" aria-hidden="true" />}
      title="No watches yet"
      description="A watch checks the boat on a schedule and writes you a short report. Start from one of these, then change it as you like."
      action={
        canWrite ? (
          <div className="flex flex-wrap items-stretch justify-center gap-2">
            {WATCH_TEMPLATES.map((t) => (
              <Button key={t.id} type="button" variant="outline" className="h-auto flex-col items-start gap-0.5 py-2" onClick={() => onNew(t)}>
                <span className="font-medium">{t.title}</span>
                <span className="text-xs font-normal text-muted-foreground">{t.detail}</span>
              </Button>
            ))}
          </div>
        ) : undefined
      }
    />
  )

  const footer = watchesFooter(storageBytes, cost24h)

  return (
    <div className="h-full overflow-y-auto p-4">
      <Page
        title="Watches"
        primaryAction={
          canWrite
            ? {
                label: 'New watch',
                icon: <Plus className="h-4 w-4" data-icon="inline-start" aria-hidden="true" />,
                onClick: () => onNew(null),
                disabled: atLimit,
              }
            : undefined
        }
      >
        {problem && (
          <p role="alert" className="rounded-md border border-destructive/40 bg-destructive/10 px-3 py-2 text-sm font-medium text-destructive">
            {problem}
          </p>
        )}
        <IndexTable
          columns={columns}
          rows={watches}
          getRowId={(w) => w.id}
          rowActionsLabel={(w) => `Actions for ${w.name}`}
          onOpen={(w) => onOpen(w.id)}
          loading={loading}
          error={error}
          onRetry={onRetry}
          empty={templates}
          initialSorting={[{ id: 'name', desc: false }]}
        />
        {!loading && (
          <div className="flex flex-col gap-0.5 text-xs text-muted-foreground" data-testid="watches-footer">
            {footer.map((line) => <p key={line}>{line}</p>)}
            {atLimit && <p>You have reached the limit of {max} scheduled watches.</p>}
          </div>
        )}
      </Page>
    </div>
  )
}

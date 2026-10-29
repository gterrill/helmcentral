import { useState } from 'react'
import { Sparkles, Trash2 } from 'lucide-react'

import { AssistantMarkdown } from '@/components/assistant-markdown'
import { ConfirmDelete } from '@/components/patterns'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  runCostLabel,
  runTimeLabel,
  WATCH_SECTIONS,
  type WatchRun,
  type WatchSamplesReport,
  type WatchSectionsReport,
  type WatchSeriesReport,
} from '@/lib/watches'

// ADR 0227: a watch's run history, newest first. A run is a report, the
// findings the rules reached, Mate's review, and what it cost.

const MICRO_LABEL = 'text-[10px] font-normal uppercase tracking-[0.16em] text-muted-foreground'

const sectionLabel = (id: string) => WATCH_SECTIONS.find((s) => s.id === id)?.label ?? id

function figure(n: number | undefined): string | null {
  if (n === undefined) return null
  return Number.isInteger(n) ? String(n) : String(Math.round(n * 100) / 100)
}

function seriesLine(s: WatchSeriesReport): string {
  const units = s.units ? ` ${s.units}` : ''
  const parts: string[] = []
  const min = figure(s.min)
  const max = figure(s.max)
  const mean = figure(s.mean)
  const last = figure(s.last)
  if (min !== null && max !== null) parts.push(`${min} to ${max}${units}`)
  if (mean !== null) parts.push(`mean ${mean}`)
  if (last !== null) parts.push(`last ${last}`)
  if (s.gap_count) parts.push(`${s.gap_count} ${s.gap_count === 1 ? 'gap' : 'gaps'}${s.gap_seconds ? ` (${s.gap_seconds} s)` : ''}`)
  parts.push(`${s.excursion_count} ${s.excursion_count === 1 ? 'excursion' : 'excursions'}`)
  return parts.join(' · ')
}

function SamplesReport({ report }: { report: WatchSamplesReport }) {
  return (
    <div className="flex flex-col gap-1.5" data-testid="watch-samples-report">
      <p className="text-xs text-muted-foreground">
        {report.reason ? `${report.reason} · ` : ''}{report.minutes} min, {report.started} to {report.ended}
        {report.timezone ? ` ${report.timezone}` : ''}
      </p>
      <ul className="flex flex-col gap-1">
        {report.series.map((s) => (
          <li key={s.label} className="min-w-0 text-xs">
            <span className="font-medium text-foreground">{s.label}</span>
            <span className="text-muted-foreground"> {seriesLine(s)}</span>
          </li>
        ))}
        {report.difference && (
          <li className="min-w-0 text-xs">
            <span className="font-medium text-foreground">{report.difference.label}</span>
            <span className="text-muted-foreground"> {seriesLine(report.difference)}</span>
          </li>
        )}
      </ul>
      {report.difference_note && <p className="text-xs text-muted-foreground">{report.difference_note}</p>}
      {report.truncated && <p className="text-xs text-muted-foreground">The report was cut short to fit.</p>}
    </div>
  )
}

function SectionsReport({ report }: { report: WatchSectionsReport }) {
  return (
    <div className="flex flex-col gap-2" data-testid="watch-sections-report">
      <p className="text-xs text-muted-foreground">
        Since {report.since}{report.since_note ? `. ${report.since_note}` : ''}
      </p>
      {report.sections.map((section) => (
        <div key={section.name} className="flex flex-col gap-0.5">
          <p className={MICRO_LABEL}>{section.title || sectionLabel(section.name)}</p>
          {section.unavailable ? (
            <p className="text-xs text-muted-foreground">Unavailable: {section.unavailable}</p>
          ) : (
            (section.lines ?? []).map((line, i) => <p key={i} className="min-w-0 text-xs text-foreground">{line}</p>)
          )}
        </div>
      ))}
    </div>
  )
}

export interface WatchRunCardProps {
  run: WatchRun
  canWrite: boolean
  onAskMate?: (run: WatchRun) => void
  onDelete: (run: WatchRun) => void
}

export function WatchRunCard({ run, canWrite, onAskMate, onDelete }: WatchRunCardProps) {
  const cost = run.outcome === 'ran' ? runCostLabel(run) : null
  const found = run.findings.length > 0
  return (
    <article
      className="flex min-w-0 flex-col gap-3 rounded-md border border-border bg-background p-3"
      data-testid="watch-run"
      aria-label={`Run ${runTimeLabel(run)}`}
    >
      <header className="flex flex-wrap items-center gap-x-3 gap-y-1">
        <h3 className="text-sm font-medium tabular-nums text-foreground">{runTimeLabel(run)}</h3>
        <Badge variant={run.outcome === 'ran' ? 'secondary' : 'outline'}>{run.outcome === 'ran' ? 'Ran' : 'Skipped'}</Badge>
        {found && (
          <Badge variant="outline" className="border-amber-500/50 text-amber-700 dark:text-amber-400">
            {run.findings.length === 1 ? '1 finding' : `${run.findings.length} findings`}
          </Badge>
        )}
        {cost && <span className="ml-auto text-xs text-muted-foreground">{cost}</span>}
      </header>

      {run.outcome === 'skipped' && (
        <p className="text-sm text-muted-foreground">{run.skip_reason || 'Skipped.'}</p>
      )}

      {found && (
        <ul className="flex flex-col gap-1" aria-label="Findings">
          {run.findings.map((f, i) => (
            <li key={i} className="flex min-w-0 flex-wrap items-baseline gap-x-2 text-sm text-foreground">
              <span className={MICRO_LABEL}>{sectionLabel(f.section)}</span>
              <span className="min-w-0">{f.text}</span>
              {f.by === 'mate' && <Badge variant="secondary">Mate</Badge>}
            </li>
          ))}
        </ul>
      )}

      {run.report && run.report_kind === 'sections' && <SectionsReport report={run.report as WatchSectionsReport} />}
      {run.report && run.report_kind === 'samples' && <SamplesReport report={run.report as WatchSamplesReport} />}

      {run.outcome === 'ran' && (
        <div className="flex flex-col gap-1 border-t border-border pt-2" data-testid="watch-run-review">
          <p className={MICRO_LABEL}>Mate&apos;s review</p>
          {run.review ? (
            <AssistantMarkdown content={run.review} />
          ) : (
            <p className="text-xs text-muted-foreground">{run.review_problem || 'Mate did not review this run.'}</p>
          )}
        </div>
      )}

      <div className="flex flex-wrap items-center gap-2">
        {onAskMate && (
          <Button type="button" variant="outline" size="sm" onClick={() => onAskMate(run)}>
            <Sparkles className="h-4 w-4" data-icon="inline-start" aria-hidden="true" />
            Ask Mate about this run
          </Button>
        )}
        {canWrite && (
          <Button type="button" variant="ghost" size="sm" onClick={() => onDelete(run)}>
            <Trash2 className="h-4 w-4" data-icon="inline-start" aria-hidden="true" />
            Delete run
          </Button>
        )}
      </div>
    </article>
  )
}

export interface WatchRunListProps {
  runs: WatchRun[]
  hasOlder: boolean
  canWrite: boolean
  loadingOlder: boolean
  olderError: string | null
  onShowOlder: () => void
  onAskMate?: (run: WatchRun) => void
  /** Deletes the run; a refusal throws with the server's own words. */
  onDeleteRun: (run: WatchRun) => Promise<void>
}

export function WatchRunList({
  runs,
  hasOlder,
  canWrite,
  loadingOlder,
  olderError,
  onShowOlder,
  onAskMate,
  onDeleteRun,
}: WatchRunListProps) {
  const [pending, setPending] = useState<WatchRun | null>(null)
  const [deleting, setDeleting] = useState(false)
  const [deleteError, setDeleteError] = useState<string | null>(null)

  if (runs.length === 0) {
    return <p className="text-sm text-muted-foreground">No runs yet. The first one appears here when the watch next runs.</p>
  }

  const confirm = async () => {
    if (!pending) return
    setDeleting(true)
    setDeleteError(null)
    try {
      await onDeleteRun(pending)
      setPending(null)
    } catch (err) {
      setDeleteError(err instanceof Error ? err.message : String(err))
    } finally {
      setDeleting(false)
    }
  }

  return (
    <div className="flex flex-col gap-3">
      {runs.map((run) => (
        <WatchRunCard
          key={run.id}
          run={run}
          canWrite={canWrite}
          onAskMate={onAskMate}
          onDelete={(r) => { setDeleteError(null); setPending(r) }}
        />
      ))}
      {hasOlder && (
        <div className="flex flex-col items-start gap-1">
          <Button type="button" variant="outline" size="sm" disabled={loadingOlder} onClick={onShowOlder}>
            {loadingOlder ? 'Loading...' : 'Show older'}
          </Button>
          <p className="text-xs text-muted-foreground">Showing the newest {runs.length} runs.</p>
        </div>
      )}
      {olderError && <p role="alert" className="text-sm text-destructive">{olderError}</p>}
      <ConfirmDelete
        open={pending !== null}
        onOpenChange={(open) => { if (!open) { setPending(null); setDeleteError(null) } }}
        title="Delete this run?"
        description={deleteError ?? 'The report, findings and Mate’s review of this run are removed. This can’t be undone.'}
        confirmLabel="Delete run"
        deleting={deleting}
        onConfirm={() => { void confirm() }}
      />
    </div>
  )
}

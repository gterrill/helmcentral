import { Fragment, memo, useCallback, useState, type CSSProperties } from 'react'
import { Sparkles, Trash2 } from 'lucide-react'

import { AssistantMarkdown } from '@/components/assistant-markdown'
import { ConfirmDelete } from '@/components/patterns'
import { Accordion, AccordionContent, AccordionItem, AccordionTrigger } from '@/components/ui/accordion'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { WatchChart } from '@/components/watches/watch-chart'
import {
  channelLabel,
  runCostLabel,
  runTimeLabel,
  WATCH_SECTIONS,
  type WatchRun,
  type WatchSamplesReport,
  type WatchSectionsReport,
  type WatchSeriesReport,
} from '@/lib/watches'
import { errorMessage } from '@/lib/errors'

// ADR 0227: a watch's run history, newest first. A run is a report, the
// findings the rules reached, Mate's review, and what it cost. In the list
// each run is its summary until it is opened; the newest starts open.

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
      <p className="text-sm text-muted-foreground">
        {report.reason ? `${report.reason} · ` : ''}{report.minutes} min, {report.started} to {report.ended}
        {report.timezone ? ` ${report.timezone}` : ''}
      </p>
      <ul className="flex flex-col gap-1">
        {report.series.map((s) => (
          <li key={s.label} className="min-w-0 text-sm">
            <span className="font-medium text-foreground">{s.label}</span>
            <span className="text-muted-foreground"> {seriesLine(s)}</span>
          </li>
        ))}
        {report.difference && (
          <li className="min-w-0 text-sm">
            <span className="font-medium text-foreground">{report.difference.label}</span>
            <span className="text-muted-foreground"> {seriesLine(report.difference)}</span>
          </li>
        )}
      </ul>
      {report.difference_note && <p className="text-sm text-muted-foreground">{report.difference_note}</p>}
      {report.truncated && <p className="text-sm text-muted-foreground">The report was cut short to fit.</p>}
    </div>
  )
}

/**
 * The report's sections in fixed places. One column on a phone, in the
 * report's own order. From sm up, and only when the report has a Tanks section,
 * two columns: Tanks on the right beside the other sections, Alarms across the
 * full width below both. Without Tanks it stays one column, so the right half is
 * not left empty. The grid rows are
 * set from the number of left-hand sections (CSS variables), with a flexible
 * row for Tanks to span, so a tall Tanks section never opens gaps between the
 * sections beside it.
 */
function SectionsReport({ report }: { report: WatchSectionsReport }) {
  const hasTanks = report.sections.some((s) => s.name === 'tanks')
  const left = report.sections.filter((s) => s.name !== 'tanks' && s.name !== 'alarms').length
  const leftRows = left > 0 ? `repeat(${left}, auto) ` : ''
  const rows = hasTanks ? `${leftRows}1fr auto` : `${leftRows}auto`
  const style = {
    '--readings-rows': rows,
    '--readings-tanks-end': left + 2,
    '--readings-alarms-row': hasTanks ? left + 2 : left + 1,
  } as CSSProperties
  return (
    <div
      className={`grid min-w-0 grid-cols-1 gap-x-6 ${hasTanks ? 'sm:grid-cols-2 sm:[grid-template-rows:var(--readings-rows)]' : ''}`}
      style={style}
      data-testid="watch-sections-report"
    >
      {report.sections.map((section) => {
        const place = !hasTanks
          ? ''
          : section.name === 'tanks'
            ? 'sm:col-start-2 sm:[grid-row:1/var(--readings-tanks-end)]'
            : section.name === 'alarms'
              ? 'sm:col-span-2 sm:[grid-row:var(--readings-alarms-row)]'
              : 'sm:col-start-1'
        return (
          <div key={section.name} data-section={section.name} className={`flex min-w-0 flex-col gap-0.5 pb-3 ${place}`}>
            <p className="text-sm font-semibold text-foreground">{section.title || sectionLabel(section.name)}</p>
            {section.unavailable ? (
              <p className="min-w-0 text-sm text-muted-foreground">Unavailable: {section.unavailable}</p>
            ) : (
              (section.lines ?? []).map((line, i) => <p key={i} className="min-w-0 text-sm text-foreground">{line}</p>)
            )}
          </div>
        )
      })}
    </div>
  )
}

export interface WatchRunCardProps {
  run: WatchRun
  /** Set on the run a link points at: whether it is marked right now. */
  highlighted?: boolean
  onAskMate?: (run: WatchRun) => void
}

/**
 * What a run shows when it is closed: when, how many findings, anything that
 * went wrong, and (only while it is closed) the opening line of Mate's review.
 */
function RunSummary({ run, opening }: { run: WatchRun; opening?: string }) {
  const count = run.findings.length
  const undelivered = (run.notify_pending ?? []).length > 0
  return (
    <span className="flex min-w-0 flex-1 flex-col gap-0.5">
      <span className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1">
        <span className="text-sm font-medium tabular-nums text-foreground">{runTimeLabel(run)}</span>
        {count > 0 && (
          <Badge variant="outline" className="border-amber-500/50 text-amber-700 dark:text-amber-400">
            {count === 1 ? '1 finding' : `${count} findings`}
          </Badge>
        )}
        {undelivered && <Badge variant="outline" className="border-destructive/50 text-destructive">Not delivered</Badge>}
        {run.not_sent && <Badge variant="outline">Not sent</Badge>}
        {run.outcome === 'ran' && !run.review && run.report_kind !== 'samples' && <Badge variant="outline">No review</Badge>}
        {run.outcome === 'skipped' && <Badge variant="outline">Skipped</Badge>}
      </span>
      {opening && <span className="block min-w-0 truncate text-sm font-normal text-muted-foreground">{opening}</span>}
    </span>
  )
}

interface RunBodyProps {
  run: WatchRun
  onAskMate?: (run: WatchRun) => void
  /** Left out where the run can't be deleted. */
  onDelete?: (run: WatchRun) => void
}

/**
 * Everything else about a run: findings, charts, report, Mate's review, who was
 * told, and the actions. Memoised so opening one run doesn't redraw the charts
 * of the others.
 */
const RunBody = memo(function RunBody({ run, onAskMate, onDelete }: RunBodyProps) {
  const sections = run.report && run.report_kind === 'sections' ? (run.report as WatchSectionsReport) : null
  const samples = run.report && run.report_kind === 'samples' ? (run.report as WatchSamplesReport) : null
  // The server owns the sentences: the opening is its split of the review, and
  // a run with no review says so rather than being given a summary here.
  // A one-off watch's report goes to the chat that asked for it, so it has no
  // review to miss.
  const opening =
    run.outcome === 'skipped'
      ? run.skip_reason || 'Skipped.'
      : run.review_opening
        ? run.review_opening
        : !run.review && !samples
          ? `No review. ${run.review_problem || 'Mate did not review this run.'}`
          : null
  const reviewText = run.review_rest ?? ''
  const cost = run.outcome === 'ran' ? runCostLabel(run) : null
  const told = run.notified.length > 0 || (run.notify_pending ?? []).length > 0 || run.not_sent || (run.notify_notes ?? []).length > 0
  const hasFooter = onAskMate || onDelete || told || cost
  return (
    <div className="flex min-w-0 flex-col gap-4">
      {sections && (
        <p className="min-w-0 text-sm text-muted-foreground">
          Since {sections.since}{sections.since_note ? `. ${sections.since_note}` : ''}
        </p>
      )}

      {opening && (
        <p className="min-w-0 text-xl font-semibold text-foreground" data-testid="watch-run-opening">
          {opening}
        </p>
      )}

      {run.findings.length > 0 && (
        <div className="flex min-w-0 flex-col gap-2 border-t border-border pt-4">
          <p className={MICRO_LABEL}>Needs a look</p>
          <ul className="flex flex-col gap-3" aria-label="Findings">
            {run.findings.map((f, i) => (
              <li key={i} className="flex min-w-0 flex-col gap-1.5 text-foreground">
                {f.by === 'mate' ? (
                  <div className="flex min-w-0 flex-wrap items-baseline gap-x-2 text-sm">
                    <Badge variant="secondary">Mate</Badge>
                    <span className="min-w-0">{f.text}</span>
                  </div>
                ) : (
                  <span className="min-w-0 text-base font-medium">{f.text}</span>
                )}
                {(run.charts ?? [])
                  .filter((c) => c.finding_index === i)
                  .map((c, ci) => (
                    <WatchChart key={ci} chart={c} />
                  ))}
                {f.chart_problem && <p className="min-w-0 text-xs text-muted-foreground">{f.chart_problem}</p>}
              </li>
            ))}
          </ul>
        </div>
      )}

      {reviewText && (
        <div className="flex min-w-0 flex-col gap-1 border-t border-border pt-4" data-testid="watch-run-review">
          <p className={MICRO_LABEL}>Mate&apos;s review</p>
          <AssistantMarkdown content={reviewText} />
        </div>
      )}

      {(sections || samples) && (
        <div className="flex min-w-0 flex-col gap-2 border-t border-border pt-4">
          <p className={MICRO_LABEL}>Readings</p>
          {sections && <SectionsReport report={sections} />}
          {samples && <SamplesReport report={samples} />}
        </div>
      )}

      {hasFooter && (
        <div
          className="flex min-w-0 flex-col gap-3 border-t border-border pt-4 sm:flex-row sm:items-start sm:justify-between"
          data-testid="watch-run-footer"
        >
          <div className="flex flex-wrap items-center gap-2">
            {onAskMate && (
              <Button type="button" variant="outline" size="sm" onClick={() => onAskMate(run)}>
                <Sparkles className="h-4 w-4" data-icon="inline-start" aria-hidden="true" />
                Ask Mate about this run
              </Button>
            )}
            {onDelete && (
              <Button type="button" variant="ghost" size="sm" onClick={() => onDelete(run)}>
                <Trash2 className="h-4 w-4" data-icon="inline-start" aria-hidden="true" />
                Delete run
              </Button>
            )}
          </div>
          {(told || cost) && (
            <div className="flex min-w-0 flex-col gap-0.5 text-xs text-muted-foreground sm:items-end sm:text-right">
              {told && (
                <div className="flex min-w-0 flex-col gap-0.5 sm:items-end" data-testid="watch-run-notified">
                  {run.not_sent && <p className="min-w-0">Not sent: {run.not_sent}</p>}
                  {run.notified.length > 0 && <p>Notified: {run.notified.map(channelLabel).join(', ')}</p>}
                  {(run.notify_notes ?? []).map((n) => (
                    <p key={n} className="min-w-0">
                      {n}
                    </p>
                  ))}
                  {(run.notify_pending ?? []).map((p) => (
                    <p key={p.channel} className="min-w-0 text-destructive">
                      Not delivered yet to {channelLabel(p.channel)}: {p.reason}
                    </p>
                  ))}
                </div>
              )}
              {cost && <p>{cost}</p>}
            </div>
          )}
        </div>
      )}
    </div>
  )
})

/** A run shown fully open, as the linked run is pinned above the list. */
export function WatchRunCard({ run, highlighted = false, onAskMate }: WatchRunCardProps) {
  return (
    <article
      className={`flex min-w-0 flex-col gap-4 rounded-md border bg-card p-4 transition-colors ${
        highlighted ? 'border-primary ring-2 ring-primary/40' : 'border-border'
      }`}
      data-testid="watch-run"
      data-highlighted={String(highlighted)}
      aria-label={`Run ${runTimeLabel(run)}`}
      id={`run-${run.id}`}
    >
      <h3 className="flex">
        <RunSummary run={run} />
      </h3>
      <RunBody run={run} onAskMate={onAskMate} />
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
  /** Open the newest run when it arrives. Off while a linked run is pinned above the list. */
  expandNewest: boolean
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
  expandNewest,
}: WatchRunListProps) {
  const [pending, setPending] = useState<WatchRun | null>(null)
  const [deleting, setDeleting] = useState(false)
  const [deleteError, setDeleteError] = useState<string | null>(null)
  // Which runs are open. A run that becomes the newest (first load, Run now, a
  // live refresh, the newest being deleted) opens during render so no frame
  // shows it closed, and takes over from the run opened that way before it, so
  // a page left open doesn't collect a run per report. A run opened by hand
  // stays open. Showing older runs changes the list's tail, never its head, so
  // nothing opens or closes then.
  const newestId = expandNewest ? runs[0]?.id ?? null : null
  const [seenNewestId, setSeenNewestId] = useState<string | null>(null)
  const [openIds, setOpenIds] = useState<string[]>([])
  const [autoOpenedId, setAutoOpenedId] = useState<string | null>(null)
  if (newestId !== seenNewestId) {
    setSeenNewestId(newestId)
    const kept = openIds.filter((id) => id !== autoOpenedId)
    const opens = newestId !== null && !kept.includes(newestId)
    setOpenIds(opens ? [...kept, newestId] : kept)
    setAutoOpenedId(opens ? newestId : null)
  }
  const changeOpen = (ids: string[]) => {
    setOpenIds(ids)
    // Closed by hand: if it is opened again, that was by hand too.
    if (autoOpenedId !== null && !ids.includes(autoOpenedId)) setAutoOpenedId(null)
  }
  const askToDelete = useCallback((run: WatchRun) => {
    setDeleteError(null)
    setPending(run)
  }, [])

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
      setDeleteError(errorMessage(err))
    } finally {
      setDeleting(false)
    }
  }

  return (
    <div className="flex flex-col gap-3">
      <Accordion multiple value={openIds} onValueChange={changeOpen} className="gap-3">
        {runs.map((run, index) => (
          <Fragment key={run.id}>
            {/* With a linked run pinned above, none of these is "earlier" than it. */}
            {index === (expandNewest ? 1 : 0) && (
              <h2 className={`${MICRO_LABEL} pt-2`}>{expandNewest ? 'Earlier reports' : 'Other reports'}</h2>
            )}
            <AccordionItem
              value={run.id}
              id={`run-${run.id}`}
              data-testid="watch-run"
              className="rounded-md border border-border bg-card px-4"
            >
              <AccordionTrigger>
                <RunSummary run={run} opening={openIds.includes(run.id) ? undefined : run.review_opening} />
              </AccordionTrigger>
              <AccordionContent className="pb-4 pt-2">
                <RunBody run={run} onAskMate={onAskMate} onDelete={canWrite ? askToDelete : undefined} />
              </AccordionContent>
            </AccordionItem>
          </Fragment>
        ))}
      </Accordion>
      {hasOlder && (
        <div className="flex flex-col items-start gap-1">
          <Button type="button" variant="outline" size="sm" disabled={loadingOlder} onClick={onShowOlder}>
            {loadingOlder ? 'Loading...' : 'Show older'}
          </Button>
          <p className="text-xs text-muted-foreground">Showing the newest {runs.length} runs.</p>
        </div>
      )}
      <p className="text-xs text-muted-foreground">Runs with a finding are kept for good.</p>
      {olderError && <p role="alert" className="text-sm text-destructive">{olderError}</p>}
      <ConfirmDelete
        open={pending !== null}
        onOpenChange={(open) => { if (!open) { setPending(null); setDeleteError(null) } }}
        title={pending ? `Delete the run from ${runTimeLabel(pending)}?` : 'Delete this run?'}
        description={deleteError ?? 'The report, findings and Mate’s review of this run are removed. This can’t be undone.'}
        confirmLabel="Delete run"
        deleting={deleting}
        onConfirm={() => { void confirm() }}
      />
    </div>
  )
}

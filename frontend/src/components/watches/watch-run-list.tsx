import { Fragment, memo, useCallback, useState, type CSSProperties } from 'react'
import { Sparkles, Trash2 } from 'lucide-react'

import { AssistantMarkdown } from '@/components/assistant-markdown'
import { ConfirmDelete } from '@/components/patterns'
import { Accordion, AccordionContent, AccordionItem, AccordionTrigger } from '@/components/ui/accordion'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { WatchChart } from '@/components/watches/watch-chart'
import {
  channelLabel,
  findingsToLookAt,
  groupFindings,
  notCheckedEntries,
  runCostLabel,
  runTimeLabel,
  UNUSUAL_ANSWERS,
  WATCH_SECTIONS,
  type WatchFinding,
  type WatchReportRow,
  type WatchReportSection,
  type WatchRun,
  type WatchSamplesReport,
  type WatchSectionsReport,
  type WatchSeriesReport,
  UnusualSavedError,
  type UnusualAnswer,
  type WatchUnusual,
  type WatchUnusualStretch,
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

const FLAGGED = 'text-amber-700 dark:text-amber-400'

const rowValueClass = (row: WatchReportRow) =>
  `text-right text-sm tabular-nums ${row.flagged ? FLAGGED : 'text-foreground'}${row.not_checked ? ' font-medium' : ''}`

function ReadingRows({ rows }: { rows: WatchReportRow[] }) {
  return (
    <dl className="grid min-w-0 grid-cols-[minmax(0,2fr)_minmax(0,3fr)] gap-x-4 gap-y-0.5">
      {rows.map((row, i) => (
        <Fragment key={i}>
          <dt className="min-w-0 text-sm text-muted-foreground">{row.label}</dt>
          <dd className={rowValueClass(row)}>{row.value}</dd>
          {row.detail && <dd className="col-span-2 min-w-0 text-xs text-muted-foreground">{row.detail}</dd>}
        </Fragment>
      ))}
    </dl>
  )
}

function TankRows({ rows }: { rows: WatchReportRow[] }) {
  const hasChange = rows.some((r) => r.change)
  const head = 'py-0.5 text-xs font-normal text-muted-foreground'
  return (
    <table className="w-full min-w-0 text-sm">
      <thead>
        <tr>
          <th scope="col" className={`${head} pr-3 text-left`}>Tank</th>
          <th scope="col" className={`${head} text-right`}>Now</th>
          {hasChange && <th scope="col" className={`${head} pl-3 text-right`}>Since last report</th>}
        </tr>
      </thead>
      <tbody>
        {rows.map((row, i) => (
          <tr key={i} className={row.flagged ? 'font-semibold' : ''}>
            <th scope="row" className={`min-w-0 py-0.5 pr-3 text-left text-foreground ${row.flagged ? 'font-semibold' : 'font-normal'}`}>{row.label}</th>
            <td className="whitespace-nowrap py-0.5 text-right tabular-nums text-foreground">{row.value}</td>
            {hasChange && (
              <td
                className={`whitespace-nowrap py-0.5 pl-3 text-right tabular-nums ${
                  row.change === 'no change' ? 'text-muted-foreground' : row.flagged ? FLAGGED : 'text-foreground'
                }`}
              >
                {row.change}
              </td>
            )}
          </tr>
        ))}
      </tbody>
    </table>
  )
}

function AlarmRows({ rows }: { rows: WatchReportRow[] }) {
  return (
    <ul className="flex min-w-0 flex-col gap-1.5">
      {rows.map((row, i) => (
        <li key={i} className="flex min-w-0 flex-col">
          <div className="flex min-w-0 items-baseline justify-between gap-3">
            <span className="min-w-0 text-sm font-medium text-foreground">{row.label}</span>
            <span className={rowValueClass(row)}>{row.value}</span>
          </div>
          {row.when && <p className="min-w-0 text-xs text-muted-foreground">{row.when}</p>}
          {row.detail && <p className="min-w-0 text-xs text-muted-foreground">{row.detail}</p>}
        </li>
      ))}
    </ul>
  )
}

function SectionBody({ section }: { section: WatchReportSection }) {
  if (section.unavailable) {
    return <p className="min-w-0 text-sm text-muted-foreground">Unavailable: {section.unavailable}</p>
  }
  // Runs saved before rows existed (and Anchor and Instruments, which have none) carry only lines.
  const rows = section.rows ?? []
  if (rows.length > 0) {
    if (section.name === 'tanks') return <TankRows rows={rows} />
    if (section.name === 'alarms') return <AlarmRows rows={rows} />
    return <ReadingRows rows={rows} />
  }
  return <>{(section.lines ?? []).map((line, i) => <p key={i} className="min-w-0 text-sm text-foreground">{line}</p>)}</>
}

/**
 * One unusual stretch, and the operator's say on it. The answer shows beneath the
 * sentence, quietly, and can be changed. It is the server's answer; only the one
 * being saved is held here. A stretch from an old report has no buttons, and they
 * are left out where the page cannot write.
 */
function UnusualLine({
  text,
  stretch,
  answer,
  onAnswer,
}: {
  text: string
  stretch?: WatchUnusualStretch
  answer: string
  /** Answers this line; left out where it cannot be answered. */
  onAnswer?: (answer: UnusualAnswer) => Promise<void>
}) {
  const [pending, setPending] = useState<UnusualAnswer | null>(null)
  const [error, setError] = useState<string | null>(null)
  // An answer saved while the page could not refresh: shown until the page's own
  // answer changes, which is the refresh arriving.
  const [kept, setKept] = useState<{ base: string; value: UnusualAnswer; note: string } | null>(null)
  const keptNow = kept && kept.base === answer ? kept : null
  const shown = pending ?? keptNow?.value ?? answer
  const said = UNUSUAL_ANSWERS.find((a) => a.answer === shown)?.said
  const choose = async (next: UnusualAnswer | undefined) => {
    // Pressing the chosen button again would clear it; an answer is changed, not cleared.
    if (!stretch || !onAnswer || !next || pending || next === shown) return
    setPending(next)
    setError(null)
    setKept(null)
    try {
      await onAnswer(next)
    } catch (err) {
      if (err instanceof UnusualSavedError) setKept({ base: answer, value: next, note: `Saved. The page could not refresh: ${err.message}` })
      else setError(errorMessage(err))
    } finally {
      setPending(null)
    }
  }
  return (
    <li className="flex min-w-0 flex-col gap-1 text-sm text-foreground">
      <span className="min-w-0">{text}</span>
      {said && (
        <span className="min-w-0 text-xs text-muted-foreground" data-testid="unusual-answer">
          {said}
        </span>
      )}
      {stretch && onAnswer && (
        <ToggleGroup
          variant="outline"
          size="sm"
          aria-label={`Your answer for the ${stretch.subject_name} stretch`}
          value={shown ? [shown] : []}
          disabled={pending !== null}
          onValueChange={(v) => { void choose(v[0] as UnusualAnswer | undefined) }}
          className="flex-wrap"
        >
          {UNUSUAL_ANSWERS.map((a) => (
            <ToggleGroupItem key={a.answer} value={a.answer} className="text-xs" aria-label={`${a.label}: ${stretch.subject_name} stretch`}>
              {a.label}
            </ToggleGroupItem>
          ))}
        </ToggleGroup>
      )}
      {keptNow && <span role="status" className="min-w-0 text-xs text-muted-foreground">{keptNow.note}</span>}
      {error && <span role="alert" className="min-w-0 text-xs text-destructive">{error}</span>}
    </li>
  )
}

/**
 * A learned normal's suggestions (ADR 0276). Quiet on purpose: no accent, no
 * count, and nothing at all when there is no line and no note.
 */
function UnusualBlock({
  run,
  unusual,
  onAnswer,
}: {
  run: WatchRun
  unusual: WatchUnusual
  onAnswer?: (run: WatchRun, index: number, answer: UnusualAnswer) => Promise<void>
}) {
  const answers = run.unusual_answers ?? []
  // Where the answers could not be read, the lines say so in place of the buttons.
  const problem = run.unusual_answers_problem
  const lines = unusual.lines ?? []
  if (lines.length === 0 && !unusual.note) return null
  return (
    <div className="flex min-w-0 flex-col gap-1.5 border-t border-border pt-4" data-testid="watch-run-unusual">
      <p className={MICRO_LABEL}>Unusual</p>
      {lines.length > 0 && (
        <ul className="flex flex-col gap-2" aria-label="Unusual readings">
          {lines.map((line, i) => (
            <UnusualLine key={unusual.stretches?.[i]?.stretch_id ?? i} text={line} stretch={unusual.stretches?.[i]} answer={answers[i] ?? ''} onAnswer={onAnswer && !problem ? (a) => onAnswer(run, i, a) : undefined} />
          ))}
        </ul>
      )}
      {problem && <p className="min-w-0 text-xs text-muted-foreground" data-testid="unusual-answers-problem">{problem}</p>}
      {unusual.note && <p className="min-w-0 text-sm text-muted-foreground">{unusual.note}</p>}
    </div>
  )
}

/** What the report could not check. It is never folded away, so a gap in the report is seen. */
function NotChecked({ entries }: { entries: string[] }) {
  return (
    <div
      className="flex min-w-0 flex-col gap-1.5 rounded-md border border-dashed border-muted-foreground/60 px-4 py-3"
      data-testid="watch-run-not-checked"
    >
      <p className={MICRO_LABEL}>Not checked</p>
      <ul className="flex flex-col gap-1">
        {entries.map((entry, i) => {
          const at = entry.indexOf(': ')
          return (
            <li key={i} className="min-w-0 text-sm text-foreground">
              {at > 0 ? (
                <>
                  <span className="font-medium">{entry.slice(0, at)}</span>
                  {entry.slice(at)}
                </>
              ) : (
                entry
              )}
            </li>
          )
        })}
      </ul>
    </div>
  )
}

/** One finding's line: the large line for a finding that stands alone, the small one for Mate's and for those that add to another. */
function FindingLine({ finding, nested }: { finding: WatchFinding; nested: boolean }) {
  if (finding.by === 'mate') {
    return (
      <div className="flex min-w-0 flex-wrap items-baseline gap-x-2 text-sm">
        <Badge variant="secondary">Mate</Badge>
        <span className="min-w-0">{finding.text}</span>
      </div>
    )
  }
  return <span className={`min-w-0 ${nested ? 'text-sm' : 'text-base font-medium'}`}>{finding.text}</span>
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
            <p className="text-sm font-semibold text-foreground">
              {section.title || sectionLabel(section.name)}
              {section.summary && !section.unavailable && <span className="font-normal text-muted-foreground"> {section.summary}</span>}
            </p>
            <SectionBody section={section} />
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
  /** Opens Mate with an unsent draft about one of the run's questions. Without it the questions show with no button. */
  onAnswer?: (run: WatchRun, question: string) => void
  /** Says what the operator thinks of an unusual stretch. Without it the stretches show with no buttons. */
  onAnswerUnusual?: (run: WatchRun, index: number, answer: UnusualAnswer) => Promise<void>
}

/**
 * What a run shows when it is closed: when, how many findings, anything that
 * went wrong, and (only while it is closed) the opening line of Mate's review.
 */
function RunSummary({ run, opening }: { run: WatchRun; opening?: string }) {
  const count = findingsToLookAt(run.findings)
  const undelivered = (run.notify_pending ?? []).length > 0
  return (
    <span className="flex min-w-0 flex-1 flex-col gap-0.5">
      <span className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1">
        <span className="text-sm font-medium tabular-nums text-foreground">{runTimeLabel(run)}</span>
        {count > 0 && (
          <Badge variant="outline" className="border-amber-500/50 text-amber-700 dark:text-amber-400">
            {count} to look at
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
  onAnswer?: (run: WatchRun, question: string) => void
  /** Says what the operator thinks of an unusual stretch. Left out where the page cannot write. */
  onAnswerUnusual?: (run: WatchRun, index: number, answer: UnusualAnswer) => Promise<void>
  /** Left out where the run can't be deleted. */
  onDelete?: (run: WatchRun) => void
}

/**
 * Everything else about a run: findings, charts, report, Mate's review, who was
 * told, and the actions. Memoised so opening one run doesn't redraw the charts
 * of the others.
 */
const RunBody = memo(function RunBody({ run, onAskMate, onAnswer, onAnswerUnusual, onDelete }: RunBodyProps) {
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
  const questions = run.questions ?? []
  const groups = groupFindings(run.findings)
  const notChecked = notCheckedEntries(sections)
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

      {groups.length > 0 && (
        <div className="flex min-w-0 flex-col gap-2 border-t border-border pt-4">
          <p className={MICRO_LABEL}>Needs a look</p>
          <ul className="flex flex-col" aria-label="Findings">
            {groups.map((g, gi) => {
              const members = [{ index: g.index, finding: g.finding }, ...g.adds]
              return (
                <li
                  key={g.index}
                  data-testid="watch-finding-group"
                  className={`flex min-w-0 flex-col gap-1.5 text-foreground ${gi > 0 ? 'mt-3 border-t border-border pt-3' : ''}`}
                >
                  <FindingLine finding={g.finding} nested={false} />
                  {g.adds.length > 0 && (
                    <div className="flex min-w-0 flex-col gap-1.5 pl-4">
                      {g.adds.map((a) => <FindingLine key={a.index} finding={a.finding} nested />)}
                    </div>
                  )}
                  {members.flatMap((m) =>
                    (run.charts ?? [])
                      .filter((c) => c.finding_index === m.index)
                      .map((c, ci) => <WatchChart key={`${m.index}-${ci}`} chart={c} />),
                  )}
                  {members.map((m) =>
                    m.finding.chart_problem ? (
                      <p key={m.index} className="min-w-0 text-xs text-muted-foreground">{m.finding.chart_problem}</p>
                    ) : null,
                  )}
                </li>
              )
            })}
          </ul>
        </div>
      )}

      {sections?.unusual && <UnusualBlock run={run} unusual={sections.unusual} onAnswer={onAnswerUnusual} />}

      {notChecked.length > 0 && <NotChecked entries={notChecked} />}

      {(reviewText || questions.length > 0) && (
        <div className="flex min-w-0 flex-col gap-1 border-t border-border pt-4" data-testid="watch-run-review">
          <p className={MICRO_LABEL}>Mate&apos;s review</p>
          {reviewText && <AssistantMarkdown content={reviewText} />}
          {questions.length > 0 && (
            <ul className="mt-2 flex flex-col gap-2" aria-label="Questions from Mate">
              {questions.map((q, i) => (
                <li
                  key={i}
                  data-testid="watch-run-question"
                  className="flex min-w-0 flex-col gap-2 rounded-md bg-muted px-3 py-2 sm:flex-row sm:items-center sm:justify-between sm:gap-3"
                >
                  <p className="min-w-0 text-sm font-medium text-foreground">{q}</p>
                  {onAnswer && (
                    <Button
                      type="button"
                      variant="outline"
                      size="sm"
                      className="min-h-11 w-full sm:min-h-10 sm:w-auto sm:shrink-0"
                      aria-label={`Answer: ${q}`}
                      onClick={() => onAnswer(run, q)}
                    >
                      Answer
                    </Button>
                  )}
                </li>
              ))}
            </ul>
          )}
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
export function WatchRunCard({ run, highlighted = false, onAskMate, onAnswer, onAnswerUnusual }: WatchRunCardProps) {
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
      <RunBody run={run} onAskMate={onAskMate} onAnswer={onAnswer} onAnswerUnusual={onAnswerUnusual} />
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
  onAnswer?: (run: WatchRun, question: string) => void
  onAnswerUnusual?: (run: WatchRun, index: number, answer: UnusualAnswer) => Promise<void>
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
  onAnswer,
  onAnswerUnusual,
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
                <RunBody run={run} onAskMate={onAskMate} onAnswer={onAnswer} onAnswerUnusual={onAnswerUnusual} onDelete={canWrite ? askToDelete : undefined} />
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

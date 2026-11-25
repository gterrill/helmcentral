import { ChevronRight, Loader2, Search, TriangleAlert } from 'lucide-react'
import { memo, useEffect, useId, useLayoutEffect, useMemo, useRef, useState, type ReactNode } from 'react'

import { useApiQuery } from '@/lib/api-query'
import {
  burstSummary,
  formatActivitySummary,
  formatCostCents,
  groupSteps,
  isThinking,
  lookupLabel,
  ReasoningSegmenter,
  timelineFromTrace,
  timelineSummary,
  visibleSteps,
  type BurstSummary,
  type LookupStep,
  type MateRoundFoot,
  type MateStep,
  type MateTimeline,
} from '@/lib/mate-activity'
import { formatRunDuration, formatRunOffset, mapRunTrace, type AssistantRunTrace, type RunApi } from '@/lib/mate-run'
import { cn } from '@/lib/utils'

/** The stored run behind an answer or a failed question: enough for the folded line. */
interface ActivityRun {
  id: string
  thoughtMs: number | null
  lookups: number
}

type LiveActivity = { timeline: MateTimeline; startedLocal: number | null }

interface MateActivityProps {
  /** The run as it streamed, kept after it was delivered; nothing is read from the server for it. */
  live?: LiveActivity
  /** The stored run, whose trace is read the first time the view is opened. */
  run?: ActivityRun
  /** What the answer cost, for the folded line before the trace is read. */
  costUsd?: number
  /** Open from the start and read the trace at once: a question that got no answer. */
  startOpen?: boolean
  /** What stopped a question that got no answer, in words; shown above the view. */
  headline?: { text: string; tone: 'error' | 'quiet' }
}

const SECOND_MS = 1000
const PRIVATE_NOTE = 'This model keeps its reasoning private.'
/** Within this many pixels of the end counts as following the latest step. */
const FOLLOW_SLACK_PX = 8

/** Re-renders once a second while mounted, so a seconds count moves without a live region. */
function useNow(active: boolean): number {
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    if (!active) return
    setNow(Date.now())
    const timer = setInterval(() => setNow(Date.now()), SECOND_MS)
    return () => clearInterval(timer)
  }, [active])
  return now
}

function ThinkingLine({ sinceMs, startedLocal }: { sinceMs: number; startedLocal: number | null }) {
  const now = useNow(true)
  const seconds = startedLocal === null ? null : Math.max(0, Math.floor((now - startedLocal - sinceMs) / SECOND_MS))
  return <p className="text-muted-foreground">{seconds === null ? 'Thinking…' : `Thinking… ${seconds} s`}</p>
}

function lookupSuffix(step: LookupStep): string | null {
  switch (step.outcome) {
    case 'failed':
      return 'failed'
    case 'cut':
      return 'cut off at the time limit'
    case 'stopped':
      return 'stopped'
    case 'withheld':
      return 'skipped, held back after repeated failures'
    case 'round_full':
      return 'skipped, too many lookups at once'
    default:
      return null
  }
}

/** "2 failed, 1 cut off": how the lookups that did not finish well ended, in words. */
function burstTrouble(summary: BurstSummary): string {
  const parts: string[] = []
  if (summary.failed > 0) parts.push(`${summary.failed} failed`)
  if (summary.cut > 0) parts.push(`${summary.cut} cut off`)
  if (summary.stopped > 0) parts.push(`${summary.stopped} stopped`)
  if (summary.skipped > 0) parts.push(`${summary.skipped} skipped`)
  return parts.join(', ')
}

/** Lookups that came one after another, as one wrapped line of subjects; a tap opens a row for each. */
function LookupBurst({ lookups }: { lookups: LookupStep[] }) {
  const [expanded, setExpanded] = useState(false)
  const listId = useId()
  const summary = burstSummary(lookups)
  const trouble = burstTrouble(summary)
  return (
    <div className="min-w-0">
      <button
        type="button"
        aria-expanded={expanded}
        aria-controls={expanded ? listId : undefined}
        onClick={() => setExpanded((value) => !value)}
        className="flex w-full min-w-0 items-baseline gap-1.5 break-words rounded-sm text-left text-xs text-muted-foreground hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
      >
        <Search className="h-3 w-3 shrink-0 self-center" aria-hidden="true" />
        <span className="min-w-0 flex-1">
          {summary.subjects}
          {trouble !== '' && (
            <span className="whitespace-nowrap">
              {' — '}
              <TriangleAlert className="inline h-3 w-3 align-[-0.125em]" aria-hidden="true" />
              {` ${trouble}`}
            </span>
          )}
        </span>
      </button>
      {expanded && (
        <ul id={listId} className="mt-1 flex min-w-0 flex-col gap-1 pl-4">
          {lookups.map((lookup) => (
            <li key={lookup.id} className="min-w-0">
              <LookupRow step={lookup} />
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}

/** One lookup on one truncated line; a tap shows all of it, since a phone has no hover. */
function LookupRow({ step }: { step: LookupStep }) {
  const [expanded, setExpanded] = useState(false)
  const suffix = lookupSuffix(step)
  return (
    <button
      type="button"
      aria-expanded={expanded}
      onClick={() => setExpanded((value) => !value)}
      className="flex w-full min-w-0 items-baseline gap-1.5 rounded-sm text-left focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
    >
      {step.outcome === 'running' && (
        <Loader2 className="h-3 w-3 shrink-0 animate-spin self-center motion-reduce:animate-none" aria-hidden="true" />
      )}
      <span className={cn('min-w-0 flex-1', expanded ? 'whitespace-normal break-words' : 'truncate')}>
        {lookupLabel(step)}
        {suffix !== null && <span className="text-muted-foreground">{` · ${suffix}`}</span>}
        {expanded && step.error !== '' && <span className="block break-words text-muted-foreground">{step.error}</span>}
        {expanded && step.detail !== '' && <span className="block break-words text-muted-foreground">{step.detail}</span>}
        {expanded && step.meta !== '' && <span className="block tabular-nums text-muted-foreground">{step.meta}</span>}
      </span>
    </button>
  )
}

/** One plain line of the list. */
function Line({ className, children }: { className?: string; children: ReactNode }) {
  return (
    <li className="min-w-0">
      <p className={className}>{children}</p>
    </li>
  )
}

function ReasoningItem({ step }: { step: Extract<MateStep, { kind: 'reasoning' }> }) {
  const segmenter = useRef<ReasoningSegmenter | null>(null)
  const segments = useMemo(() => {
    segmenter.current ??= new ReasoningSegmenter()
    return segmenter.current.segments(step.text)
  }, [step.text])
  return (
    <li className="min-w-0 [&:not(:first-child)]:mt-2">
      {segments.map((segment, i) =>
        segment.kind === 'heading' ? (
          <p key={i} className="break-words text-sm font-medium text-foreground [&:not(:first-child)]:mt-2">
            {segment.text}
          </p>
        ) : (
          <p key={i} className="whitespace-pre-wrap break-words text-sm leading-relaxed text-muted-foreground">
            {segment.text}
          </p>
        ),
      )}
      {step.cut && <p className="text-muted-foreground">The rest of this reasoning was cut.</p>}
    </li>
  )
}

/** Any step but a lookup, which groupSteps gathers into bursts. Memoised: a step that did not change is not drawn again. */
const StepItem = memo(function StepItem({ step }: { step: Exclude<MateStep, { kind: 'lookup' }> }) {
  switch (step.kind) {
    case 'reasoning':
      return <ReasoningItem step={step} />
    case 'private':
      return <Line className="text-muted-foreground">{PRIVATE_NOTE}</Line>
    case 'chatter':
      return <Line className="whitespace-pre-wrap break-words">{step.text}</Line>
    case 'sent_back':
      return (
        <li className="min-w-0">
          <p className="font-medium">{step.label}</p>
          {step.draft !== '' && (
            <p className="mt-0.5 whitespace-pre-wrap break-words border-l-2 border-border pl-2 text-muted-foreground">{step.draft}</p>
          )}
        </li>
      )
    case 'note':
      return <Line className="text-muted-foreground">{step.text}</Line>
    case 'error':
      return <Line className="break-words text-destructive">{step.text}</Line>
    case 'stopped':
      return <Line className="text-muted-foreground">Stopped</Line>
  }
})

function roundLine(round: MateRoundFoot): string {
  const parts = [`Round ${round.round + 1}`]
  if (round.atMs !== null) parts.push(formatRunOffset(round.atMs))
  if (round.latencyMs !== null) parts.push(`${formatRunDuration(round.latencyMs)} for the reply`)
  parts.push(
    `${round.promptTokens.toLocaleString()} in, ${round.completionTokens.toLocaleString()} out${
      round.cachedTokens > 0 ? `, ${round.cachedTokens.toLocaleString()} cached` : ''
    }`,
  )
  if (round.costUsd > 0) parts.push(formatCostCents(round.costUsd))
  if (round.state !== '') parts.push(round.state)
  return parts.join(' · ')
}

/** What each round cost, and how the run ended. */
function Foot({ timeline, headline }: { timeline: MateTimeline; headline?: string }) {
  const noReasoning = timeline.rounds.length > 0 && timeline.rounds.every((r) => r.reasoning === 'none')
  // The outcome is said once: not again when the headline above, or the error step, already says it.
  const errorStep = timeline.steps.find((s) => s.kind === 'error')
  const outcomeSaid = timeline.outcome === headline || (errorStep?.kind === 'error' && errorStep.text === timeline.outcome)
  const outcome = outcomeSaid ? '' : timeline.outcome
  if (timeline.rounds.length === 0 && outcome === '') return null
  return (
    <div className="mt-2 flex min-w-0 flex-col gap-0.5 border-t border-border pt-2 text-muted-foreground">
      {outcome !== '' && <p>{outcome}</p>}
      {noReasoning && <p>{"Mate didn't show any reasoning."}</p>}
      {timeline.rounds.map((round) => (
        <p key={round.round} className={cn('break-words tabular-nums', round.error !== '' && 'text-destructive')}>
          {roundLine(round)}
          {round.error !== '' && ` · ${round.error}`}
        </p>
      ))}
      {timeline.budget !== '' && <p>{timeline.budget}</p>}
    </div>
  )
}

function headerLabel(timeline: MateTimeline | null, fallback: string, answerCostUsd: number | null): string {
  if (timeline === null) return fallback
  switch (timeline.phase) {
    case 'working': {
      const summary = timelineSummary(timeline, answerCostUsd)
      return summary === '' ? 'Working…' : `Working · ${summary}`
    }
    case 'failed':
      return 'Stopped before answering'
    case 'stopped':
      return 'Stopped'
    default:
      return timelineSummary(timeline, answerCostUsd) || fallback
  }
}

const NO_SUMMARY = 'How Mate worked this out'

/**
 * The folded line. A saved answer keeps the line it showed before its trace was
 * read; a live run, and one that failed or was stopped, say what the timeline does.
 */
function activityLabel(timeline: MateTimeline | null, isLive: boolean, run: ActivityRun | undefined, costUsd: number | null): string {
  const fallback = run !== undefined ? formatActivitySummary({ thoughtMs: run.thoughtMs, lookups: run.lookups, costUsd }) : ''
  if (timeline === null) return fallback || NO_SUMMARY
  if (isLive || timeline.phase === 'failed' || timeline.phase === 'stopped') return headerLabel(timeline, fallback || NO_SUMMARY, costUsd)
  return fallback || headerLabel(timeline, NO_SUMMARY, costUsd)
}

/** The timeline to show: the live one, or the saved run's trace once it has been asked for and read. */
function useActivityTimeline(live: LiveActivity | undefined, run: ActivityRun | undefined, requested: boolean) {
  const trace = useApiQuery<AssistantRunTrace>({
    queryKey: ['/api/assistant/runs', run?.id ?? ''],
    path: `/api/assistant/runs/${encodeURIComponent(run?.id ?? '')}`,
    enabled: live === undefined && run !== undefined && requested,
    select: (raw: RunApi) => mapRunTrace(raw),
  })
  const saved = trace.data
  const timeline = useMemo(() => live?.timeline ?? (saved !== undefined ? timelineFromTrace(saved) : null), [live, saved])
  return { timeline, loadError: trace.error }
}

/** What the open box holds: the steps, the thinking line while Mate is at it, and the foot once it is done. */
function ActivityBody({
  timeline,
  loadError,
  startedLocal,
  headline,
}: {
  timeline: MateTimeline | null
  loadError: Error | null
  startedLocal: number | null
  headline?: string
}) {
  const items = useMemo(() => groupSteps(visibleSteps(timeline?.steps ?? [])), [timeline?.steps])
  if (timeline === null) {
    if (loadError === null) return <p className="text-muted-foreground">Loading…</p>
    return (
      <p role="alert" className="text-destructive">
        {loadError.message}
      </p>
    )
  }
  return (
    <>
      {items.length > 0 && (
        <ol className="flex min-w-0 flex-col gap-1.5">
          {items.map((item) =>
            item.kind === 'burst' ? (
              <li key={item.id} className="min-w-0">
                <LookupBurst lookups={item.lookups} />
              </li>
            ) : (
              <StepItem key={item.id} step={item.step} />
            ),
          )}
        </ol>
      )}
      {isThinking(timeline) && <ThinkingLine sinceMs={timeline.thinkingSinceMs} startedLocal={startedLocal} />}
      {timeline.phase !== 'working' && <Foot timeline={timeline} headline={headline} />}
    </>
  )
}

/**
 * What Mate did for an answer, as steps (ADR 0305). While it works the view is
 * open, a box that follows the latest step and grows to about a third of the
 * screen; when the
 * answer begins it folds to one line, "Thought 12 s · 4 lookups · 3¢", which
 * opens to every step with what each round cost. A run that failed or was
 * stopped stays open with that as its last step. Reasoning is plain text, and
 * nothing that streams is a live region: a hidden status announces each step
 * once instead.
 */
export function MateActivity({ live, run, costUsd, startOpen = false, headline }: MateActivityProps) {
  const boxId = useId()
  // What the operator chose this run: null until they touch the fold.
  const [chosen, setChosen] = useState<boolean | null>(null)
  const [requested, setRequested] = useState(startOpen)

  const { timeline, loadError } = useActivityTimeline(live, run, requested)
  const phase = timeline?.phase ?? null

  // A run that fails or is stopped opens, whatever the operator chose before:
  // what went wrong is the first thing to read. Reset on that transition.
  const [seenPhase, setSeenPhase] = useState(phase)
  if (seenPhase !== phase) {
    setSeenPhase(phase)
    if (phase === 'failed' || phase === 'stopped') setChosen(null)
  }

  // The headline of a run that got no answer stands above the view until the
  // view's own error step says the same sentence; it is shown once, there.
  const errorStep = timeline?.steps.find((s) => s.kind === 'error')
  const headlineText = headline !== undefined && !(errorStep?.kind === 'error' && errorStep.text === headline.text) ? headline.text : null

  const following = phase === 'working' || phase === 'answering'
  const autoOpen = startOpen || phase === 'working' || phase === 'failed' || phase === 'stopped'
  const open = chosen ?? autoOpen

  // The latest step stays in view until the operator scrolls up, and again once they scroll back to the end.
  const boxRef = useRef<HTMLDivElement>(null)
  const stuckRef = useRef(true)
  useLayoutEffect(() => {
    const box = boxRef.current
    if (box !== null && following && stuckRef.current) box.scrollTop = box.scrollHeight
  }, [timeline, open, following])

  const label = activityLabel(timeline, live !== undefined, run, costUsd ?? null)

  function toggle() {
    setChosen(!open)
    if (!open) setRequested(true)
  }

  return (
    <div className="min-w-0 text-xs">
      {headlineText !== null && (
        <div role="status" className="mb-1 min-w-0 rounded-md border border-border bg-muted/50 px-3 py-2">
          <p className={headline?.tone === 'error' ? 'text-destructive' : 'text-muted-foreground'}>{headlineText}</p>
        </div>
      )}
      {/* Mounted for as long as the view is live, whatever phase it is in, so the
          failure and the stop are announced and not removed in the render that
          brings them. */}
      {live !== undefined && timeline !== null && (
        <div role="status" className="sr-only">
          {timeline.phase === 'done' ? '' : timeline.announcement}
        </div>
      )}
      <button
        type="button"
        aria-expanded={open}
        aria-controls={open ? boxId : undefined}
        onClick={toggle}
        className="flex min-w-0 max-w-full items-center gap-1 text-muted-foreground hover:text-foreground"
      >
        <ChevronRight
          className={cn('h-3 w-3 shrink-0 transition-transform motion-reduce:transition-none', open && 'rotate-90')}
          aria-hidden="true"
        />
        <span className="min-w-0 truncate">{label}</span>
      </button>
      {open && (
        <div
          id={boxId}
          ref={boxRef}
          role="group"
          tabIndex={0}
          aria-label="What Mate is doing"
          onScroll={(event) => {
            const box = event.currentTarget
            stuckRef.current = box.scrollHeight - box.scrollTop - box.clientHeight <= FOLLOW_SLACK_PX
          }}
          className={cn(
            'mt-1 min-w-0 overflow-y-auto rounded-md border border-border bg-muted/30 p-2 animate-in fade-in-0 motion-reduce:animate-none',
            phase === 'working' ? 'max-h-[min(12lh,40vh)]' : 'max-h-[60vh]',
          )}
        >
          <ActivityBody timeline={timeline} loadError={loadError} startedLocal={live?.startedLocal ?? null} headline={headline?.text} />
        </div>
      )}
    </div>
  )
}

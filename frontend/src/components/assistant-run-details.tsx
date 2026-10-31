import { ChevronRight } from 'lucide-react'
import { useState } from 'react'

import { useApiQuery } from '@/lib/api-query'
import { formatBytes } from '@/lib/document-display'
import {
  formatRunDuration,
  formatRunOffset,
  mapRunTrace,
  rounds,
  runBudgetText,
  runOutcomeText,
  type AssistantRunTrace,
  type RunApi,
  type RunCall,
  type RunRound,
} from '@/lib/mate-run'
import { cn } from '@/lib/utils'

function roundState(round: RunRound): string {
  switch (round.kind) {
    case 'failed':
      return 'failed'
    case 'cut':
      return 'cut off by time'
    case 'correction':
      return 'sent back for correction'
    case 'final':
      return 'final answer'
    case 'answer':
      return 'answer'
    case 'lookups':
      return round.progress ? 'progress' : 'nothing new'
  }
}

function callSource(call: RunCall): string | null {
  if (call.cache === 'hit') return 'from memory'
  if (call.cache === 'miss') return 'looked up'
  return null
}

function CallRow({ call }: { call: RunCall }) {
  const source = callSource(call)
  return (
    <li className="flex min-w-0 items-baseline gap-2">
      <span className="shrink-0 font-medium">{call.name}</span>
      <span className="min-w-0 flex-1 truncate text-muted-foreground" title={call.args}>
        {call.args}
      </span>
      <span className="shrink-0 tabular-nums text-muted-foreground">
        {formatRunDuration(call.durationMs)} · {formatBytes(call.resultBytes)}
        {source ? ` · ${source}` : ''}
      </span>
      {call.error !== '' && <span className="min-w-0 shrink truncate text-destructive" title={call.error}>{call.error}</span>}
    </li>
  )
}

function RoundBlock({ round }: { round: RunRound }) {
  return (
    <li className="min-w-0 border-t border-border pt-1.5 first:border-t-0 first:pt-0" data-testid="run-round">
      <div className="flex min-w-0 flex-wrap items-baseline gap-x-3 gap-y-0.5 tabular-nums">
        <span className="text-[10px] font-medium uppercase tracking-wider text-muted-foreground">Round {round.index + 1}</span>
        <span>{formatRunOffset(round.startOffsetMs)}</span>
        <span>{formatRunDuration(round.latencyMs)} for the reply</span>
        <span className="text-muted-foreground">
          {round.promptTokens.toLocaleString()} in, {round.completionTokens.toLocaleString()} out
          {round.cachedTokens > 0 ? `, ${round.cachedTokens.toLocaleString()} cached` : ''}
        </span>
        <span className="text-muted-foreground">${round.costUsd.toFixed(3)}</span>
        <span className={cn(round.error !== '' ? 'text-destructive' : 'text-muted-foreground')}>{roundState(round)}</span>
      </div>
      {round.error !== '' && <p className="text-destructive">{round.error}</p>}
      {round.calls.length > 0 && (
        <ul className="mt-1 flex min-w-0 flex-col gap-0.5 pl-3">
          {round.calls.map((call) => (
            <CallRow key={`${round.index}-${call.seq}`} call={call} />
          ))}
        </ul>
      )}
    </li>
  )
}

function TraceBody({ trace }: { trace: AssistantRunTrace }) {
  return (
    <div className="flex min-w-0 flex-col gap-2">
      <div className="flex min-w-0 flex-col gap-0.5">
        <p>
          {runOutcomeText(trace)}{' '}
          {trace.answerMessageId === null ? 'It ran for ' : `${rounds(trace.roundCount)} of lookups in `}
          <span className="tabular-nums">{formatRunDuration(trace.durationMs)}</span>.
        </p>
        {trace.error !== '' && <p className="break-words text-destructive">{trace.error}</p>}
        <p className="text-muted-foreground">{runBudgetText(trace.budget)}</p>
      </div>
      {trace.rounds.length > 0 && (
        <ol className="flex min-w-0 flex-col gap-1.5">
          {trace.rounds.map((round) => (
            <RoundBlock key={round.index} round={round} />
          ))}
        </ol>
      )}
    </div>
  )
}

/**
 * What a run did, on request: its rounds, how long each took, which lookups it
 * made and how it ended (ADR 0240). Read from the server when opened, so a
 * thread full of answers loads none of it.
 */
export function AssistantRunDetails({ runId }: { runId: string }) {
  const [open, setOpen] = useState(false)
  // Read on the first open and kept, so closing and opening again reads nothing.
  const [requested, setRequested] = useState(false)
  const trace = useApiQuery<AssistantRunTrace>({
    queryKey: ['/api/assistant/runs', runId],
    path: `/api/assistant/runs/${encodeURIComponent(runId)}`,
    enabled: requested,
    select: (raw: RunApi) => mapRunTrace(raw),
  })

  return (
    <div className="min-w-0 text-xs">
      <button
        type="button"
        aria-expanded={open}
        onClick={() => {
          setOpen((value) => !value)
          setRequested(true)
        }}
        className="flex items-center gap-1 text-muted-foreground hover:text-foreground"
      >
        <ChevronRight className={cn('h-3 w-3 transition-transform', open && 'rotate-90')} aria-hidden="true" />
        Run details
      </button>
      {open && (
        <section aria-label="Run details" className="mt-1 min-w-0 rounded-md border border-border bg-muted/30 p-2">
          {trace.data !== undefined ? (
            <TraceBody trace={trace.data} />
          ) : trace.error !== null ? (
            <p role="alert" className="text-destructive">
              {trace.error.message}
            </p>
          ) : (
            <p className="text-muted-foreground">Loading…</p>
          )}
        </section>
      )}
    </div>
  )
}

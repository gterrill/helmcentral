// What Mate did while it worked, as steps (ADR 0305). One timeline model is
// built two ways: applyMateEvent folds the live stream into it as events
// arrive, and timelineFromTrace reads a saved run into it. The activity view
// shows either. Live order is the order things arrived; a saved run puts each
// round's reasoning first, then its chatter, its lookups and the draft it sent
// back, because the trace does not keep the order within a round.

import { formatBytes } from '@/lib/document-display'
import type { MateEvent, MateReasoningKind } from '@/lib/mate-events'
import {
  formatRunDuration,
  runBudgetText,
  runOutcomeText,
  type AssistantRunTrace,
  type RunRound,
} from '@/lib/mate-run'

/** working: Mate is at it. answering: the answer has begun and the view folds. */
export type ActivityPhase = 'working' | 'answering' | 'done' | 'failed' | 'stopped'

export type LookupOutcome = 'running' | 'done' | 'failed' | 'cut' | 'stopped' | 'withheld' | 'round_full'

export type MateStep =
  | { kind: 'reasoning'; id: string; round: number; atMs: number | null; text: string; cut: boolean }
  /** A round the model thought through and shows nothing of. */
  | { kind: 'private'; id: string; round: number; atMs: number | null }
  | { kind: 'chatter'; id: string; round: number; atMs: number | null; text: string }
  | {
      kind: 'lookup'
      id: string
      round: number
      seq: number
      atMs: number | null
      /** What Mate was doing, in the operator's words. */
      text: string
      /** What it looked at, with no verb; empty for a tool the server does not know. */
      subject: string
      /** The arguments, on a saved run. */
      detail: string
      /** How long it took and how much came back, on a saved run. */
      meta: string
      outcome: LookupOutcome
      /** Why a failed lookup failed. */
      error: string
    }
  | { kind: 'sent_back'; id: string; round: number; atMs: number | null; triggers: string[]; label: string; draft: string }
  | { kind: 'note'; id: string; text: string }
  | { kind: 'error'; id: string; text: string }
  | { kind: 'stopped'; id: string }

/** What one round cost and how it went, for the foot of the expanded view. */
export interface MateRoundFoot {
  round: number
  atMs: number | null
  /** How long the model took to reply; a saved run only. */
  latencyMs: number | null
  promptTokens: number
  completionTokens: number
  cachedTokens: number
  costUsd: number
  reasoning: MateReasoningKind
  /** "progress", "final answer" and the like; a saved run only. */
  state: string
  error: string
}

export interface MateTimeline {
  phase: ActivityPhase
  steps: MateStep[]
  rounds: MateRoundFoot[]
  /** When the open round began, or the last lookup ended with none running: where "Thinking… N s" counts from. */
  thinkingSinceMs: number
  /** Milliseconds from the run's start to the answer's first words; null until there is an answer. */
  thoughtMs: number | null
  /** The answer text shown so far, which an answer_withdrawn moves into chatter. */
  answer: string
  /** What a screen reader hears when this changes; the live view's hidden status. */
  announcement: string
  runId: string | null
  /** Lookups that ran, when the server counted them; null to count the steps. */
  lookups: number | null
  /** How the run ended and what it was allowed, for a saved run's foot. */
  outcome: string
  budget: string
  /** When each round started, so a step and the round's foot can say when before the round ends. */
  roundAt: Record<number, number>
}

export function emptyTimeline(): MateTimeline {
  return {
    phase: 'working',
    steps: [],
    rounds: [],
    thinkingSinceMs: 0,
    thoughtMs: null,
    answer: '',
    announcement: '',
    runId: null,
    lookups: null,
    outcome: '',
    budget: '',
    roundAt: {},
  }
}

const TRIGGER_LABELS: Record<string, string> = {
  'relative wind': 'wind angle wrong for the course',
  'passage limits claim': 'claim about your passage limits',
  'anchorage shelter': 'shelter at the anchorage',
  'anchorage depth': 'depth at the anchorage',
}

/** What a check found, in the operator's words. A key this does not know is shown as it is. */
function triggerLabel(key: string): string {
  return TRIGGER_LABELS[key] ?? key
}

/** "Draft sent back: shelter at the anchorage". */
export function sentBackLabel(triggers: string[]): string {
  if (triggers.length === 0) return 'Draft sent back'
  return `Draft sent back: ${triggers.map(triggerLabel).join(' and ')}`
}

const FORCED_FINAL_NOTE = 'Lookups stopped, so Mate was asked to answer.'
const EMPTY_ERROR = 'Mate stopped before answering.'

function nextId(t: MateTimeline, kind: string): string {
  return `${kind}-${t.steps.length}`
}

const encoder = new TextEncoder()

/** Whether a lookup is still running. */
export function runningLookups(steps: MateStep[]): boolean {
  return steps.some((s) => s.kind === 'lookup' && s.outcome === 'running')
}

/** Text joins the step before it when that is the same round's step of the same kind. */
function appendText(t: MateTimeline, kind: 'reasoning' | 'chatter', round: number, text: string): MateStep[] {
  const last = t.steps[t.steps.length - 1]
  if (last !== undefined && last.kind === kind && last.round === round) {
    return [...t.steps.slice(0, -1), { ...last, text: last.text + text }]
  }
  const atMs = t.roundAt[round] ?? null
  const step: MateStep =
    kind === 'reasoning'
      ? { kind, id: nextId(t, kind), round, atMs, text, cut: false }
      : { kind, id: nextId(t, kind), round, atMs, text }
  return [...t.steps, step]
}

/** One round's foot; what only a saved run knows defaults to nothing. */
function makeFoot(foot: Omit<MateRoundFoot, 'latencyMs' | 'state' | 'error'> & Partial<Pick<MateRoundFoot, 'latencyMs' | 'state' | 'error'>>): MateRoundFoot {
  return { latencyMs: null, state: '', error: '', ...foot }
}

/** Back to working: what was shown as the answer was not it, so thinking counts from atMs again. */
function withdraw(t: MateTimeline, atMs: number, steps: MateStep[]): MateTimeline {
  return { ...t, phase: 'working', thoughtMs: null, answer: '', thinkingSinceMs: atMs, steps }
}

export type ActivityEvent = MateEvent | { type: 'stopped' }

/** Folds one stream event into the timeline. The input is left alone. */
export function applyMateEvent(t: MateTimeline, event: ActivityEvent): MateTimeline {
  switch (event.type) {
    case 'clock':
      return t
    case 'round': {
      let steps = t.steps
      if (event.final && steps.some((s) => s.kind === 'lookup')) {
        steps = [...steps, { kind: 'note', id: nextId(t, 'note'), text: FORCED_FINAL_NOTE }]
      }
      return { ...t, steps, thinkingSinceMs: event.at_ms, roundAt: { ...t.roundAt, [event.round]: event.at_ms } }
    }
    case 'reasoning':
      return { ...t, steps: appendText(t, 'reasoning', event.round, event.text) }
    case 'chatter':
      return { ...t, steps: appendText(t, 'chatter', event.round, event.text) }
    case 'lookup': {
      const step: MateStep = {
        kind: 'lookup',
        id: nextId(t, 'lookup'),
        round: event.round,
        seq: event.seq,
        atMs: event.at_ms,
        text: event.text,
        subject: event.subject,
        detail: '',
        meta: '',
        outcome: event.skipped === '' ? 'running' : event.skipped,
        error: '',
      }
      return { ...t, steps: [...t.steps, step], announcement: event.skipped === '' ? event.text : t.announcement }
    }
    case 'lookup_done': {
      const index = t.steps.findIndex((s) => s.kind === 'lookup' && s.round === event.round && s.seq === event.seq)
      if (index === -1) return t
      const found = t.steps[index] as LookupStep
      const steps = t.steps.slice()
      steps[index] = { ...found, outcome: event.outcome, error: event.reason }
      return { ...t, steps, thinkingSinceMs: runningLookups(steps) ? t.thinkingSinceMs : event.at_ms }
    }
    case 'answer_start':
      return { ...t, phase: 'answering', thoughtMs: event.at_ms, answer: '', announcement: 'Answer started' }
    case 'delta':
      return { ...t, answer: t.answer + event.text }
    case 'answer_withdrawn':
      return withdraw(t, event.at_ms, t.answer === '' ? t.steps : appendText(t, 'chatter', event.round, t.answer))
    case 'draft_sent_back': {
      const label = sentBackLabel(event.triggers)
      const step: MateStep = {
        kind: 'sent_back',
        id: nextId(t, 'sent_back'),
        round: event.round,
        atMs: event.at_ms,
        triggers: event.triggers,
        label,
        draft: event.draft,
      }
      return { ...withdraw(t, event.at_ms, [...t.steps, step]), announcement: label }
    }
    case 'round_end': {
      let steps = t.steps
      const atMs = t.roundAt[event.round] ?? null
      const shown = steps.filter((s) => s.kind === 'reasoning' && s.round === event.round)
      if (event.reasoning === 'private' && shown.length === 0) {
        steps = [...steps, { kind: 'private', id: nextId(t, 'private'), round: event.round, atMs }]
      }
      if (event.reasoning === 'text' && shown.length > 0) {
        const bytes = shown.reduce((sum, s) => sum + (s.kind === 'reasoning' ? encoder.encode(s.text).length : 0), 0)
        if (event.reasoning_bytes > bytes) {
          const lastShown = shown[shown.length - 1]
          steps = steps.map((s) => (s === lastShown && s.kind === 'reasoning' ? { ...s, cut: true } : s))
        }
      }
      const foot = makeFoot({
        round: event.round,
        atMs,
        promptTokens: event.prompt_tokens,
        completionTokens: event.completion_tokens,
        cachedTokens: event.cached_tokens,
        costUsd: event.cost_usd,
        reasoning: event.reasoning,
      })
      return { ...t, steps, rounds: [...t.rounds, foot] }
    }
    case 'message': {
      const next: MateTimeline = { ...t, phase: 'done' }
      if (event.run) {
        next.runId = event.run.id
        next.lookups = event.run.lookups
        if (event.run.thought_ms !== null) next.thoughtMs = event.run.thought_ms
      }
      return next
    }
    case 'error': {
      const text = event.error !== '' ? event.error : EMPTY_ERROR
      return {
        ...t,
        phase: 'failed',
        runId: event.run_id ?? t.runId,
        steps: [...t.steps, { kind: 'error', id: nextId(t, 'error'), text }],
        announcement: text,
      }
    }
    case 'stopped': {
      if (t.phase === 'done' || t.phase === 'failed' || t.phase === 'stopped') return t
      const steps = t.steps.map((s): MateStep => (s.kind === 'lookup' && s.outcome === 'running' ? { ...s, outcome: 'stopped' } : s))
      steps.push({ kind: 'stopped', id: nextId(t, 'stopped') })
      return { ...t, phase: 'stopped', steps, announcement: 'Stopped' }
    }
    default: {
      const unhandled: never = event
      throw new Error(`unhandled activity event ${JSON.stringify(unhandled)}`)
    }
  }
}

export type LookupStep = Extract<MateStep, { kind: 'lookup' }>

export type ReasoningSegment = { kind: 'heading' | 'text'; text: string }

const HEADING_LINE = /^\*\*([^*]+)\*\*$/

/** The segments so far and the plain lines not yet gathered into one. */
type SegmentState = { out: ReasoningSegment[]; plain: string[] }

function flushPlain(state: SegmentState): void {
  const body = state.plain.join('\n').replace(/^\n+|\n+$/g, '')
  if (body.trim() !== '') state.out.push({ kind: 'text', text: body })
  state.plain = []
}

function addLine(state: SegmentState, line: string): void {
  const title = HEADING_LINE.exec(line.trim())?.[1].trim() ?? ''
  if (title !== '') {
    flushPlain(state)
    state.out.push({ kind: 'heading', text: title })
  } else {
    state.plain.push(line)
  }
}

/**
 * Reasoning split into the title lines a model puts at the start of a block
 * (a line that is exactly **…**) and the plain text between them. Nothing else
 * in it is read as markdown.
 */
export function reasoningSegments(text: string): ReasoningSegment[] {
  const state: SegmentState = { out: [], plain: [] }
  for (const line of text.split('\n')) addLine(state, line)
  flushPlain(state)
  return state.out
}

/**
 * reasoningSegments for text that grows as it streams: the lines before the
 * last newline are read once and kept, so each new chunk reads only the
 * unfinished last line. Text that does not continue what it saw starts over.
 */
export class ReasoningSegmenter {
  private done: SegmentState = { out: [], plain: [] }
  private prefix = ''

  segments(text: string): ReasoningSegment[] {
    if (!text.startsWith(this.prefix)) {
      this.done = { out: [], plain: [] }
      this.prefix = ''
    }
    const rest = text.slice(this.prefix.length)
    const lastBreak = rest.lastIndexOf('\n')
    if (lastBreak >= 0) {
      for (const line of rest.slice(0, lastBreak).split('\n')) addLine(this.done, line)
      this.prefix += rest.slice(0, lastBreak + 1)
    }
    const tail = lastBreak >= 0 ? rest.slice(lastBreak + 1) : rest
    const state: SegmentState = { out: [...this.done.out], plain: [...this.done.plain] }
    addLine(state, tail)
    flushPlain(state)
    return state.out
  }
}

function withoutEllipsis(text: string): string {
  return text.replace(/(?:…|\.\.\.)$/, '')
}

/** A lookup in the operator's words: the ellipsis shows only while it runs. */
export function lookupLabel(step: LookupStep): string {
  return step.outcome === 'running' ? step.text : withoutEllipsis(step.text)
}

/** What a lookup was about: the server's subject, else its sentence without the ellipsis (a saved run's sentence is the tool's name when nothing describes it). */
function lookupSubject(step: LookupStep): string {
  return step.subject !== '' ? step.subject : withoutEllipsis(step.text)
}

type ActivityItem =
  | { kind: 'step'; id: string; step: Exclude<MateStep, { kind: 'lookup' }> }
  /** Lookups that came one after another in a round. */
  | { kind: 'burst'; id: string; lookups: LookupStep[] }

/** The steps with each run of consecutive same-round lookups gathered into one burst. */
export function groupSteps(steps: MateStep[]): ActivityItem[] {
  const items: ActivityItem[] = []
  for (const step of steps) {
    if (step.kind !== 'lookup') {
      items.push({ kind: 'step', id: step.id, step })
      continue
    }
    const last = items[items.length - 1]
    if (last !== undefined && last.kind === 'burst' && last.lookups[0].round === step.round) {
      last.lookups.push(step)
    } else {
      items.push({ kind: 'burst', id: step.id, lookups: [step] })
    }
  }
  return items
}

/** The steps the view lists: the private-reasoning note shows once, at the first round that kept its reasoning to itself. */
export function visibleSteps(steps: MateStep[]): MateStep[] {
  const firstPrivate = steps.findIndex((s) => s.kind === 'private')
  return steps.filter((s, i) => s.kind !== 'private' || i === firstPrivate)
}

/** Whether the view says "Thinking…": Mate is working with no lookup running and no text arriving. */
export function isThinking(t: MateTimeline): boolean {
  if (t.phase !== 'working' || runningLookups(t.steps)) return false
  const last = t.steps[t.steps.length - 1]?.kind
  return last !== 'reasoning' && last !== 'chatter'
}

export interface BurstSummary {
  subjects: string
  failed: number
  cut: number
  stopped: number
  skipped: number
}

/** One burst as a line: the subjects joined with " · " (a running one keeps its ellipsis), and how many did not finish well. */
export function burstSummary(lookups: LookupStep[]): BurstSummary {
  const count = (...outcomes: LookupStep['outcome'][]) => lookups.filter((l) => outcomes.includes(l.outcome)).length
  return {
    subjects: lookups.map((l) => (l.outcome === 'running' ? `${lookupSubject(l)}…` : lookupSubject(l))).join(' · '),
    failed: count('failed'),
    cut: count('cut'),
    stopped: count('stopped'),
    skipped: count('withheld', 'round_full'),
  }
}

/** Lookups that ran: the server's count when it gave one, otherwise the lookup steps that were not skipped. */
export function lookupCount(t: MateTimeline): number {
  if (t.lookups !== null) return t.lookups
  return t.steps.filter((s) => s.kind === 'lookup' && s.outcome !== 'withheld' && s.outcome !== 'round_full').length
}

/** "3¢", "<1¢", "$1.20". */
export function formatCostCents(usd: number): string {
  const cents = usd * 100
  if (cents < 1) return '<1¢'
  const whole = Math.round(cents)
  // Dollars from the rounded cents, so 99.5¢ reads "$1.00" and not "$0.99".
  if (whole >= 100) return `$${(whole / 100).toFixed(2)}`
  return `${whole}¢`
}

/** "Thought 12 s · 4 lookups · 3¢". A part that was not recorded is left out, so an old run shows no time. */
export function formatActivitySummary(parts: { thoughtMs: number | null; lookups: number; costUsd: number | null }): string {
  const out: string[] = []
  if (parts.thoughtMs !== null) out.push(`Thought ${formatRunDuration(parts.thoughtMs, { wholeSeconds: true })}`)
  if (parts.lookups > 0) out.push(`${parts.lookups} lookup${parts.lookups === 1 ? '' : 's'}`)
  if (parts.costUsd !== null && parts.costUsd > 0) out.push(formatCostCents(parts.costUsd))
  return out.join(' · ')
}

/** The folded line for a timeline. The rounds' cost is its own; a stream that sent no round totals gets the answer's cost instead. */
export function timelineSummary(t: MateTimeline, answerCostUsd: number | null = null): string {
  const cost = t.rounds.length > 0 ? t.rounds.reduce((sum, r) => sum + r.costUsd, 0) : answerCostUsd
  return formatActivitySummary({ thoughtMs: t.thoughtMs, lookups: lookupCount(t), costUsd: cost })
}

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

/** A saved run, read into the same steps the live stream builds. */
export function timelineFromTrace(trace: AssistantRunTrace): MateTimeline {
  const steps: MateStep[] = []
  const push = (step: (id: string) => MateStep) => steps.push(step(`${steps.length}`))
  const feet: MateRoundFoot[] = []

  for (const round of trace.rounds) {
    if (round.forcedFinal && steps.some((s) => s.kind === 'lookup')) {
      push((id) => ({ kind: 'note', id: `note-${id}`, text: FORCED_FINAL_NOTE }))
    }
    if (round.reasoning !== '') {
      push((id) => ({
        kind: 'reasoning',
        id: `reasoning-${id}`,
        round: round.index,
        atMs: round.startOffsetMs,
        text: round.reasoning,
        cut: round.reasoningBytes > encoder.encode(round.reasoning).length,
      }))
    } else if (round.reasoningKind === 'private') {
      push((id) => ({ kind: 'private', id: `private-${id}`, round: round.index, atMs: round.startOffsetMs }))
    }
    if (round.chatter !== '') {
      push((id) => ({ kind: 'chatter', id: `chatter-${id}`, round: round.index, atMs: round.startOffsetMs, text: round.chatter }))
    }
    for (const call of round.calls) {
      const source = call.cache === 'hit' ? ' · from memory' : call.cache === 'miss' ? ' · looked up' : ''
      push((id) => ({
        kind: 'lookup',
        id: `lookup-${id}`,
        round: round.index,
        seq: call.seq,
        atMs: null,
        // Empty only for a tool the server's describer does not know, so the
        // name is shown rather than nothing.
        text: call.text !== '' ? call.text : call.name,
        subject: call.subject,
        detail: call.args,
        meta: `${formatRunDuration(call.durationMs)} · ${formatBytes(call.resultBytes)}${source}`,
        outcome: call.outcome,
        error: call.reason,
      }))
    }
    if (round.checkTriggers.length > 0 || round.checkDraft !== '') {
      push((id) => ({
        kind: 'sent_back',
        id: `sent_back-${id}`,
        round: round.index,
        atMs: round.startOffsetMs,
        triggers: round.checkTriggers,
        label: sentBackLabel(round.checkTriggers),
        draft: round.checkDraft,
      }))
    }
    feet.push(
      makeFoot({
        round: round.index,
        atMs: round.startOffsetMs,
        latencyMs: round.latencyMs,
        promptTokens: round.promptTokens,
        completionTokens: round.completionTokens,
        cachedTokens: round.cachedTokens,
        costUsd: round.costUsd,
        reasoning: round.reasoningKind,
        state: roundState(round),
        error: round.error,
      }),
    )
  }

  let phase: ActivityPhase = 'done'
  if (trace.answerMessageId === null) {
    if (trace.stopReason === 'cancelled') {
      phase = 'stopped'
      push((id) => ({ kind: 'stopped', id: `stopped-${id}` }))
    } else {
      phase = 'failed'
      const failedRound = trace.rounds.find((r) => r.error !== '')
      const text = trace.error !== '' ? trace.error : (failedRound?.error ?? runOutcomeText(trace))
      push((id) => ({ kind: 'error', id: `error-${id}`, text }))
    }
  }

  return {
    ...emptyTimeline(),
    phase,
    steps,
    rounds: feet,
    thoughtMs: trace.thoughtMs,
    lookups: trace.lookupCount,
    runId: trace.id,
    outcome: runOutcomeText(trace),
    budget: runBudgetText(trace.budget),
  }
}

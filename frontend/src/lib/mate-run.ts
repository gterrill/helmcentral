// A Mate run's trace: how long each round took, what it asked for, and how the
// run ended. The wire shape is snake_case from the server; it is mapped to
// camelCase here so nothing else thinks about the wire.

export type MateStopReason = 'answered' | 'stalled' | 'round_ceiling' | 'time_ceiling' | 'cancelled' | 'error'

export interface RunBudgetApi {
  max_rounds: number
  max_stalled_rounds: number
  tool_phase_ms: number
  final_reserve_ms: number
}

export interface RunCallApi {
  seq: number
  name: string
  args: string
  elapsed_ms: number
  result_bytes: number
  error: string
  /** Whether the place search cache answered a find_places lookup; empty for every other call. */
  cache: 'hit' | 'miss' | ''
}

/** What a round was: lookups, a draft sent back for correction, the answer, the
 * final answer after the tools were withdrawn, a round cut off by the tool phase
 * running out of time, or one that failed. */
export type RunRoundKind = 'lookups' | 'correction' | 'answer' | 'final' | 'cut' | 'failed'

export interface RunRoundApi {
  index: number
  kind: RunRoundKind
  start_offset_ms: number
  latency_ms: number
  prompt_tokens: number
  completion_tokens: number
  cached_tokens: number
  cost_usd: number
  forced_final: boolean
  progress: boolean
  error: string
  calls: RunCallApi[]
}

export interface RunApi {
  id: string
  conversation_id: string
  after_seq: number
  answer_message_id: string | null
  model: string
  started_at: string
  duration_ms: number
  stop_reason: MateStopReason
  error: string
  round_count: number
  budget: RunBudgetApi
  rounds?: RunRoundApi[]
}

/** A run that gave an answer, as the conversation lists it: which answer it was. */
export interface RunRefApi {
  id: string
  answer_message_id: string
}

export interface RunBudget {
  maxRounds: number
  maxStalledRounds: number
  toolPhaseMs: number
  finalReserveMs: number
}

export interface RunCall {
  seq: number
  name: string
  args: string
  durationMs: number
  resultBytes: number
  error: string
  cache: 'hit' | 'miss' | ''
}

export interface RunRound {
  index: number
  kind: RunRoundKind
  startOffsetMs: number
  latencyMs: number
  promptTokens: number
  completionTokens: number
  cachedTokens: number
  costUsd: number
  forcedFinal: boolean
  progress: boolean
  error: string
  calls: RunCall[]
}

/** One run, without its rounds. */
export interface AssistantRun {
  id: string
  /** Sequence number of the question this run answered, to place it in the thread. */
  afterSeq: number
  /** The answer it produced; null when it gave none. */
  answerMessageId: string | null
  model: string
  startedAt: string
  durationMs: number
  stopReason: MateStopReason
  error: string
  /** Rounds of lookups, which is what the server's own error text counts. */
  roundCount: number
  budget: RunBudget
}

/** One run with its rounds, from the run's own endpoint. */
export interface AssistantRunTrace extends AssistantRun {
  rounds: RunRound[]
}

export function mapRun(api: RunApi): AssistantRun {
  return {
    id: api.id,
    afterSeq: api.after_seq,
    answerMessageId: api.answer_message_id,
    model: api.model,
    startedAt: api.started_at,
    durationMs: api.duration_ms,
    stopReason: api.stop_reason,
    error: api.error,
    roundCount: api.round_count,
    budget: {
      maxRounds: api.budget.max_rounds,
      maxStalledRounds: api.budget.max_stalled_rounds,
      toolPhaseMs: api.budget.tool_phase_ms,
      finalReserveMs: api.budget.final_reserve_ms,
    },
  }
}

export function mapRunTrace(api: RunApi): AssistantRunTrace {
  const rounds = (api.rounds ?? []).map((r): RunRound => ({
    index: r.index,
    kind: r.kind,
    startOffsetMs: r.start_offset_ms,
    latencyMs: r.latency_ms,
    promptTokens: r.prompt_tokens,
    completionTokens: r.completion_tokens,
    cachedTokens: r.cached_tokens,
    costUsd: r.cost_usd,
    forcedFinal: r.forced_final,
    progress: r.progress,
    error: r.error,
    calls: r.calls.map((c): RunCall => ({
      seq: c.seq,
      name: c.name,
      args: c.args,
      durationMs: c.elapsed_ms,
      resultBytes: c.result_bytes,
      error: c.error,
      cache: c.cache,
    })),
  }))
  return { ...mapRun(api), rounds }
}

/**
 * The runs a conversation lists, split: those that gave no answer, in full, to
 * be shown in the thread in their place; and for those that did, the run's id
 * by the answer it belongs to, so the answer can offer its details.
 */
function isAnsweredRun(api: RunApi | RunRefApi): api is RunRefApi {
  return api.answer_message_id !== null
}

export function splitConversationRuns(apiRuns: Array<RunApi | RunRefApi>): { unanswered: AssistantRun[]; idByAnswer: Map<string, string> } {
  const unanswered: AssistantRun[] = []
  const idByAnswer = new Map<string, string>()
  for (const api of apiRuns) {
    if (isAnsweredRun(api)) idByAnswer.set(api.answer_message_id, api.id)
    else unanswered.push(mapRun(api))
  }
  return { unanswered, idByAnswer }
}

/** "1 round", "11 rounds". */
export function rounds(n: number): string {
  return `${n} round${n === 1 ? '' : 's'}`
}

/** "4.1 s", "2 min 13 s". */
export function formatRunDuration(ms: number): string {
  if (ms < 1000) return `${Math.round(ms)} ms`
  const seconds = ms / 1000
  if (seconds < 60) return `${seconds.toFixed(1)} s`
  const whole = Math.round(seconds)
  return `${Math.floor(whole / 60)} min ${String(whole % 60).padStart(2, '0')} s`
}

/** Minutes for a budget figure: "10 min", "90 s". */
function formatBudgetTime(ms: number): string {
  return ms % 60000 === 0 ? `${ms / 60000} min` : `${Math.round(ms / 1000)} s`
}

/** "+1:12" from the start of the run. */
export function formatRunOffset(ms: number): string {
  const total = Math.round(ms / 1000)
  return `+${Math.floor(total / 60)}:${String(total % 60).padStart(2, '0')}`
}

/**
 * The one-line account of how a run ended: what the thread says in place of an
 * answer that never came, and what the run's details open with. The words stay
 * here, not on the server, which sends only the stop reason.
 */
export function runOutcomeText(run: AssistantRun): string {
  if (run.answerMessageId === null) return unansweredText(run)
  switch (run.stopReason) {
    case 'stalled':
      return `The last ${rounds(run.budget.maxStalledRounds)} found nothing new, so Mate was asked to answer.`
    case 'round_ceiling':
      return `Mate reached its limit of ${rounds(run.budget.maxRounds)} and was asked to answer.`
    case 'time_ceiling':
      return `The ${formatBudgetTime(run.budget.toolPhaseMs)} for lookups ran out, so Mate was asked to answer.`
    default:
      return 'Mate answered when it was ready.'
  }
}

function unansweredText(run: AssistantRun): string {
  if (run.stopReason === 'cancelled') return 'You stopped Mate before it answered.'
  const after = run.roundCount > 0 ? ` after ${rounds(run.roundCount)}` : ''
  switch (run.stopReason) {
    case 'time_ceiling':
      return `Mate stopped before answering: it ran out of time${after}.`
    case 'round_ceiling':
      return `Mate stopped before answering: it reached its limit of ${rounds(run.budget.maxRounds)}.`
    case 'stalled':
      return `Mate stopped before answering: it stopped finding anything new${after}, and then could not write its answer.`
    default:
      return `Mate stopped before answering${after}.`
  }
}

/** The budget a run was given, in a sentence. */
export function runBudgetText(budget: RunBudget): string {
  // A zero stall rule or final reserve is one the run never had (a run recorded
  // by an earlier version), so it is left out rather than shown as zero.
  const stall = budget.maxStalledRounds > 0 ? `, stopping early after ${rounds(budget.maxStalledRounds)} with nothing new` : ''
  const reserve = budget.finalReserveMs > 0 ? `, then ${formatBudgetTime(budget.finalReserveMs)} to write the answer` : ''
  return `Up to ${rounds(budget.maxRounds)} and ${formatBudgetTime(budget.toolPhaseMs)} of lookups${stall}${reserve}.`
}

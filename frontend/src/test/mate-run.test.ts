import { describe, expect, it } from 'vitest'

import {
  formatRunDuration,
  formatRunOffset,
  mapRun,
  mapRunTrace,
  runBudgetText,
  runOutcomeText,
  splitConversationRuns,
  type AssistantRun,
  type RunApi,
} from '@/lib/mate-run'

const budget = { max_rounds: 30, max_stalled_rounds: 2, tool_phase_ms: 600000, final_reserve_ms: 120000 }

function api(overrides: Partial<RunApi> = {}): RunApi {
  return {
    id: 'run-1',
    conversation_id: 'c1',
    after_seq: 1,
    answer_message_id: null,
    model: 'anthropic/claude-sonnet-4.5',
    started_at: '2026-10-31T09:00:00Z',
    duration_ms: 133000,
    stop_reason: 'time_ceiling',
    error: 'read openrouter stream: context deadline exceeded',
    round_count: 11,
    budget,
    ...overrides,
  }
}

function run(overrides: Partial<RunApi> = {}): AssistantRun {
  return mapRun(api(overrides))
}

describe('mapRun', () => {
  it('maps the wire shape', () => {
    const r = run({ error: '' })
    expect(r).toMatchObject({ id: 'run-1', afterSeq: 1, answerMessageId: null, stopReason: 'time_ceiling', error: '', roundCount: 11 })
    expect(r.budget).toEqual({ maxRounds: 30, maxStalledRounds: 2, toolPhaseMs: 600000, finalReserveMs: 120000 })
  })

  it('maps rounds and calls, taking the round count from the server', () => {
    const trace = mapRunTrace(api({
      round_count: 1,
      rounds: [{
        index: 0, kind: 'lookups', start_offset_ms: 0, latency_ms: 4100, prompt_tokens: 900, completion_tokens: 40, cached_tokens: 600,
        cost_usd: 0.004, forced_final: false, progress: true, error: '',
        calls: [{ seq: 0, name: 'find_places', args: '{"query":"Cairns"}', elapsed_ms: 2300, result_bytes: 812, error: '', cache: 'miss' }],
      }],
    }))
    expect(trace.roundCount).toBe(1)
    expect(trace.rounds[0].calls[0]).toMatchObject({ name: 'find_places', cache: 'miss', error: '', durationMs: 2300 })
  })

  it('splits a conversation\'s runs into those without an answer and the answers\' run ids', () => {
    const split = splitConversationRuns([api(), { id: 'run-2', answer_message_id: 'm1' }])
    expect(split.unanswered.map((r) => r.id)).toEqual(['run-1'])
    expect(split.idByAnswer.get('m1')).toBe('run-2')
  })
})

describe('runOutcomeText for a run that gave no answer', () => {
  it('says it ran out of time after the rounds it made, without the raw error', () => {
    expect(runOutcomeText(run())).toBe('Mate stopped before answering: it ran out of time after 11 rounds.')
  })

  it('names the round limit', () => {
    expect(runOutcomeText(run({ stop_reason: 'round_ceiling' }))).toBe('Mate stopped before answering: it reached its limit of 30 rounds.')
  })

  it('says a stalled run could not write its answer', () => {
    expect(runOutcomeText(run({ stop_reason: 'stalled', round_count: 4 }))).toBe(
      'Mate stopped before answering: it stopped finding anything new after 4 rounds, and then could not write its answer.',
    )
  })

  it('reads plainly for an error and for a stop', () => {
    expect(runOutcomeText(run({ stop_reason: 'error', round_count: 1 }))).toBe('Mate stopped before answering after 1 round.')
    expect(runOutcomeText(run({ stop_reason: 'error', round_count: 0 }))).toBe('Mate stopped before answering.')
    expect(runOutcomeText(run({ stop_reason: 'cancelled' }))).toBe('You stopped Mate before it answered.')
  })

  it('uses no internal identifiers in any headline', () => {
    for (const stop of ['time_ceiling', 'round_ceiling', 'stalled', 'error', 'cancelled'] as const) {
      expect(runOutcomeText(run({ stop_reason: stop }))).not.toMatch(/_|openrouter|ceiling/i)
    }
  })
})

describe('run details wording', () => {
  it('explains how an answered run ended', () => {
    expect(runOutcomeText(run({ stop_reason: 'answered', answer_message_id: 'm1' }))).toBe('Mate answered when it was ready.')
    expect(runOutcomeText(run({ stop_reason: 'stalled', answer_message_id: 'm1' }))).toBe(
      'The last 2 rounds found nothing new, so Mate was asked to answer.',
    )
    expect(runOutcomeText(run({ stop_reason: 'time_ceiling', answer_message_id: 'm1' }))).toBe(
      'The 10 min for lookups ran out, so Mate was asked to answer.',
    )
  })

  it('states the budget', () => {
    expect(runBudgetText(mapRun(api()).budget)).toBe(
      'Up to 30 rounds and 10 min of lookups, stopping early after 2 rounds with nothing new, then 2 min to write the answer.',
    )
  })

  it('leaves out a stall rule and a final reserve the run never had', () => {
    const legacy = { max_rounds: 8, max_stalled_rounds: 0, tool_phase_ms: 180000, final_reserve_ms: 0 }
    expect(runBudgetText(mapRun(api({ budget: legacy })).budget)).toBe('Up to 8 rounds and 3 min of lookups.')
  })

  it('formats durations and offsets', () => {
    expect(formatRunDuration(640)).toBe('640 ms')
    expect(formatRunDuration(4100)).toBe('4.1 s')
    expect(formatRunDuration(133000)).toBe('2 min 13 s')
    expect(formatRunOffset(72000)).toBe('+1:12')
  })
})

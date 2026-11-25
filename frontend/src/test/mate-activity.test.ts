import { describe, expect, it } from 'vitest'

import type { MateEvent } from '@/lib/mate-events'
import {
  applyMateEvent,
  emptyTimeline,
  burstSummary,
  formatActivitySummary,
  formatCostCents,
  groupSteps,
  lookupCount,
  lookupLabel,
  ReasoningSegmenter,
  reasoningSegments,
  sentBackLabel,
  timelineFromTrace,
  timelineSummary,
  type MateStep,
  type MateTimeline,
} from '@/lib/mate-activity'
import { mapRunTrace, type RunApi, type RunRoundApi } from '@/lib/mate-run'

// The reducer behind the activity view (ADR 0305's stream, read as steps) and
// the mapper that reads a saved run into the same shape.

const foot = { prompt_tokens: 900, completion_tokens: 40, cached_tokens: 600, cost_usd: 0.004 }

const ev = {
  round: (round: number, at_ms = 0, final = false): MateEvent => ({ type: 'round', round, at_ms, final }),
  reasoning: (round: number, text: string): MateEvent => ({ type: 'reasoning', round, text }),
  chatter: (round: number, text: string): MateEvent => ({ type: 'chatter', round, text }),
  lookup: (round: number, seq: number, text: string, at_ms = 10, skipped: '' | 'withheld' | 'round_full' = '', subject = ''): MateEvent => ({
    type: 'lookup', round, seq, at_ms, text, subject, skipped,
  }),
  done: (round: number, seq: number, extra: { outcome?: 'done' | 'failed' | 'cut' | 'stopped'; reason?: string; at_ms?: number } = {}): MateEvent => ({
    type: 'lookup_done', round, seq, at_ms: extra.at_ms ?? 20, outcome: extra.outcome ?? 'done', reason: extra.reason ?? '',
  }),
  answerStart: (round: number, at_ms: number): MateEvent => ({ type: 'answer_start', round, at_ms }),
  delta: (text: string): MateEvent => ({ type: 'delta', text }),
  withdrawn: (round: number, reason: 'lookups' | 'cut_off' | 'failed' = 'lookups'): MateEvent => ({
    type: 'answer_withdrawn', round, at_ms: 50, reason,
  }),
  sentBack: (round: number, triggers: string[], draft: string, at_ms = 70): MateEvent => ({
    type: 'draft_sent_back', round, at_ms, triggers, draft,
  }),
  end: (
    round: number,
    reasoning: 'text' | 'private' | 'none' | '' = 'text',
    reasoning_bytes = 0,
    at_ms = 100,
    extra: Partial<Extract<MateEvent, { type: 'round_end' }>> = {},
  ): MateEvent => ({ type: 'round_end', round, at_ms, reasoning, reasoning_bytes, ...foot, ...extra }),
  message: (run?: { id: string; thought_ms: number | null; lookups: number }): MateEvent => ({
    type: 'message',
    message: { id: 'm1' } as never,
    conversation: { id: 'c1' } as never,
    ...(run ? { run } : {}),
  }),
  error: (error: string): MateEvent => ({ type: 'error', error }),
}

function run(events: Array<MateEvent | { type: 'stopped' }>, from: MateTimeline = emptyTimeline()): MateTimeline {
  return events.reduce(applyMateEvent, from)
}

const kinds = (t: MateTimeline) => t.steps.map((s) => s.kind)
const lookups = (t: MateTimeline) => t.steps.filter((s): s is Extract<MateStep, { kind: 'lookup' }> => s.kind === 'lookup')

describe('applyMateEvent', () => {
  it('starts working with nothing in it', () => {
    const t = emptyTimeline()
    expect(t.phase).toBe('working')
    expect(t.steps).toEqual([])
    expect(t.thoughtMs).toBeNull()
  })

  it('keeps reasoning, a lookup and its result in the order they arrived', () => {
    const t = run([
      ev.round(0), ev.reasoning(0, 'Need the tide.'), ev.reasoning(0, ' Then the wind.'),
      ev.lookup(0, 0, 'Looking up the tide'), ev.done(0, 0), ev.end(0, 'text', 29),
    ])
    expect(kinds(t)).toEqual(['reasoning', 'lookup'])
    expect(t.steps[0]).toMatchObject({ kind: 'reasoning', text: 'Need the tide. Then the wind.' })
    expect(lookups(t)[0]).toMatchObject({ text: 'Looking up the tide', outcome: 'done' })
    expect(t.phase).toBe('working')
  })

  it('shows a round with reasoning and no lookups, then the answer', () => {
    const t = run([ev.round(0), ev.reasoning(0, 'Easy one.'), ev.answerStart(0, 1800), ev.delta('Yes.'), ev.end(0, 'text', 9)])
    expect(kinds(t)).toEqual(['reasoning'])
    expect(t.phase).toBe('answering')
    expect(t.thoughtMs).toBe(1800)
  })

  it('shows lookups from a model that gave no reasoning, and says nothing about it', () => {
    const t = run([ev.round(0), ev.lookup(0, 0, 'Looking up the wind'), ev.done(0, 0), ev.end(0, 'none')])
    expect(kinds(t)).toEqual(['lookup'])
    expect(t.rounds[0].reasoning).toBe('none')
  })

  it('records a private round as one private step, whatever the model sent', () => {
    const t = run([ev.round(0), ev.end(0, 'private'), ev.round(1, 100), ev.end(1, 'private')])
    expect(kinds(t)).toEqual(['private', 'private'])
  })

  it('does not add a private step to a round that showed its reasoning', () => {
    const t = run([ev.round(0), ev.reasoning(0, 'Thinking out loud.'), ev.end(0, 'private')])
    expect(kinds(t)).toEqual(['reasoning'])
  })

  it('keeps live order when reasoning arrives after content', () => {
    const t = run([
      ev.round(0), ev.reasoning(0, 'first'), ev.chatter(0, 'Let me check.'), ev.reasoning(0, 'second'),
    ])
    expect(kinds(t)).toEqual(['reasoning', 'chatter', 'reasoning'])
  })

  it('puts chatter before the lookup it came with, as one step', () => {
    const t = run([
      ev.round(0), ev.chatter(0, 'Let me '), ev.chatter(0, 'check the tide.'), ev.lookup(0, 0, 'Looking up the tide'),
    ])
    expect(kinds(t)).toEqual(['chatter', 'lookup'])
    expect(t.steps[0]).toMatchObject({ text: 'Let me check the tide.' })
  })

  it('marks the lookup that is running, and a failed one with its reason', () => {
    let t = run([ev.round(0), ev.lookup(0, 0, 'Looking up the tide'), ev.lookup(0, 1, 'Looking up the wind')])
    expect(lookups(t).map((l) => l.outcome)).toEqual(['running', 'running'])
    t = run([ev.done(0, 0, { outcome: 'failed', reason: 'station offline' }), ev.done(0, 1, { outcome: 'cut' })], t)
    expect(lookups(t)[0]).toMatchObject({ outcome: 'failed', error: 'station offline' })
    expect(lookups(t)[1]).toMatchObject({ outcome: 'cut' })
  })

  it('shows a lookup that was stopped, apart from one that failed', () => {
    const t = run([ev.round(0), ev.lookup(0, 0, 'Looking up the tide'), ev.done(0, 0, { outcome: 'stopped' })])
    expect(lookups(t)[0]).toMatchObject({ outcome: 'stopped' })
  })

  it('shows a skipped lookup as skipped and never as running', () => {
    const t = run([ev.round(0), ev.lookup(0, 0, 'Looking up the tide', 10, 'withheld'), ev.lookup(0, 1, 'Looking up the wind', 10, 'round_full')])
    expect(lookups(t).map((l) => l.outcome)).toEqual(['withheld', 'round_full'])
    expect(lookupCount(t)).toBe(0)
  })

  it('counts lookups that ran, cut ones included', () => {
    const t = run([ev.round(0), ev.lookup(0, 0, 'a'), ev.lookup(0, 1, 'b'), ev.done(0, 1, { outcome: 'cut' })])
    expect(lookupCount(t)).toBe(2)
  })

  it('folds on answer_start and says when the answer began', () => {
    const t = run([ev.round(0), ev.answerStart(0, 4200)])
    expect(t.phase).toBe('answering')
    expect(t.thoughtMs).toBe(4200)
    expect(t.announcement).toBe('Answer started')
  })

  it('reaches done on the message, taking the time from the stored run when the stream gave none', () => {
    const t = run([ev.round(0), ev.message({ id: 'r1', thought_ms: 7000, lookups: 2 })])
    expect(t.phase).toBe('done')
    expect(t.runId).toBe('r1')
    expect(t.thoughtMs).toBe(7000)
  })

  it('keeps the time the stream gave when the stored run has none', () => {
    const t = run([ev.round(0), ev.answerStart(0, 3000), ev.message({ id: 'r1', thought_ms: null, lookups: 0 })])
    expect(t.thoughtMs).toBe(3000)
  })

  it('lists a forced final round as the reason lookups stopped', () => {
    const t = run([
      ev.round(0), ev.lookup(0, 0, 'Looking up the tide'), ev.done(0, 0), ev.end(0),
      ev.round(1, 200, true), ev.answerStart(1, 210), ev.delta('Here.'),
    ])
    expect(kinds(t)).toEqual(['lookup', 'note'])
    expect(t.steps[1]).toMatchObject({ text: 'Lookups stopped, so Mate was asked to answer.' })
    expect(t.phase).toBe('answering')
  })

  it('does not add that note when the only round is final', () => {
    const t = run([ev.round(0, 0, true), ev.answerStart(0, 10)])
    expect(kinds(t)).toEqual([])
  })

  describe('an answer taken back', () => {
    it('moves the shown answer text into the round chatter and goes back to working', () => {
      const t = run([
        ev.round(0), ev.answerStart(0, 900), ev.delta('Let me '), ev.delta('check the tide.'),
        ev.withdrawn(0, 'lookups'), ev.chatter(0, ' Back shortly.'), ev.lookup(0, 0, 'Looking up the tide'),
      ])
      expect(t.phase).toBe('working')
      expect(t.thoughtMs).toBeNull()
      expect(t.answer).toBe('')
      expect(kinds(t)).toEqual(['chatter', 'lookup'])
      expect(t.steps[0]).toMatchObject({ text: 'Let me check the tide. Back shortly.' })
    })

    it('moves text for a cut-off round the same way', () => {
      const t = run([ev.round(0), ev.answerStart(0, 900), ev.delta('Half an ans'), ev.withdrawn(0, 'cut_off')])
      expect(t.steps).toHaveLength(1)
      expect(t.steps[0]).toMatchObject({ kind: 'chatter', text: 'Half an ans' })
    })

    it('adds nothing when no text had been shown', () => {
      const t = run([ev.round(0), ev.answerStart(0, 900), ev.withdrawn(0, 'failed')])
      expect(kinds(t)).toEqual([])
      expect(t.phase).toBe('working')
    })
  })

  describe('a draft sent back', () => {
    const events = [
      ev.round(0), ev.answerStart(0, 2000), ev.delta('Whites Bay is sheltered.'),
      ev.sentBack(0, ['anchorage shelter'], 'Whites Bay is sheltered from the north.'),
    ]

    it('is one step carrying the whole draft, and unfolds back to working', () => {
      const t = run(events)
      expect(t.phase).toBe('working')
      expect(t.thoughtMs).toBeNull()
      expect(t.answer).toBe('')
      expect(t.steps).toHaveLength(1)
      expect(t.steps[0]).toMatchObject({
        kind: 'sent_back',
        triggers: ['anchorage shelter'],
        draft: 'Whites Bay is sheltered from the north.',
        label: 'Draft sent back: shelter at the anchorage',
      })
      expect(t.announcement).toBe('Draft sent back: shelter at the anchorage')
    })

    it('folds again on the second answer and takes the thought time from it', () => {
      const t = run([...events, ev.round(1, 3000), ev.answerStart(1, 5500), ev.delta('Fine.'), ev.message({ id: 'r', thought_ms: 5500, lookups: 0 })])
      expect(t.phase).toBe('done')
      expect(t.thoughtMs).toBe(5500)
      expect(kinds(t)).toEqual(['sent_back'])
    })
  })

  describe('a run that does not finish', () => {
    it('stays open as failed with the error as its last step, partial reasoning kept', () => {
      const t = run([ev.round(0), ev.reasoning(0, 'Working it out'), ev.error('upstream 502')], emptyTimeline())
      expect(t.phase).toBe('failed')
      expect(kinds(t)).toEqual(['reasoning', 'error'])
      expect(t.steps[1]).toMatchObject({ text: 'upstream 502' })
    })

    it('says an empty error plainly', () => {
      const t = run([ev.error('')])
      expect(t.steps[0]).toMatchObject({ kind: 'error', text: 'Mate stopped before answering.' })
    })

    it('keeps the timeline on Stop, ends it with a Stopped step, and stops the lookups still running', () => {
      const t = run([
        ev.round(0), ev.reasoning(0, 'Checking'), ev.lookup(0, 0, 'Looking up the tide'),
        ev.lookup(0, 1, 'Looking up the wind'), ev.done(0, 1), { type: 'stopped' },
      ])
      expect(t.phase).toBe('stopped')
      expect(kinds(t)).toEqual(['reasoning', 'lookup', 'lookup', 'stopped'])
      expect(lookups(t).map((l) => l.outcome)).toEqual(['stopped', 'done'])
    })

    it('leaves a finished run alone when Stop arrives after it', () => {
      const done = run([ev.round(0), ev.message()])
      expect(run([{ type: 'stopped' }], done)).toBe(done)
    })
  })

  describe('the foot', () => {
    it('keeps each round tokens, cache and cost, and totals the cost', () => {
      const t = run([ev.round(0), ev.end(0), ev.round(1, 200), ev.end(1, 'text', 0, 300)])
      expect(t.rounds).toHaveLength(2)
      expect(t.rounds[0]).toMatchObject({ promptTokens: 900, completionTokens: 40, cachedTokens: 600, costUsd: 0.004 })
      expect(t.rounds.reduce((sum, r) => sum + r.costUsd, 0)).toBeCloseTo(0.008)
    })

    it('says reasoning was cut when the round wrote more than the stream carried', () => {
      const long = 'x'.repeat(100)
      const cut = run([ev.round(0), ev.reasoning(0, long), ev.end(0, 'text', 5000)])
      expect(cut.steps[0]).toMatchObject({ kind: 'reasoning', cut: true })
      const whole = run([ev.round(0), ev.reasoning(0, long), ev.end(0, 'text', 100)])
      expect(whole.steps[0]).toMatchObject({ kind: 'reasoning', cut: false })
    })

    it('counts bytes, not characters, when comparing', () => {
      const t = run([ev.round(0), ev.reasoning(0, 'é'.repeat(10)), ev.end(0, 'text', 20)])
      expect(t.steps[0]).toMatchObject({ cut: false })
    })
  })

  describe('a reload in the middle of a run', () => {
    it('rebuilds the same timeline from the replayed events', () => {
      const events = [
        ev.round(0), ev.reasoning(0, 'Need the tide.'), ev.lookup(0, 0, 'Looking up the tide'),
        ev.done(0, 0, { at_ms: 4000 }), ev.end(0, 'text', 14, 4000), ev.round(1, 4000),
      ]
      const live = run(events)
      const replayed = run([{ type: 'clock', at_ms: 4100 } as MateEvent, ...events])
      expect(replayed).toEqual(live)
    })
  })

  it('announces a lookup in the operator wording, and no announcement for streaming text', () => {
    let t = run([ev.round(0), ev.lookup(0, 0, 'Looking up the tide')])
    expect(t.announcement).toBe('Looking up the tide')
    const before = t.announcement
    t = run([ev.reasoning(0, 'more thoughts'), ev.chatter(0, 'chat'), ev.delta('x')], t)
    expect(t.announcement).toBe(before)
  })
})

describe('trigger labels', () => {
  it('says each check the way the operator approved', () => {
    expect(sentBackLabel(['relative wind'])).toBe('Draft sent back: wind angle wrong for the course')
    expect(sentBackLabel(['passage limits claim'])).toBe('Draft sent back: claim about your passage limits')
    expect(sentBackLabel(['anchorage shelter'])).toBe('Draft sent back: shelter at the anchorage')
    expect(sentBackLabel(['anchorage depth'])).toBe('Draft sent back: depth at the anchorage')
  })

  it('shows an unknown key as it is', () => {
    expect(sentBackLabel(['something new'])).toBe('Draft sent back: something new')
  })

  it('joins several with and', () => {
    expect(sentBackLabel(['anchorage shelter', 'anchorage depth'])).toBe(
      'Draft sent back: shelter at the anchorage and depth at the anchorage',
    )
  })

  it('says only that it was sent back when no reason was kept', () => {
    expect(sentBackLabel([])).toBe('Draft sent back')
  })
})

describe('formatCostCents', () => {
  it('rounds a cost just under a dollar to a dollar, not to 100 cents', () => {
    expect(formatCostCents(0.995)).toBe('$1.00')
    expect(formatCostCents(0.994)).toBe('99¢')
    expect(formatCostCents(1.2)).toBe('$1.20')
    expect(formatCostCents(0.004)).toBe('<1¢')
  })
})

describe('formatActivitySummary', () => {
  it('reads time, lookups and cost', () => {
    expect(formatActivitySummary({ thoughtMs: 12000, lookups: 4, costUsd: 0.03 })).toBe('Thought 12 s · 4 lookups · 3¢')
  })

  it('uses the singular for one lookup', () => {
    expect(formatActivitySummary({ thoughtMs: 5000, lookups: 1, costUsd: null })).toBe('Thought 5 s · 1 lookup')
  })

  it('drops what is not recorded', () => {
    expect(formatActivitySummary({ thoughtMs: null, lookups: 2, costUsd: 0.05 })).toBe('2 lookups · 5¢')
    expect(formatActivitySummary({ thoughtMs: 3000, lookups: 0, costUsd: 0 })).toBe('Thought 3 s')
    expect(formatActivitySummary({ thoughtMs: null, lookups: 0, costUsd: null })).toBe('')
  })

  it('shows a very short time and a very small cost without rounding them away', () => {
    expect(formatActivitySummary({ thoughtMs: 300, lookups: 0, costUsd: 0.002 })).toBe('Thought <1 s · <1¢')
  })

  it('reads minutes and dollars when they get large', () => {
    expect(formatActivitySummary({ thoughtMs: 135000, lookups: 0, costUsd: 1.234 })).toBe('Thought 2 min 15 s · $1.23')
  })
})

const budget = { max_rounds: 30, max_stalled_rounds: 2, tool_phase_ms: 600000, final_reserve_ms: 120000 }

function roundApi(over: Partial<RunRoundApi> = {}): RunRoundApi {
  return {
    index: 0, kind: 'lookups', start_offset_ms: 0, latency_ms: 4100, prompt_tokens: 900, completion_tokens: 40, cached_tokens: 600, cost_usd: 0.004,
    forced_final: false, progress: true, error: '', check_trigger: '', check_draft: '', calls: [],
    reasoning: '', reasoning_kind: 'text', reasoning_bytes: 0, chatter: '', check_triggers: [], content_start_ms: null,
    ...over,
  }
}

function traceApi(rounds: RunRoundApi[], over: Partial<RunApi> = {}): RunApi {
  return {
    id: 'run-1', conversation_id: 'c1', after_seq: 1, answer_message_id: 'm1', model: 'x', started_at: '2026-11-25T09:00:00Z',
    duration_ms: 20000, stop_reason: 'answered', error: '', round_count: rounds.length, budget,
    thought_ms: 12000, lookups: 1, reasoning_kind: 'text', rounds, ...over,
  }
}

const call = (over: object = {}) => ({
  seq: 0, name: 'get_tides', args: '{"lat":1}', elapsed_ms: 90, result_bytes: 300, error: '', cache: '' as const, outcome: 'done' as const, ...over,
})

describe('timelineFromTrace', () => {
  it('orders each round as reasoning, chatter, lookups, then the draft sent back', () => {
    const t = timelineFromTrace(mapRunTrace(traceApi([
      roundApi({
        reasoning: 'Need the tide.', reasoning_bytes: 14, chatter: 'Checking.', calls: [call()],
        check_draft: 'Old draft', check_triggers: ['relative wind'],
      }),
    ])))
    expect(kinds(t)).toEqual(['reasoning', 'chatter', 'lookup', 'sent_back'])
    expect(t.steps[3]).toMatchObject({ draft: 'Old draft', label: 'Draft sent back: wind angle wrong for the course' })
    expect(t.phase).toBe('done')
    expect(t.thoughtMs).toBe(12000)
  })

  it('reads the saved result of a lookup into its outcome', () => {
    const t = timelineFromTrace(mapRunTrace(traceApi([
      roundApi({
        calls: [
          call({ seq: 0 }),
          call({ seq: 1, outcome: 'failed', reason: 'tide station offline' }),
          call({ seq: 2, outcome: 'cut' }),
          call({ seq: 3, outcome: 'stopped' }),
          call({ seq: 4, outcome: 'withheld' }),
          call({ seq: 5, outcome: 'round_full' }),
        ],
      }),
    ])))
    expect(lookups(t).map((l) => l.outcome)).toEqual(['done', 'failed', 'cut', 'stopped', 'withheld', 'round_full'])
    expect(lookups(t)[1].error).toBe('tide station offline')
  })

  it('words a saved lookup as the server did and never as the tool name', () => {
    const t = timelineFromTrace(mapRunTrace(traceApi([
      roundApi({ calls: [call({ text: 'Looking up Cairns…' }), call({ seq: 1, name: 'brand_new_tool', text: '' })] }),
    ])))
    // An empty text happens only for a tool the describer does not know.
    expect(lookups(t).map((l) => l.text)).toEqual(['Looking up Cairns…', 'brand_new_tool'])
  })

  it('shows a saved failure as the reason the server sent, without the tool name', () => {
    const t = timelineFromTrace(mapRunTrace(traceApi([
      roundApi({
        calls: [call({
          name: 'marina_details', text: 'Looking up the marina…', outcome: 'failed',
          error: 'marina_details: plugin "osm-overpass": no marina charted within 500 m',
          reason: 'plugin "osm-overpass": no marina charted within 500 m',
        })],
      }),
    ])))
    expect(lookups(t)[0]).toMatchObject({
      outcome: 'failed',
      text: 'Looking up the marina…',
      error: 'plugin "osm-overpass": no marina charted within 500 m',
    })
  })

  it('notes a private round once and a round with no reasoning not at all', () => {
    const t = timelineFromTrace(mapRunTrace(traceApi([
      roundApi({ index: 0, reasoning_kind: 'private' }),
      roundApi({ index: 1, reasoning_kind: 'none' }),
    ])))
    expect(kinds(t)).toEqual(['private'])
  })

  it('marks reasoning that was stored cut', () => {
    const t = timelineFromTrace(mapRunTrace(traceApi([roundApi({ reasoning: 'abc', reasoning_bytes: 40000 })])))
    expect(t.steps[0]).toMatchObject({ kind: 'reasoning', cut: true })
  })

  it('shows an old run with no time and no reasoning note', () => {
    const t = timelineFromTrace(mapRunTrace(traceApi(
      [roundApi({ reasoning_kind: '', calls: [call()] })],
      { thought_ms: null, reasoning_kind: '' },
    )))
    expect(t.thoughtMs).toBeNull()
    expect(kinds(t)).toEqual(['lookup'])
    expect(t.rounds[0].reasoning).toBe('')
    expect(timelineSummary(t)).toBe('1 lookup · <1¢')
  })

  it('reads a draft sent back from a run stored before triggers were listed', () => {
    const t = timelineFromTrace(mapRunTrace(traceApi([
      roundApi({ check_trigger: 'anchorage depth', check_draft: 'Draft', check_triggers: null as never }),
    ])))
    expect(t.steps[0]).toMatchObject({ kind: 'sent_back', triggers: ['anchorage depth'] })
  })

  it('shows a correction round stored before drafts were kept as a step with no draft text', () => {
    const t = timelineFromTrace(mapRunTrace(traceApi([roundApi({ kind: 'correction', check_trigger: '', check_draft: '' })])))
    expect(kinds(t)).toEqual([])
  })

  it('ends a failed run open with the error as its last step', () => {
    const t = timelineFromTrace(mapRunTrace(traceApi(
      [roundApi({ kind: 'failed', error: 'context deadline exceeded' })],
      { answer_message_id: null, stop_reason: 'error', error: 'context deadline exceeded' },
    )))
    expect(t.phase).toBe('failed')
    expect(t.steps[t.steps.length - 1]).toMatchObject({ kind: 'error', text: 'context deadline exceeded' })
  })

  it('ends a run the operator stopped with a Stopped step', () => {
    const t = timelineFromTrace(mapRunTrace(traceApi([roundApi()], { answer_message_id: null, stop_reason: 'cancelled' })))
    expect(t.phase).toBe('stopped')
    expect(t.steps[t.steps.length - 1].kind).toBe('stopped')
  })

  it('says why lookups stopped when a round was forced to answer, and keeps the outcome and budget for the foot', () => {
    const t = timelineFromTrace(mapRunTrace(traceApi(
      [roundApi({ calls: [call()] }), roundApi({ index: 1, kind: 'final', forced_final: true })],
      { stop_reason: 'round_ceiling' },
    )))
    expect(kinds(t)).toEqual(['lookup', 'note'])
    expect(t.outcome).toMatch(/reached its limit of 30 rounds/)
    expect(t.budget).toMatch(/Up to 30 rounds/)
  })

  it('keeps each round for the foot, with its time and state', () => {
    const t = timelineFromTrace(mapRunTrace(traceApi([roundApi({ start_offset_ms: 72000 })])))
    expect(t.rounds[0]).toMatchObject({ round: 0, atMs: 72000, latencyMs: 4100, promptTokens: 900, cachedTokens: 600, state: 'progress' })
  })

  it('counts lookups from the run, which is what the stored list says', () => {
    const t = timelineFromTrace(mapRunTrace(traceApi([roundApi({ calls: [call(), call({ seq: 1, outcome: 'withheld' })] })], { lookups: 1 })))
    expect(t.lookups).toBe(1)
  })
})

describe('reasoningSegments', () => {
  it('turns a line that is only **…** into a heading, without the asterisks', () => {
    expect(reasoningSegments('**Advancing the Timeline**\n\nI need the tide first.')).toEqual([
      { kind: 'heading', text: 'Advancing the Timeline' },
      { kind: 'text', text: 'I need the tide first.' },
    ])
  })

  it('keeps the words as the model wrote them and handles several headings', () => {
    expect(reasoningSegments('  **Checking the wind**  \nFirst.\nSecond.\n**Then the swell**\nThird.')).toEqual([
      { kind: 'heading', text: 'Checking the wind' },
      { kind: 'text', text: 'First.\nSecond.' },
      { kind: 'heading', text: 'Then the swell' },
      { kind: 'text', text: 'Third.' },
    ])
  })

  it('leaves everything else as plain text, markdown and all', () => {
    expect(reasoningSegments('some **bold** words\n- a list')).toEqual([{ kind: 'text', text: 'some **bold** words\n- a list' }])
    expect(reasoningSegments('**unfinished')).toEqual([{ kind: 'text', text: '**unfinished' }])
    expect(reasoningSegments('****')).toEqual([{ kind: 'text', text: '****' }])
    expect(reasoningSegments('')).toEqual([])
  })

  it('does not read two bold phrases on one line as a heading', () => {
    expect(reasoningSegments('**A** vs **B**')).toEqual([{ kind: 'text', text: '**A** vs **B**' }])
    expect(reasoningSegments('**A** and more\n**B**')).toEqual([
      { kind: 'text', text: '**A** and more' },
      { kind: 'heading', text: 'B' },
    ])
  })
})

describe('ReasoningSegmenter', () => {
  const sample = [
    '**Checking the wind**\nFirst line.\nSecond **bold** line.\n\n',
    '**Then the swell**\n**A** vs **B**\nThird.\n',
    '  **Last one**  \ntrailing text without a newline',
  ].join('')

  it('gives the same segments as a full pass when the text grows in random chunks', () => {
    let seed = 7
    const rand = () => {
      seed = (seed * 1103515245 + 12345) % 2147483648
      return seed / 2147483648
    }
    for (let run = 0; run < 50; run++) {
      const segmenter = new ReasoningSegmenter()
      let at = 0
      while (at < sample.length) {
        at = Math.min(sample.length, at + 1 + Math.floor(rand() * 12))
        const text = sample.slice(0, at)
        expect(segmenter.segments(text)).toEqual(reasoningSegments(text))
      }
    }
  })

  it('starts over when the text is not a continuation of what it saw', () => {
    const segmenter = new ReasoningSegmenter()
    segmenter.segments('**One**\nabc\ndef')
    expect(segmenter.segments('**Two**\nxyz')).toEqual(reasoningSegments('**Two**\nxyz'))
  })
})

describe('a lookup subject in a burst line', () => {
  it('is the subject the server sent', () => {
    const items = groupSteps(run([ev.round(0), ev.lookup(0, 0, 'Fetching tides near Cairns…', 10, '', 'tides near Cairns'), ev.done(0, 0)]).steps)
    const burst = items[0] as Extract<(typeof items)[number], { kind: 'burst' }>
    expect(burstSummary(burst.lookups).subjects).toBe('tides near Cairns')
  })

  it('falls back to the sentence without its ellipsis, which for a saved tool nobody describes is the tool name', () => {
    const items = groupSteps(run([
      ev.round(0), ev.lookup(0, 0, 'Running get_brand_new_tool…'), ev.lookup(0, 1, 'get_other_tool'), ev.done(0, 0), ev.done(0, 1),
    ]).steps)
    const burst = items[0] as Extract<(typeof items)[number], { kind: 'burst' }>
    expect(burstSummary(burst.lookups).subjects).toBe('Running get_brand_new_tool · get_other_tool')
  })
})

describe('lookupLabel', () => {
  const step = (over: Partial<Extract<MateStep, { kind: 'lookup' }>> = {}): Extract<MateStep, { kind: 'lookup' }> => ({
    kind: 'lookup', id: 'l', round: 0, seq: 0, atMs: null, text: 'Looking up the tide…', subject: 'the tide', detail: '', meta: '', outcome: 'done', error: '', ...over,
  })

  it('keeps the ellipsis only while the lookup runs', () => {
    expect(lookupLabel(step({ outcome: 'running' }))).toBe('Looking up the tide…')
    for (const outcome of ['done', 'failed', 'cut', 'stopped', 'withheld', 'round_full'] as const) {
      expect(lookupLabel(step({ outcome }))).toBe('Looking up the tide')
    }
  })
})

describe('groupSteps and burstSummary', () => {
  const live = (events: Array<MateEvent | { type: 'stopped' }>) => groupSteps(run(events).steps)

  it('puts consecutive lookups of a round in one burst and breaks it at any other step', () => {
    const items = live([
      ev.round(0), ev.reasoning(0, 'Thinking.'),
      ev.lookup(0, 0, 'Looking up Cairns…'), ev.lookup(0, 1, 'Fetching tides near Cairns…'),
      ev.chatter(0, 'Next.'),
      ev.lookup(0, 2, 'Reading the plans…'),
    ])
    expect(items.map((i) => i.kind)).toEqual(['step', 'burst', 'step', 'burst'])
    const bursts = items.filter((i): i is Extract<typeof i, { kind: 'burst' }> => i.kind === 'burst')
    expect(bursts.map((b) => b.lookups.length)).toEqual([2, 1])
  })

  it('breaks a burst at a new round and keeps its id while it grows', () => {
    const first = live([ev.round(0), ev.lookup(0, 0, 'Looking up A…')])
    const grown = live([ev.round(0), ev.lookup(0, 0, 'Looking up A…'), ev.lookup(0, 1, 'Looking up B…')])
    expect(first[0].id).toBe(grown[0].id)
    const across = live([ev.round(0), ev.lookup(0, 0, 'Looking up A…'), ev.round(1, 100), ev.lookup(1, 0, 'Looking up B…')])
    expect(across.filter((i) => i.kind === 'burst')).toHaveLength(2)
  })

  it('joins the subjects with a dot, and shows a running lookup with its ellipsis and the finished ones without', () => {
    const items = live([
      ev.round(0), ev.lookup(0, 0, 'Looking up Cairns…', 10, '', 'Cairns'), ev.lookup(0, 1, 'Fetching tides near Cairns…', 10, '', 'tides near Cairns'), ev.done(0, 0),
    ])
    const burst = items[0] as Extract<(typeof items)[number], { kind: 'burst' }>
    expect(burstSummary(burst.lookups)).toEqual({ subjects: 'Cairns · tides near Cairns…', failed: 0, cut: 0, stopped: 0, skipped: 0 })
  })

  it('counts how the lookups ended, skipped ones together', () => {
    const items = live([
      ev.round(0),
      ev.lookup(0, 0, 'Looking up A…', 10, '', 'A'), ev.lookup(0, 1, 'Looking up B…', 10, '', 'B'), ev.lookup(0, 2, 'Looking up C…', 10, '', 'C'),
      ev.lookup(0, 3, 'Looking up D…', 10, 'withheld', 'D'), ev.lookup(0, 4, 'Looking up E…', 10, 'round_full', 'E'), ev.lookup(0, 5, 'Looking up F…', 10, '', 'F'),
      ev.done(0, 0, { outcome: 'failed', reason: 'boom' }), ev.done(0, 1, { outcome: 'cut' }), ev.done(0, 2, { outcome: 'stopped' }), ev.done(0, 5),
    ])
    const burst = items[0] as Extract<(typeof items)[number], { kind: 'burst' }>
    expect(burstSummary(burst.lookups)).toMatchObject({ failed: 1, cut: 1, stopped: 1, skipped: 2 })
    expect(burstSummary(burst.lookups).subjects).toBe('A · B · C · D · E · F')
  })

  it('reads a saved run into the same bursts', () => {
    const t = timelineFromTrace(mapRunTrace(traceApi([
      roundApi({
        calls: [
          call({ text: 'Looking up Cairns…', subject: 'Cairns' }),
          call({ seq: 1, text: 'Reading the plans…', subject: 'the plans', outcome: 'failed', reason: 'no plans' }),
        ],
      }),
    ])))
    const items = groupSteps(t.steps)
    const burst = items.find((i) => i.kind === 'burst') as Extract<(typeof items)[number], { kind: 'burst' }>
    expect(burstSummary(burst.lookups)).toMatchObject({ subjects: 'Cairns · the plans', failed: 1 })
  })
})

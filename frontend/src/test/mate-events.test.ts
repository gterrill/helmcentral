import { readdirSync, readFileSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, it } from 'vitest'

import { MateEventError, readMateEvents, type MateEvent } from '@/lib/mate-events'

// The golden files are written by the Go backend's own stream writer
// (backend/assistant_mate_events_test.go, `go test -update`). Decoding the
// same bytes here is the contract between the two (ADR 0189, ADR 0305).
const fixtureDir = join(__dirname, '../../../backend/testdata/mate_events')

function fixture(name: string): string {
  return readFileSync(join(fixtureDir, `${name}.sse`), 'utf8')
}

function bodyOf(text: string): ReadableStream<Uint8Array> {
  const bytes = new TextEncoder().encode(text)
  return new ReadableStream({
    start(controller) {
      controller.enqueue(bytes)
      controller.close()
    },
  })
}

async function decode(text: string): Promise<MateEvent[]> {
  const events: MateEvent[] = []
  await readMateEvents(bodyOf(text), (e) => events.push(e))
  return events
}

function frame(event: string, data: string): string {
  return `event: ${event}\ndata: ${data}\n\n`
}

const runId = '5d0f6c1a-77a2-4a3e-bb1f-0c2f4f6d9e11'

describe('readMateEvents golden fixtures', () => {
  // One entry per golden frame the backend writes; a frame with no entry here
  // is a shape the browser has never been checked against.
  const shapes: Array<[string, MateEvent]> = [
    ['clock', { type: 'clock', at_ms: 4200 }],
    ['round', { type: 'round', round: 1, at_ms: 1840, final: false }],
    ['reasoning', { type: 'reasoning', round: 1, text: 'Neaps at Mackay, so the bar is shallow at six.' }],
    ['chatter', { type: 'chatter', round: 1, text: 'Let me check the tide.' }],
    ['lookup', { type: 'lookup', round: 1, seq: 0, at_ms: 2100, text: 'Checking the tide at Mackay', subject: 'the tide at Mackay', skipped: '' }],
    ['lookup_skipped', { type: 'lookup', round: 1, seq: 21, at_ms: 2101, text: 'Checking the tide at Hay Point', subject: 'the tide at Hay Point', skipped: 'round_full' }],
    ['lookup_done', { type: 'lookup_done', round: 1, seq: 0, at_ms: 3050, outcome: 'done', reason: '' }],
    ['lookup_done_failed', { type: 'lookup_done', round: 1, seq: 1, at_ms: 3060, outcome: 'failed', reason: 'tide station offline' }],
    ['lookup_done_cut', { type: 'lookup_done', round: 1, seq: 2, at_ms: 3070, outcome: 'cut', reason: '' }],
    ['answer_start', { type: 'answer_start', round: 2, at_ms: 5300 }],
    ['delta', { type: 'delta', text: 'Tongue Bay first, ' }],
    ['answer_withdrawn', { type: 'answer_withdrawn', round: 1, at_ms: 2090, reason: 'lookups' }],
    ['answer_withdrawn_failed', { type: 'answer_withdrawn', round: 1, at_ms: 2091, reason: 'failed' }],
    [
      'draft_sent_back',
      {
        type: 'draft_sent_back',
        round: 2,
        at_ms: 6400,
        triggers: ['shelter at the anchorage', 'depth at the anchorage'],
        draft: 'Tongue Bay is well sheltered and has 3 m at low water.',
      },
    ],
    [
      'round_end',
      { type: 'round_end', round: 1, at_ms: 3070, reasoning: 'text', reasoning_bytes: 2150, prompt_tokens: 1200, completion_tokens: 340, cached_tokens: 900, cost_usd: 0.0042 },
    ],
    ['error', { type: 'error', error: 'stopped' }],
    ['error_run', { type: 'error', error: 'stopped', run_id: runId }],
  ]
  it.each(shapes)('decodes %s', async (name, want) => {
    expect(await decode(fixture(name))).toEqual([want])
  })
  it('has a decoder test for every golden frame the backend writes', () => {
    const covered = new Set([...shapes.map(([name]) => name), 'message', 'message_run', 'message_run_no_thought'])
    const written = readdirSync(fixtureDir).filter((f) => f.endsWith('.sse')).map((f) => f.replace(/\.sse$/, ''))
    expect(written.filter((name) => !covered.has(name))).toEqual([])
    expect([...covered].filter((name) => !written.includes(name))).toEqual([])
  })

  it('decodes message with its reply and conversation, and no run', async () => {
    const [event] = await decode(fixture('message'))
    expect(event.type).toBe('message')
    if (event.type !== 'message') return
    expect(event.message.id).toBe('0b6415af-2b90-487b-828e-c5db090bca36')
    expect(event.message.form_drafts).toHaveLength(1)
    expect(event.conversation.title).toBe('Can you fill this form in for me ?')
    expect(event.run).toBeUndefined()
  })
  it('decodes the run a message names', async () => {
    const [event] = await decode(fixture('message_run'))
    expect(event.type === 'message' && event.run).toEqual({ id: runId, thought_ms: 5300, lookups: 4 })
  })
  it('decodes a run with no thought time as null', async () => {
    const [event] = await decode(fixture('message_run_no_thought'))
    expect(event.type === 'message' && event.run).toEqual({ id: runId, thought_ms: null, lookups: 0 })
  })
  it('treats a frame with no event name as a message event and validates it as one', async () => {
    const data = '{"message":{"id":"m"},"conversation":{"id":"c"}}'
    const [event] = await decode(`data: ${data}\n\n`)
    expect(event.type).toBe('message')
    const err = await decode('data: {"text":"x"}\n\n').then(() => null, (e: unknown) => e)
    expect(err).toBeInstanceOf(MateEventError)
    expect((err as MateEventError).event).toBe('message')
  })
  it('decodes several frames in order', async () => {
    const events = await decode(fixture('clock') + fixture('round') + fixture('delta') + fixture('answer_withdrawn'))
    expect(events.map((e) => e.type)).toEqual(['clock', 'round', 'delta', 'answer_withdrawn'])
  })
})

describe('readMateEvents rejections', () => {
  const bad: Array<[string, string, string | RegExp]> = [
    ['an unknown event name', frame('tool_call', '{}'), 'tool_call'],
    ['the retired status event', frame('status', '{"text":"x"}'), 'status'],
    ['the retired retract event', frame('retract', '{}'), 'retract'],
    ['invalid JSON', frame('delta', '{nope'), 'delta'],
    ['a delta with a non-string text', frame('delta', '{"text":4}'), 'delta'],
    ['a delta with no text', frame('delta', '{}'), 'delta'],
    ['an error with no message', frame('error', '{}'), 'error'],
    ['an error whose run_id is not a string', frame('error', '{"error":"x","run_id":4}'), 'error'],
    ['a payload that is not an object', frame('clock', '[]'), 'clock'],
    ['a clock with no time', frame('clock', '{}'), 'clock'],
    ['a round with no final flag', frame('round', '{"round":0,"at_ms":1}'), 'round'],
    ['a reasoning with a numeric text', frame('reasoning', '{"round":0,"text":1}'), 'reasoning'],
    ['a chatter with no round', frame('chatter', '{"text":"x"}'), 'chatter'],
    ['a lookup with an unknown skipped reason', frame('lookup', '{"round":0,"seq":0,"at_ms":1,"text":"x","subject":"x","skipped":"slow"}'), 'lookup'],
    ['a lookup with no subject', frame('lookup', '{"round":0,"seq":0,"at_ms":1,"text":"x","skipped":""}'), 'lookup'],
    ['a lookup_done with no outcome', frame('lookup_done', '{"round":0,"seq":0,"at_ms":1,"reason":""}'), 'lookup_done'],
    ['a lookup_done with an unknown outcome', frame('lookup_done', '{"round":0,"seq":0,"at_ms":1,"outcome":"withheld","reason":""}'), 'lookup_done'],
    ['an answer_start with no time', frame('answer_start', '{"round":0}'), 'answer_start'],
    ['an answer_withdrawn with an unknown reason', frame('answer_withdrawn', '{"round":0,"at_ms":1,"reason":"tired"}'), 'answer_withdrawn'],
    ['a draft_sent_back whose triggers are not a list', frame('draft_sent_back', '{"round":0,"at_ms":1,"triggers":"x","draft":"d"}'), 'draft_sent_back'],
    ['a draft_sent_back with null triggers', frame('draft_sent_back', '{"round":0,"at_ms":1,"triggers":null,"draft":"d"}'), 'draft_sent_back'],
    [
      'a round_end with an unknown reasoning kind',
      frame('round_end', '{"round":0,"at_ms":1,"reasoning":"loud","reasoning_bytes":0,"prompt_tokens":1,"completion_tokens":1,"cached_tokens":0,"cost_usd":0}'),
      'round_end',
    ],
    ['a message with no conversation', frame('message', '{"message":{"id":"m"}}'), 'message'],
    ['a message whose message is not an object', frame('message', '{"message":"hi","conversation":{"id":"c"}}'), 'message'],
    ['a message whose conversation has no string id', frame('message', '{"message":{"id":"m"},"conversation":{"id":7}}'), 'message'],
    ['a message whose run is not an object', frame('message', '{"message":{"id":"m"},"conversation":{"id":"c"},"run":"r"}'), 'message'],
    ['a message whose run has no lookups', frame('message', '{"message":{"id":"m"},"conversation":{"id":"c"},"run":{"id":"r","thought_ms":null}}'), 'message'],
  ]
  it.each(bad)('throws MateEventError for %s', async (_label, text, name) => {
    const err = await decode(text).then(() => null, (e: unknown) => e)
    expect(err).toBeInstanceOf(MateEventError)
    expect((err as MateEventError).event).toBe(name)
    expect((err as MateEventError).message).toContain(String(name))
  })

  it('aborting the read leaves no unhandled rejection and no error', async () => {
    const unhandled: unknown[] = []
    const onUnhandled = (reason: unknown) => unhandled.push(reason)
    process.on('unhandledRejection', onUnhandled)
    const controller = new AbortController()
    const body = new ReadableStream<Uint8Array>({ start() {} })
    const done = readMateEvents(body, () => {}, controller.signal)
    controller.abort()
    await expect(done).resolves.toBeUndefined()
    await new Promise((r) => setTimeout(r, 10))
    process.off('unhandledRejection', onUnhandled)
    expect(unhandled).toEqual([])
  })

  it('delivers the events before the bad one', async () => {
    const seen: MateEvent[] = []
    await expect(
      readMateEvents(bodyOf(fixture('delta') + frame('mystery', '{}')), (e) => seen.push(e)),
    ).rejects.toBeInstanceOf(MateEventError)
    expect(seen).toHaveLength(1)
  })
})

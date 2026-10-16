import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, it } from 'vitest'

import { MateEventError, readMateEvents, type MateEvent } from '@/lib/mate-events'

// The golden files are written by the Go backend's own stream writer
// (backend/assistant_mate_events_test.go, `go test -update`). Decoding the
// same bytes here is the contract between the two (ADR 0189).
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

describe('readMateEvents golden fixtures', () => {
  it('decodes status text', async () => {
    expect(await decode(fixture('status_text'))).toEqual([{ type: 'status', text: 'Checking the tide at Mackay' }])
  })
  it('decodes status waiting', async () => {
    expect(await decode(fixture('status_waiting'))).toEqual([{ type: 'status', waiting: true }])
  })
  it('decodes delta', async () => {
    expect(await decode(fixture('delta'))).toEqual([{ type: 'delta', text: 'Tongue Bay first, ' }])
  })
  it('decodes retract', async () => {
    expect(await decode(fixture('retract'))).toEqual([{ type: 'retract' }])
  })
  it('decodes error', async () => {
    expect(await decode(fixture('error'))).toEqual([{ type: 'error', error: 'stopped' }])
  })
  it('decodes message with its reply and conversation', async () => {
    const [event] = await decode(fixture('message'))
    expect(event.type).toBe('message')
    if (event.type !== 'message') return
    expect(event.message.id).toBe('0b6415af-2b90-487b-828e-c5db090bca36')
    expect(event.message.form_drafts).toHaveLength(1)
    expect(event.conversation.title).toBe('Can you fill this form in for me ?')
  })
  it('decodes several frames in order', async () => {
    const events = await decode(fixture('status_waiting') + fixture('delta') + fixture('retract'))
    expect(events.map((e) => e.type)).toEqual(['status', 'delta', 'retract'])
  })
})

describe('readMateEvents rejections', () => {
  const bad: Array<[string, string, string | RegExp]> = [
    ['an unknown event name', frame('tool_call', '{}'), 'tool_call'],
    ['invalid JSON', frame('delta', '{nope'), 'delta'],
    ['a delta with a non-string text', frame('delta', '{"text":4}'), 'delta'],
    ['a delta with no text', frame('delta', '{}'), 'delta'],
    ['an error with no message', frame('error', '{}'), 'error'],
    ['a status with both text and kind', frame('status', '{"text":"x","kind":"waiting"}'), 'status'],
    ['a status with neither', frame('status', '{}'), 'status'],
    ['a status with an unknown kind', frame('status', '{"kind":"sleeping"}'), 'status'],
    ['a payload that is not an object', frame('retract', '[]'), 'retract'],
    ['a message with no conversation', frame('message', '{"message":{"id":"m"}}'), 'message'],
    ['a message whose message is not an object', frame('message', '{"message":"hi","conversation":{"id":"c"}}'), 'message'],
    ['a message whose conversation has no string id', frame('message', '{"message":{"id":"m"},"conversation":{"id":7}}'), 'message'],
  ]
  it.each(bad)('throws MateEventError for %s', async (_label, text, name) => {
    const err = await decode(text).then(() => null, (e: unknown) => e)
    expect(err).toBeInstanceOf(MateEventError)
    expect((err as MateEventError).event).toBe(name)
    expect((err as MateEventError).message).toContain(String(name))
  })

  it('delivers the events before the bad one', async () => {
    const seen: MateEvent[] = []
    await expect(
      readMateEvents(bodyOf(fixture('delta') + frame('mystery', '{}')), (e) => seen.push(e)),
    ).rejects.toBeInstanceOf(MateEventError)
    expect(seen).toHaveLength(1)
  })
})

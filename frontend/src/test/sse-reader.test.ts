import { describe, it, expect, vi } from 'vitest'
import { parseServerSentEventChunk, readServerSentEvents, type ServerSentEvent } from '@/lib/sse-reader'

// ADR 0093: the assistant's reply streams progress (`status`, `message`,
// `error` events) as Server-Sent Events framed on a POST response, since
// the browser's EventSource cannot POST. These tests exercise the pure
// frame parser directly, then the stream reader built on it.

function encode(chunks: string[]): Uint8Array[] {
  const encoder = new TextEncoder()
  return chunks.map((chunk) => encoder.encode(chunk))
}

function streamFrom(chunks: Uint8Array[]): ReadableStream<Uint8Array> {
  return new ReadableStream({
    start(controller) {
      for (const chunk of chunks) controller.enqueue(chunk)
      controller.close()
    },
  })
}

describe('parseServerSentEventChunk', () => {
  it('parses a single complete frame with event and data', () => {
    const { events, rest } = parseServerSentEventChunk('event: status\ndata: looking up\n\n')

    expect(events).toEqual([{ event: 'status', data: 'looking up' }])
    expect(rest).toBe('')
  })

  it('defaults the event name to "message" when no event: line is present', () => {
    const { events } = parseServerSentEventChunk('data: hello\n\n')

    expect(events).toEqual([{ event: 'message', data: 'hello' }])
  })

  it('joins multiple data: lines with a newline', () => {
    const { events } = parseServerSentEventChunk('event: message\ndata: line one\ndata: line two\n\n')

    expect(events).toEqual([{ event: 'message', data: 'line one\nline two' }])
  })

  it('strips a single leading space after the colon in a data: line', () => {
    const { events } = parseServerSentEventChunk('data:  two leading spaces kept minus one\n\n')

    expect(events[0].data).toBe(' two leading spaces kept minus one')
  })

  it('tolerates CRLF frame and line endings', () => {
    const { events, rest } = parseServerSentEventChunk('event: status\r\ndata: hello\r\n\r\n')

    expect(events).toEqual([{ event: 'status', data: 'hello' }])
    expect(rest).toBe('')
  })

  it('ignores comment lines starting with a colon', () => {
    const { events } = parseServerSentEventChunk(':keep-alive\nevent: status\ndata: hello\n\n')

    expect(events).toEqual([{ event: 'status', data: 'hello' }])
  })

  it('ignores id: and retry: lines', () => {
    const { events } = parseServerSentEventChunk('id: 42\nretry: 3000\nevent: status\ndata: hello\n\n')

    expect(events).toEqual([{ event: 'status', data: 'hello' }])
  })

  it('keeps a trailing partial frame in rest without emitting it', () => {
    const { events, rest } = parseServerSentEventChunk('event: status\ndata: hello\n\nevent: message\ndata: incompl')

    expect(events).toEqual([{ event: 'status', data: 'hello' }])
    expect(rest).toBe('event: message\ndata: incompl')
  })

  it('parses multiple complete frames in one buffer', () => {
    const { events, rest } = parseServerSentEventChunk('data: one\n\ndata: two\n\n')

    expect(events).toEqual([
      { event: 'message', data: 'one' },
      { event: 'message', data: 'two' },
    ])
    expect(rest).toBe('')
  })
})

describe('readServerSentEvents', () => {
  it('dispatches an event whose frame is split across two chunks', async () => {
    const stream = streamFrom(encode(['event: status\ndata: look', 'ing up\n\n']))
    const events: ServerSentEvent[] = []

    await readServerSentEvents(stream, (e) => events.push(e))

    expect(events).toEqual([{ event: 'status', data: 'looking up' }])
  })

  it('dispatches multiple events across chunk boundaries in order', async () => {
    const stream = streamFrom(
      encode(['event: status\ndata: one\n\nevent: sta', 'tus\ndata: two\n\n', 'data: three\n\n']),
    )
    const events: ServerSentEvent[] = []

    await readServerSentEvents(stream, (e) => events.push(e))

    expect(events).toEqual([
      { event: 'status', data: 'one' },
      { event: 'status', data: 'two' },
      { event: 'message', data: 'three' },
    ])
  })

  it('completes a trailing partial buffer with the next chunk', async () => {
    const stream = streamFrom(encode(['data: partial', ' complete\n\n']))
    const events: ServerSentEvent[] = []

    await readServerSentEvents(stream, (e) => events.push(e))

    expect(events).toEqual([{ event: 'message', data: 'partial complete' }])
  })

  it('stops reading when the signal aborts', async () => {
    const controller = new AbortController()
    let cancelled = false
    const encoder = new TextEncoder()

    const stream = new ReadableStream<Uint8Array>({
      start(streamController) {
        streamController.enqueue(encoder.encode('event: status\ndata: first\n\n'))
        // Deliberately never closes — simulates a long-lived server stream
        // that the caller aborts mid-flight.
      },
      cancel() {
        cancelled = true
      },
    })

    const onEvent = vi.fn(() => {
      controller.abort()
    })

    await readServerSentEvents(stream, onEvent, controller.signal)

    expect(onEvent).toHaveBeenCalledWith({ event: 'status', data: 'first' })
    expect(cancelled).toBe(true)
  })
})

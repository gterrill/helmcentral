import { vi } from 'vitest'

// Fakes for Mate's streamed answers (ADR 0105). A send or a rejoin reads
// status/delta/retract/message/error frames off a response body; these build
// such bodies without a server.

const encoder = new TextEncoder()

/** One SSE frame, as the backend writes it. */
export function sseFrame(event: string, data: unknown): string {
  return `event: ${event}\ndata: ${JSON.stringify(data)}\n\n`
}

/** A body that delivers every chunk at once and closes. */
export function sseStream(chunks: string[]): ReadableStream<Uint8Array> {
  return new ReadableStream({
    start(controller) {
      for (const chunk of chunks) controller.enqueue(encoder.encode(chunk))
      controller.close()
    },
  })
}

/**
 * A body the test pushes chunks into by hand, so an assertion about what
 * shows mid-stream never races however many microtask hops the reader needs:
 * every hop is awaited explicitly instead.
 */
export function controllableSseStream() {
  let streamController!: ReadableStreamDefaultController<Uint8Array>
  const stream = new ReadableStream<Uint8Array>({
    start(controller) { streamController = controller },
  })
  return {
    stream,
    push: (chunk: string) => streamController.enqueue(encoder.encode(chunk)),
    close: () => streamController.close(),
  }
}

/**
 * A body that never closes on its own; the reader has to give up on it.
 * `onCancel` fires when it does, which is how a test sees a superseded
 * stream being let go.
 */
export function neverEndingStream(onCancel: () => void): ReadableStream<Uint8Array> {
  return new ReadableStream({
    start(controller) {
      controller.enqueue(encoder.encode('event: status\ndata: {"text":"first request working"}\n\n'))
    },
    cancel() {
      onCancel()
    },
  })
}

// Passes an SSE body through unchanged, storing each `message` frame's
// message on the way past, as the real server stores a reply before it sends
// the frame. The session re-reads the conversation once an answer lands.
export function recordingBody(body: ReadableStream<Uint8Array>, record: (message: Record<string, unknown>) => void): ReadableStream<Uint8Array> {
  const reader = body.getReader()
  const decoder = new TextDecoder()
  let buffer = ''
  return new ReadableStream<Uint8Array>({
    async pull(controller) {
      const { done, value } = await reader.read()
      if (done) {
        controller.close()
        return
      }
      buffer += decoder.decode(value, { stream: true })
      let end = buffer.indexOf('\n\n')
      while (end !== -1) {
        const data = buffer.slice(0, end).split('\n').find((line) => line.startsWith('data: '))
        buffer = buffer.slice(end + 2)
        if (data) {
          const parsed = JSON.parse(data.slice(6)) as { message?: Record<string, unknown> }
          if (parsed.message) record(parsed.message)
        }
        end = buffer.indexOf('\n\n')
      }
      controller.enqueue(value)
    },
    cancel(reason) {
      return reader.cancel(reason)
    },
  })
}

export async function flushMicrotasks(times = 8) {
  for (let i = 0; i < times; i++) await Promise.resolve()
}

/**
 * Delta text is flushed to the draft at most once per animation frame.
 * happy-dom's rAF fires on a real timer, which would make tests slow and
 * non-deterministic, so this records the queued callback instead of calling
 * it, and `flush()` runs it on demand.
 */
export function installFakeRaf() {
  let queued: FrameRequestCallback | null = null
  let nextId = 1
  vi.stubGlobal('requestAnimationFrame', (cb: FrameRequestCallback) => {
    queued = cb
    return nextId++
  })
  vi.stubGlobal('cancelAnimationFrame', () => {
    queued = null
  })
  return {
    flush: () => {
      const cb = queued
      queued = null
      cb?.(0)
    },
    pending: () => queued !== null,
  }
}

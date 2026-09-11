/**
 * Minimal Server-Sent Events framing for a POST response body.
 *
 * The browser's `EventSource` can only issue a GET, and the assistant's
 * question (position, forecast/tide excerpts, standing notes) does not
 * belong in a URL, so its streamed reply (`status`, `message`, `error`
 * events) rides an ordinary `fetch` POST whose body is read as a
 * `ReadableStream` instead. See ADR 0093.
 */

export interface ServerSentEvent {
  event: string
  data: string
}

interface ParsedFrame {
  events: ServerSentEvent[]
  rest: string
}

/**
 * Splits `buffer` on blank lines (`\n\n`, tolerating `\r\n\r\n`) into
 * complete SSE frames plus whatever trailing partial frame hasn't arrived
 * yet. Each complete frame becomes one event: `event:` sets the event name
 * (default `message`), one or more `data:` lines are joined with `\n`
 * (a single leading space after the colon is stripped, per the SSE spec),
 * `id:`/`retry:` lines and lines starting with `:` (comments) are ignored.
 */
export function parseServerSentEventChunk(buffer: string): ParsedFrame {
  // Normalize CRLF frame/line breaks to LF so the rest of the parser only
  // has to handle one line ending.
  const normalized = buffer.replace(/\r\n/g, '\n')

  const frames = normalized.split('\n\n')
  // The last element is always the (possibly empty) trailing partial frame.
  // A buffer ending exactly on a frame boundary leaves it as ''.
  const rest = frames.pop() ?? ''

  const events: ServerSentEvent[] = []
  for (const frame of frames) {
    if (frame.length === 0) continue
    const event = parseFrame(frame)
    if (event) events.push(event)
  }

  return { events, rest }
}

function parseFrame(frame: string): ServerSentEvent | null {
  let eventName = 'message'
  const dataLines: string[] = []

  for (const line of frame.split('\n')) {
    if (line.length === 0 || line.startsWith(':')) continue

    const colonIndex = line.indexOf(':')
    const field = colonIndex === -1 ? line : line.slice(0, colonIndex)
    let value = colonIndex === -1 ? '' : line.slice(colonIndex + 1)
    if (value.startsWith(' ')) value = value.slice(1)

    if (field === 'event') {
      eventName = value
    } else if (field === 'data') {
      dataLines.push(value)
    }
    // id: and retry: are recognised but unused. This reader has no
    // reconnect/resume logic, so there's nothing to do with either.
  }

  if (dataLines.length === 0) return null
  return { event: eventName, data: dataLines.join('\n') }
}

/**
 * Reads `body` as a UTF-8 SSE stream, calling `onEvent` for each complete
 * frame in order as it arrives. Stops when the stream ends or when
 * `signal` aborts (cancelling the underlying reader either way). A
 * trailing partial frame left in the stream when it ends without a final
 * blank line is silently dropped.
 */
export async function readServerSentEvents(
  body: ReadableStream<Uint8Array>,
  onEvent: (event: ServerSentEvent) => void,
  signal?: AbortSignal,
): Promise<void> {
  const reader = body.getReader()
  const decoder = new TextDecoder()
  let buffer = ''

  const onAbort = () => {
    void reader.cancel()
  }
  signal?.addEventListener('abort', onAbort)

  try {
    while (!signal?.aborted) {
      const { done, value } = await reader.read()
      if (done) break

      buffer += decoder.decode(value, { stream: true })
      const { events, rest } = parseServerSentEventChunk(buffer)
      buffer = rest
      for (const event of events) onEvent(event)
    }
  } finally {
    signal?.removeEventListener('abort', onAbort)
  }
}

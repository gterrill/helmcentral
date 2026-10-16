import type { ConversationApi, MessageApi } from '@/lib/mate-api'
import { readServerSentEvents } from '@/lib/sse-reader'

/**
 * The closed set of events on Mate's browser stream. The Go side owns the
 * bytes (backend/assistant_run.go); backend/testdata/mate_events holds one
 * golden frame per shape and src/test/mate-events.test.ts decodes them
 * (ADR 0189). A frame that is not one of these is a contract break, not
 * something to skip.
 */
export type MateEvent =
  | { type: 'status'; text: string }
  | { type: 'status'; waiting: true }
  | { type: 'delta'; text: string }
  | { type: 'retract' }
  | { type: 'message'; message: MessageApi; conversation: ConversationApi }
  | { type: 'error'; error: string }

/**
 * What the operator reads when a frame cannot be decoded. The server run
 * carries on and stores the answer, so the advice is to reload, not to ask again.
 */
export const MATE_REPLY_NOT_SHOWN = "Mate's reply couldn't be shown. Reload to see it."

/** A frame on the stream that does not decode to a MateEvent. */
export class MateEventError extends Error {
  readonly event: string
  readonly reason: string

  constructor(event: string, reason: string) {
    super(`Mate stream: bad "${event}" event: ${reason}`)
    this.name = 'MateEventError'
    this.event = event
    this.reason = reason
  }
}

type Payload = Record<string, unknown>

function isObject(value: unknown): value is Payload {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

function hasStringId(value: unknown): boolean {
  return isObject(value) && typeof value.id === 'string'
}

function decodeMateEvent(name: string, data: string): MateEvent {
  let parsed: unknown
  try {
    parsed = JSON.parse(data)
  } catch {
    throw new MateEventError(name, 'data is not valid JSON')
  }
  if (!isObject(parsed)) throw new MateEventError(name, 'data is not a JSON object')

  switch (name) {
    case 'status': {
      const { text, kind } = parsed
      if (text !== undefined && kind !== undefined) throw new MateEventError(name, 'carries both text and kind')
      if (text !== undefined) {
        if (typeof text !== 'string') throw new MateEventError(name, 'text is not a string')
        return { type: 'status', text }
      }
      if (kind === 'waiting') return { type: 'status', waiting: true }
      throw new MateEventError(name, kind === undefined ? 'carries neither text nor kind' : `unknown kind ${JSON.stringify(kind)}`)
    }
    case 'delta':
      if (typeof parsed.text !== 'string') throw new MateEventError(name, 'text is not a string')
      return { type: 'delta', text: parsed.text }
    case 'retract':
      return { type: 'retract' }
    case 'message':
      // Top level only: the shape inside a message or conversation stays
      // with the mappers that read them.
      if (!hasStringId(parsed.message)) throw new MateEventError(name, 'message is not an object with a string id')
      if (!hasStringId(parsed.conversation)) throw new MateEventError(name, 'conversation is not an object with a string id')
      return {
        type: 'message',
        message: parsed.message as unknown as MessageApi,
        conversation: parsed.conversation as unknown as ConversationApi,
      }
    case 'error':
      if (typeof parsed.error !== 'string') throw new MateEventError(name, 'error is not a string')
      return { type: 'error', error: parsed.error }
    default:
      throw new MateEventError(name, 'unknown event')
  }
}

/**
 * Reads Mate's SSE body, calling `onEvent` with each decoded event in order.
 * Rejects with a MateEventError on the first frame that is not a valid
 * MateEvent; events before it have already been delivered. Aborting `signal`
 * ends the read as readServerSentEvents does, without an error.
 */
export function readMateEvents(
  body: ReadableStream<Uint8Array>,
  onEvent: (event: MateEvent) => void,
  signal?: AbortSignal,
): Promise<void> {
  return readServerSentEvents(body, (frame) => onEvent(decodeMateEvent(frame.event, frame.data)), signal)
}

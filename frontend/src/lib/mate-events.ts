import type { ConversationApi, MessageApi } from '@/lib/mate-api'
import { readServerSentEvents } from '@/lib/sse-reader'

/** What a round's model gave of its reasoning: readable text, a thought it keeps to itself, nothing, or "" when the round never reached the model's output. */
const REASONING_KINDS = ['text', 'private', 'none', ''] as const
export type MateReasoningKind = (typeof REASONING_KINDS)[number]

/** Why a lookup was not run: withheld after repeated failures, or past the round's call limit. "" for a call that ran. */
const SKIPPED = ['', 'withheld', 'round_full'] as const
export type MateSkipped = (typeof SKIPPED)[number]

/** Why text already shown as the answer was taken back. */
const WITHDRAWN_REASONS = ['lookups', 'cut_off', 'failed'] as const
export type MateWithdrawnReason = (typeof WITHDRAWN_REASONS)[number]

/** The stored trace of the run behind a reply. */
export interface MateRunRef {
  id: string
  thought_ms: number | null
  lookups: number
}

/**
 * The closed set of events on Mate's browser stream. The Go side owns the
 * bytes (backend/assistant_run.go); backend/testdata/mate_events holds one
 * golden frame per shape and src/test/mate-events.test.ts decodes them
 * (ADR 0189, ADR 0305). A frame that is not one of these is a contract break,
 * not something to skip. `at_ms` is milliseconds from the run's start.
 */
export type MateEvent =
  | { type: 'clock'; at_ms: number }
  | { type: 'round'; round: number; at_ms: number; final: boolean }
  | { type: 'reasoning'; round: number; text: string }
  | { type: 'chatter'; round: number; text: string }
  | { type: 'lookup'; round: number; seq: number; at_ms: number; text: string; skipped: MateSkipped }
  | { type: 'lookup_done'; round: number; seq: number; at_ms: number; error: string; cut: boolean }
  | { type: 'answer_start'; round: number; at_ms: number }
  | { type: 'delta'; text: string }
  | { type: 'answer_withdrawn'; round: number; at_ms: number; reason: MateWithdrawnReason }
  | { type: 'draft_sent_back'; round: number; at_ms: number; triggers: string[]; draft: string }
  | {
      type: 'round_end'
      round: number
      at_ms: number
      reasoning: MateReasoningKind
      /** Everything the model wrote of its reasoning; the reasoning events stop at a cap, so this says when they were cut. */
      reasoning_bytes: number
      prompt_tokens: number
      completion_tokens: number
      cached_tokens: number
      cost_usd: number
    }
  | { type: 'message'; message: MessageApi; conversation: ConversationApi; run?: MateRunRef }
  | { type: 'error'; error: string; run_id?: string }

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

/** Reads one event's fields, naming the event in the error when one is missing or the wrong kind. */
function fields(name: string, parsed: Payload) {
  return {
    str(key: string): string {
      const value = parsed[key]
      if (typeof value !== 'string') throw new MateEventError(name, `${key} is not a string`)
      return value
    },
    num(key: string): number {
      const value = parsed[key]
      if (typeof value !== 'number' || !Number.isFinite(value)) throw new MateEventError(name, `${key} is not a number`)
      return value
    },
    bool(key: string): boolean {
      const value = parsed[key]
      if (typeof value !== 'boolean') throw new MateEventError(name, `${key} is not a boolean`)
      return value
    },
    oneOf<T extends string>(key: string, allowed: readonly T[]): T {
      const value = parsed[key]
      if (typeof value !== 'string' || !(allowed as readonly string[]).includes(value)) {
        throw new MateEventError(name, `${key} is not one of ${allowed.map((a) => JSON.stringify(a)).join(', ')}`)
      }
      return value as T
    },
  }
}

function decodeRunRef(name: string, value: unknown): MateRunRef {
  if (!isObject(value)) throw new MateEventError(name, 'run is not an object')
  const { thought_ms: thought } = value
  if (thought !== null && (typeof thought !== 'number' || !Number.isFinite(thought))) {
    throw new MateEventError(name, 'run thought_ms is not a number or null')
  }
  const f = fields(name, value)
  return { id: f.str('id'), thought_ms: thought, lookups: f.num('lookups') }
}

function decodeMateEvent(name: string, data: string): MateEvent {
  let parsed: unknown
  try {
    parsed = JSON.parse(data)
  } catch {
    throw new MateEventError(name, 'data is not valid JSON')
  }
  if (!isObject(parsed)) throw new MateEventError(name, 'data is not a JSON object')

  const f = fields(name, parsed)
  switch (name) {
    case 'clock':
      return { type: 'clock', at_ms: f.num('at_ms') }
    case 'round':
      return { type: 'round', round: f.num('round'), at_ms: f.num('at_ms'), final: f.bool('final') }
    case 'reasoning':
      return { type: 'reasoning', round: f.num('round'), text: f.str('text') }
    case 'chatter':
      return { type: 'chatter', round: f.num('round'), text: f.str('text') }
    case 'lookup':
      return {
        type: 'lookup',
        round: f.num('round'),
        seq: f.num('seq'),
        at_ms: f.num('at_ms'),
        text: f.str('text'),
        skipped: f.oneOf('skipped', SKIPPED),
      }
    case 'lookup_done':
      // cut is left out of the frame when false.
      return {
        type: 'lookup_done',
        round: f.num('round'),
        seq: f.num('seq'),
        at_ms: f.num('at_ms'),
        error: f.str('error'),
        cut: parsed.cut === undefined ? false : f.bool('cut'),
      }
    case 'answer_start':
      return { type: 'answer_start', round: f.num('round'), at_ms: f.num('at_ms') }
    case 'delta':
      return { type: 'delta', text: f.str('text') }
    case 'answer_withdrawn':
      return { type: 'answer_withdrawn', round: f.num('round'), at_ms: f.num('at_ms'), reason: f.oneOf('reason', WITHDRAWN_REASONS) }
    case 'draft_sent_back': {
      const { triggers } = parsed
      if (!Array.isArray(triggers) || !triggers.every((t) => typeof t === 'string')) {
        throw new MateEventError(name, 'triggers is not a list of strings')
      }
      return {
        type: 'draft_sent_back',
        round: f.num('round'),
        at_ms: f.num('at_ms'),
        triggers: triggers as string[],
        draft: f.str('draft'),
      }
    }
    case 'round_end':
      return {
        type: 'round_end',
        round: f.num('round'),
        at_ms: f.num('at_ms'),
        reasoning: f.oneOf('reasoning', REASONING_KINDS),
        reasoning_bytes: f.num('reasoning_bytes'),
        prompt_tokens: f.num('prompt_tokens'),
        completion_tokens: f.num('completion_tokens'),
        cached_tokens: f.num('cached_tokens'),
        cost_usd: f.num('cost_usd'),
      }
    case 'message': {
      // Top level only: the shape inside a message or conversation stays
      // with the mappers that read them.
      if (!hasStringId(parsed.message)) throw new MateEventError(name, 'message is not an object with a string id')
      if (!hasStringId(parsed.conversation)) throw new MateEventError(name, 'conversation is not an object with a string id')
      const event: MateEvent = {
        type: 'message',
        message: parsed.message as unknown as MessageApi,
        conversation: parsed.conversation as unknown as ConversationApi,
      }
      if (parsed.run !== undefined) event.run = decodeRunRef(name, parsed.run)
      return event
    }
    case 'error': {
      const event: MateEvent = { type: 'error', error: f.str('error') }
      if (parsed.run_id !== undefined) event.run_id = f.str('run_id')
      return event
    }
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

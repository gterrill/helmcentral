/**
 * Cards under a Mate reply (ADR 0191). Each kind is one member of this union,
 * keyed by `kind`. `error` is not a server kind: it is what the decoder makes
 * of an entry it cannot read, so the thread can say so for that message.
 */
export type MateCard = { kind: 'error'; reason: string; rawKind?: string }

/**
 * Decode the `cards` field of a stored message. An absent field is no cards.
 * An entry this build cannot read (no kind, or a kind it does not know) becomes
 * an `error` entry in its place, so one bad card never blanks the thread and is
 * never silently dropped. A non-array value yields a single `error` entry.
 */
export function decodeMateCards(raw: unknown): MateCard[] {
  if (raw === undefined) return []
  if (!Array.isArray(raw)) return [{ kind: 'error', reason: 'message cards: expected an array' }]
  return raw.map((entry, i): MateCard => {
    const kind = typeof entry === 'object' && entry !== null ? (entry as { kind?: unknown }).kind : undefined
    if (typeof kind !== 'string') return { kind: 'error', reason: `message card ${i}: missing kind` }
    return { kind: 'error', reason: `message card ${i}: unknown kind "${kind}"`, rawKind: kind }
  })
}

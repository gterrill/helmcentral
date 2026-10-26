import { decodePassageDecision, oneOf, STATES, type PassageDecision, type PassageState } from '@/lib/mate-cards'

// What GET /api/passages/:id says beyond the track: the operator's rating and
// the Mate plans the passage was sailed from. Read strictly, but a shape this
// build cannot read never fails the passage: the part that could not be read
// becomes a plain sentence for the page, and the detail goes to the console.

export const PASSAGE_RATINGS = ['comfortable', 'lively', 'shouldnt_have_gone'] as const
export type PassageRating = '' | (typeof PASSAGE_RATINGS)[number]

export const PLANS_UNREADABLE = "Couldn't read the Mate plans for this passage."
export const RATING_UNREADABLE = "Couldn't read how this passage went."

export interface PassagePlan {
  decision: PassageDecision
  /** Every departure day on the card, in order. */
  rowDates: string[]
  /** The verdict of the departure day that was sailed; null when no day is chosen. */
  rowVerdict: PassageState | null
  conversationId: string
  /** False once the chat that made the plan has been deleted. */
  chatExists: boolean
}

export interface PassageDetailExtras {
  rating: PassageRating
  /** A plain sentence when the stored rating could not be read; `rating` is then blank. */
  ratingError: string | null
  /** Null when the plans could not be read; `plannedWithMateError` says so. */
  plannedWithMate: PassagePlan[] | null
  plannedWithMateError: string | null
}

function bad(path: string, why: string): never {
  throw new Error(`passage ${path}: ${why}`)
}

function obj(v: unknown, path: string): Record<string, unknown> {
  if (typeof v !== 'object' || v === null || Array.isArray(v)) bad(path, 'expected an object')
  return v as Record<string, unknown>
}

function decodePlan(raw: unknown, i: number): PassagePlan {
  const path = `planned_with_mate[${i}]`
  const o = obj(raw, path)
  if (!Array.isArray(o.row_dates) || !o.row_dates.every((d) => typeof d === 'string' && /^\d{4}-\d{2}-\d{2}$/.test(d))) {
    bad(`${path}.row_dates`, 'expected a list of YYYY-MM-DD dates')
  }
  const verdict = o.row_verdict === null ? null : oneOf<PassageState>(o.row_verdict, STATES, `${path}.row_verdict`)
  if (typeof o.conversation_id !== 'string') bad(`${path}.conversation_id`, 'expected a string')
  if (typeof o.chat_exists !== 'boolean') bad(`${path}.chat_exists`, 'expected true or false')
  return {
    decision: decodePassageDecision(o.decision, `${path}.decision`),
    rowDates: o.row_dates as string[],
    rowVerdict: verdict,
    conversationId: o.conversation_id as string,
    chatExists: o.chat_exists as boolean,
  }
}

function decodePlans(o: Record<string, unknown>): Pick<PassageDetailExtras, 'plannedWithMate' | 'plannedWithMateError'> {
  const error = o.planned_with_mate_error
  if (error !== undefined) {
    // The server could not read the plans; its log has the cause.
    console.error(`passage planned_with_mate_error: ${String(error)}`)
    return { plannedWithMate: null, plannedWithMateError: PLANS_UNREADABLE }
  }
  const plans = o.planned_with_mate
  if (!Array.isArray(plans)) bad('planned_with_mate', 'expected an array')
  return { plannedWithMate: (plans as unknown[]).map(decodePlan), plannedWithMateError: null }
}

/** Reads the rating and the plans. Never throws: each part that fails becomes its own explicit error. */
export function decodePassageDetailExtras(raw: unknown): PassageDetailExtras {
  const o = obj(raw, 'detail')
  let rating: PassageRating = ''
  let ratingError: string | null = null
  if (o.rating !== '' && !PASSAGE_RATINGS.includes(o.rating as never)) {
    console.error(`passage rating: expected one of ${PASSAGE_RATINGS.join(', ')} or blank, got ${JSON.stringify(o.rating)}`)
    ratingError = RATING_UNREADABLE
  } else {
    rating = o.rating as PassageRating
  }
  let plans: Pick<PassageDetailExtras, 'plannedWithMate' | 'plannedWithMateError'>
  try {
    plans = decodePlans(o)
  } catch (err) {
    console.error(err)
    plans = { plannedWithMate: null, plannedWithMateError: PLANS_UNREADABLE }
  }
  return { rating, ratingError, ...plans }
}

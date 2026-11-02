// The lengths a replay can be set to: how long the whole timeline takes to
// sail at full pace, in seconds. A single passage offers four; a cruise, or
// passages selected together, has more to get through and offers longer ones
// as well. Each starts on its own length.

export interface ReplayLength {
  id: string
  label: string
  seconds: number
}

const length = <const Id extends string>(id: Id, label: string, seconds: number) => ({ id, label, seconds }) as const

const L180 = length('180s', '3 min', 180)
const L120 = length('120s', '2 min', 120)
const L60 = length('60s', '60 s', 60)
const L30 = length('30s', '30 s', 30)
const L15 = length('15s', '15 s', 15)
const L8 = length('8s', '8 s', 8)

/** On offer for a single passage. */
export const PASSAGE_REPLAY_LENGTHS = [L60, L30, L15, L8] as const
/** On offer for a cruise or a selection of passages. */
export const CRUISE_REPLAY_LENGTHS = [L180, L120, L60, L30, L15, L8] as const

export type ReplaySpeedId = (typeof CRUISE_REPLAY_LENGTHS)[number]['id']

/** Where each starts: a single passage on 15 s, a cruise on a minute. */
export const DEFAULT_REPLAY_LENGTH: ReplaySpeedId = L15.id
export const DEFAULT_CRUISE_REPLAY_LENGTH: ReplaySpeedId = L60.id

export const replaySeconds = (id: ReplaySpeedId): number => CRUISE_REPLAY_LENGTHS.find((l) => l.id === id)?.seconds ?? L15.seconds

// What the Mate thread shows while it is waiting on the model and no tool is
// running. The server sends a bare "waiting" status; the phrase is chosen
// here and rotates so a long wait does not sit on one line.

export const MATE_WAITING_PHRASES: readonly string[] = [
  'Holding fast…',
  'Bearing down…',
  'Coming about…',
  'Careening…',
  'Tacking…',
  'Jibing…',
  'Battening down the hatches…',
  'Clearing the decks…',
  'In the offing…',
  'Jury rigging…',
  'Letting the cat out of the bag…',
  'Ships passing in the night…',
  'In it for the long haul…',
  'Making up leeway…',
  'Swinging the cat…',
  'Pressing into service…',
  'Running a tight ship…',
  'Sailing close to the wind…',
  'Shivering me timbers…',
  'Smooth sailing…',
  'Getting squared away…',
  'Trimming the sails…',
  'Going overboard…',
  'Learning the ropes…',
  'Firing a shot across the bow…',
]

export const MATE_WAITING_ROTATE_MS = 3000

/**
 * Picks a waiting phrase at random, never the one passed as `previous`.
 * `random` is injectable (returns [0, 1)) so tests are deterministic.
 */
export function pickWaitingPhrase(previous: string | null, random: () => number = Math.random): string {
  const pool = previous === null ? MATE_WAITING_PHRASES : MATE_WAITING_PHRASES.filter((phrase) => phrase !== previous)
  const index = Math.min(Math.floor(random() * pool.length), pool.length - 1)
  return pool[index]
}

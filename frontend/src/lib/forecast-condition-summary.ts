// The ten-day strip shows ten cards, but the panel header above them used to
// say nothing about what those ten days add up to. This builds that one
// sentence from the run-length shape of days[].condition - not a single
// day's reading and not every day spelled out, but the shape a person would
// say out loud: "Rain expected the next 3 days."
//
// Condition labels come from backend/main.go's formatWeatherConditionAt, so
// the three named buckets below are keyed to that exact vocabulary. Anything
// outside it (Breezy, Windy, Unknown, an unrecognised code) falls into
// "other" and is treated as non-wet.
//
// The sentence names only the first run ("Drizzle expected the next 4
// days."); later runs are left to the day cards. A trough day inside that
// first run rides along in its clause, since a trough buried in day six of a
// "for all 10 days" run reads nothing like a trough on today alone. Troughs
// on later days are not mentioned.

export interface ConditionSummaryDay {
  condition: string
  /** Short day label for a trough mention, e.g. "Sat 20". Unused unless trough is true. */
  label: string
  /** Whether upper air supports a surface low on this day. */
  trough: boolean
}

type Bucket = 'wet' | 'cloudy' | 'fair' | 'other'

// Anything containing one of these words is wet, which also catches the
// compounds the exact-match sets below don't need to list separately
// (freezing rain, mixed rain & snow, heavy snow, ...).
const WET_WORDS = ['drizzle', 'rain', 'snow', 'sleet', 'hail', 'thunderstorm', 'blizzard']

const CLOUDY_CONDITIONS = new Set(['cloudy', 'mostly cloudy', 'foggy', 'hazy', 'dusty', 'smoky'])

const FAIR_CONDITIONS = new Set(['clear', 'mostly sunny', 'mostly clear', 'sunny', 'partly cloudy'])

function bucketFor(condition: string): Bucket {
  const normalized = condition.trim().toLowerCase()
  if (WET_WORDS.some((word) => normalized.includes(word))) return 'wet'
  if (CLOUDY_CONDITIONS.has(normalized)) return 'cloudy'
  if (FAIR_CONDITIONS.has(normalized)) return 'fair'
  return 'other'
}

interface Run {
  bucket: Bucket
  days: ConditionSummaryDay[]
}

// The days from the first one up to the first change of bucket.
function firstRun(days: ConditionSummaryDay[]): Run {
  const bucket = bucketFor(days[0].condition)
  const end = days.findIndex((day) => bucketFor(day.condition) !== bucket)
  return { bucket, days: end === -1 ? days : days.slice(0, end) }
}

// The most frequent condition within the run, in its own original casing.
// Ties go to whichever condition occurred earliest in the run - tracked by
// only replacing the leader on a strictly higher count, never an equal one.
function labelForRun(run: Run): string {
  const counts = new Map<string, number>()
  let best = run.days[0].condition
  let bestCount = 0
  for (const day of run.days) {
    const count = (counts.get(day.condition) ?? 0) + 1
    counts.set(day.condition, count)
    if (count > bestCount) {
      bestCount = count
      best = day.condition
    }
  }
  return best
}

function capitalize(label: string): string {
  return label.length === 0 ? label : label.charAt(0).toUpperCase() + label.slice(1)
}

// The suffix for a clause that names days by their short label: " with a
// trough aloft on Sat 20", or joined for several ("Sat 20 and Mon 22", three
// or more "A, B and C"). Empty when nothing in the given days is flagged.
function namedTroughSuffix(days: ConditionSummaryDay[]): string {
  const flagged = days.filter((day) => day.trough)
  if (flagged.length === 0) return ''
  const labels = flagged.map((day) => day.label)
  const named =
    labels.length === 1 ? labels[0] : `${labels.slice(0, -1).join(', ')} and ${labels[labels.length - 1]}`
  const word = labels.length === 1 ? 'a trough' : 'troughs'
  return ` with ${word} aloft on ${named}`
}

// Builds the one-sentence conditions summary for the 10-Day Forecast panel's
// intro. days[0] is today. Only the first run is named; the day cards carry
// the rest. The sentence names the condition directly, so it reads the same
// for every bucket - only whether "expected" appears (wet) changes. A run
// covering every day says "for all n days", the claim that matches nothing
// else being left to describe. A one-day run is today by construction, so a
// trough on it is just "with a trough aloft", not "on <label>" again.
// Returns null for no days - null renders nothing rather than an empty
// paragraph.
export function buildConditionsSummary(days: ConditionSummaryDay[]): string | null {
  if (days.length === 0) return null

  const run = firstRun(days)
  const label = capitalize(labelForRun(run).toLowerCase())
  const n = run.days.length
  if (n === 1) return `${label} today${run.days[0].trough ? ' with a trough aloft' : ''}.`
  const span = n === days.length ? `for all ${n} days` : `the next ${n} days`
  const verb = run.bucket === 'wet' ? ' expected' : ''
  return `${label}${verb} ${span}${namedTroughSuffix(run.days)}.`
}

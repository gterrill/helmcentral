// The ten-day strip shows ten cards, but the panel header above them used to
// say nothing about what those ten days add up to. This builds that one
// sentence from the run-length shape of days[].condition - not a single
// day's reading and not every day spelled out, but the shape a person would
// say out loud: "rain today, then clearing."
//
// Condition labels come from backend/main.go's formatWeatherConditionAt, so
// the three named buckets below are keyed to that exact vocabulary. Anything
// outside it (Breezy, Windy, Unknown, an unrecognised code) falls into
// "other" and is treated as non-wet.
//
// Trough days ride along in the same sentence. The day card used to carry
// its own coloured trough tint, but that's gone now, so the mention has to
// live somewhere - and it belongs per-clause, not bolted onto the end,
// because a trough buried in day six of a "for all 10 days" run reads
// nothing like a trough on the one day of a "followed by" clause.

export interface ConditionSummaryDay {
  condition: string
  dayName: string
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

// Consecutive days sharing a bucket collapse into one run, in day order.
function groupRuns(days: ConditionSummaryDay[]): Run[] {
  const runs: Run[] = []
  for (const day of days) {
    const bucket = bucketFor(day.condition)
    const current = runs[runs.length - 1]
    if (current && current.bucket === bucket) {
      current.days.push(day)
    } else {
      runs.push({ bucket, days: [day] })
    }
  }
  return runs
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

// The suffix for the "<Label> today" form specifically - the one-day first
// clause, whether that's the whole sentence (singleRunClause) or just its
// opening (firstClause). That single day is today by construction, so
// naming it again with "on <label>" would only repeat what "today" already
// said - this drops straight to "with a trough aloft".
function todayClauseTroughSuffix(run: Run): string {
  return run.days[0].trough ? ' with a trough aloft' : ''
}

// Wet labels read as nouns ("a day of rain", "2 days of rain"); everything
// else reads as an adjective ("a mostly sunny day", "2 mostly sunny days").
// This is the phrasing for a run that isn't the sentence's first clause, so
// the label is always lowercase. It is never the "today" clause, so any
// trough day in the run gets named in full rather than folded away.
function clausePhrase(run: Run): string {
  const label = labelForRun(run).toLowerCase()
  const n = run.days.length
  const isWet = run.bucket === 'wet'
  const phrase = n === 1 ? (isWet ? `a day of ${label}` : `a ${label} day`) : isWet ? `${n} days of ${label}` : `${n} ${label} days`
  return `${phrase}${namedTroughSuffix(run.days)}`
}

// The opening clause names the condition directly rather than turning it
// into a noun or adjective phrase, so it reads the same for every bucket -
// only whether "expected" appears (wet) changes.
function firstClause(run: Run): string {
  const label = capitalize(labelForRun(run).toLowerCase())
  const n = run.days.length
  if (n === 1) return `${label} today${todayClauseTroughSuffix(run)}`
  const isWet = run.bucket === 'wet'
  const clause = isWet ? `${label} expected the next ${n} days` : `${label} the next ${n} days`
  return `${clause}${namedTroughSuffix(run.days)}`
}

// A single run spanning the whole window gets its own phrasing rather than
// reusing firstClause's "the next n days" - "for all n days" is the claim
// that actually matches nothing else being left to describe.
function singleRunClause(run: Run): string {
  const label = capitalize(labelForRun(run).toLowerCase())
  const n = run.days.length
  if (n === 1) return `${label} today${todayClauseTroughSuffix(run)}.`
  const isWet = run.bucket === 'wet'
  const clause = isWet ? `${label} expected for all ${n} days` : `${label} for all ${n} days`
  return `${clause}${namedTroughSuffix(run.days)}.`
}

// Builds the one-sentence conditions summary for the 10-Day Forecast panel's
// intro. days[0] is today. Returns null for an empty run - there is nothing
// to summarise, and null renders nothing rather than an empty paragraph.
export function buildConditionsSummary(days: ConditionSummaryDay[]): string | null {
  if (days.length === 0) return null

  const runs = groupRuns(days)
  if (runs.length === 1) return singleRunClause(runs[0])

  let sentence = firstClause(runs[0])
  sentence += `, followed by ${clausePhrase(runs[1])}`

  if (runs.length === 3) {
    sentence += `, then ${clausePhrase(runs[2])}`
  } else if (runs.length > 3) {
    // Naming just the third run when a fourth or fifth is waiting behind it
    // would be a stopped clock - hard-coded, not necessarily reached. The
    // window has stopped being three tidy chunks by then anyway, so this
    // says so instead of picking one more run to name.
    //
    // A trough anywhere from the third run on falls in this same collapsed
    // clause, since that's the only mention its run gets.
    const tailDays = runs.slice(2).flatMap((run) => run.days)
    sentence += `, then changeable through ${days[days.length - 1].dayName}${namedTroughSuffix(tailDays)}`
  }

  return `${sentence}.`
}

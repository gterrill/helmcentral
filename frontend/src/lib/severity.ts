/**
 * Severity colours (PFD fix: warn/alert used to be amber-500 vs amber-400, a
 * one-shade near-miss pair indistinguishable at arm's length). Each of the
 * four live states sits on its own hue, with emergency filled rather than
 * merely coloured in severityClass, since it is the one state SignalK will
 * not let the operator silence or acknowledge away.
 *
 * Two ladders, one source: severityClass governs alarm TEXT (the alarms
 * drawer and status chips). severityFill and severityTextClass govern gauge
 * ZONES - the same five components (dial-ring, gauge-tile, lamp-strip,
 * engine-cluster, cluster-readings) that used to each keep their own copy of
 * this switch, with the same near-miss colours.
 */
import { ALARM_STATES, type AlarmState } from '@/hooks/use-alarms'

export function severityClass(state: AlarmState | string): string {
  switch (state) {
    case 'alert':
      return 'text-sky-700'
    case 'warn':
      return 'text-amber-600'
    case 'alarm':
      return 'text-red-600'
    case 'emergency':
      return 'bg-red-700 text-white rounded-xs px-1'
    default:
      return 'text-muted-foreground'
  }
}

/**
 * The gauge-zone ladder: one ring/fill colour per severity, for SVG arcs and
 * fills (dial-ring, gauge-tile, lamp-strip). Same near-miss problem as
 * severityClass above - warn and alert were a shade apart on the same hue -
 * plus emergency and alarm were only 9 points of lightness apart on the same
 * red, which read as one colour at a glance. alert moves to a distinct blue
 * hue and emergency drops to a visibly deeper red than alarm.
 *
 * `normal` is an explicit case, not the default: a gauge zone can legitimately
 * be `normal`, and that should always paint the same green regardless of what
 * a caller does with an unrecognised value.
 */
export function severityFill(state: AlarmState | string | null): string {
  switch (state) {
    case 'alert':
      return 'hsl(199 89% 48%)'
    case 'warn':
      return 'hsl(38 92% 50%)'
    case 'alarm':
      return 'hsl(0 72% 51%)'
    case 'emergency':
      return 'hsl(0 74% 32%)'
    case 'normal':
      return 'hsl(142 71% 45%)'
    default:
      return 'hsl(var(--muted-foreground))'
  }
}

/**
 * The gauge-zone ladder's text form, for readouts that colour a number rather
 * than a chip (unlike severityClass's emergency, which fills). `outside` is
 * cluster-readings' vocabulary for a reading past every configured band: an
 * out-of-range value with no matching zone is a warning-grade condition, not
 * a confirmed alarm, so it reads as warn's colour.
 *
 * `normal`, null and anything else a caller was not expecting fall through to
 * `fallback`, since each gauge component has its own idea of what a healthy
 * or bandless reading should look like (a themed primary colour, a muted
 * grey, an emerald green).
 */
export function severityTextClass(state: AlarmState | string | null, fallback: string): string {
  switch (state) {
    case 'alert':
      return 'text-sky-700'
    case 'warn':
      return 'text-amber-600'
    case 'alarm':
      return 'text-red-600'
    case 'emergency':
      return 'text-red-800'
    case 'outside':
      return 'text-amber-600'
    default:
      return fallback
  }
}

/**
 * A tile's own vocabulary (ADR 0081): every AlarmState, plus `outside` —
 * cluster-readings.ts's "past every configured band" case, which a tile has
 * to be able to carry up to its edge exactly like an alarm state can.
 */
export type ZoneState = AlarmState | 'outside'

/**
 * `outside` shares warn's rung: an out-of-range reading is warning-grade, not
 * a confirmed alarm, the same call severityTextClass already makes.
 *
 * Ranked with `indexOf` at call time rather than a rank table built at module
 * load: severity.ts is a leaf utility that most of the dashboard imports
 * (directly or through Tile), and a table built eagerly from ALARM_STATES
 * would make every one of those importers depend on `@/hooks/use-alarms`
 * being fully present the moment severity.ts loads, including in a test that
 * mocks that hook down to only what it calls.
 */
function rankOf(state: ZoneState): number {
  return ALARM_STATES.indexOf(state === 'outside' ? 'warn' : state)
}

/**
 * The worst of a tile's readings, for the border colour on its edge.
 * `null`/`undefined` entries are readings with nothing to report and are
 * ignored rather than counted as normal; the result is `null` only when
 * every entry is like that. When the worst rung is the warn/outside tie,
 * a literal `warn` wins it — `outside` surfaces only when nothing on the
 * tile has actually crossed into a named band above it.
 */
export function worstZoneState(states: Array<ZoneState | null | undefined>): ZoneState | null {
  const present = states.filter((state): state is ZoneState => state !== null && state !== undefined)
  if (present.length === 0) return null

  const maxRank = Math.max(...present.map(rankOf))
  if (maxRank === rankOf('warn') && present.includes('warn')) return 'warn'

  return present.find((state) => rankOf(state) === maxRank)!
}

/**
 * The tile-edge border ladder (ADR 0081). Null and `normal` draw no border,
 * on the same rationed-colour principle as the rest of the board: colour is
 * spent only once a tile actually has something to say.
 */
export function severityBorderClass(state: ZoneState | null): string {
  switch (state) {
    case 'alert':
      return 'border-sky-500'
    case 'warn':
    case 'outside':
      return 'border-amber-500 dark:border-amber-400'
    case 'alarm':
      return 'border-red-500 dark:border-red-400'
    case 'emergency':
      return 'border-red-700 bg-red-500/10 dark:border-red-500'
    default:
      return ''
  }
}

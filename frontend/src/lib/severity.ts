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
import type { AlarmState } from '@/hooks/use-alarms'

export function severityClass(state: AlarmState | string): string {
  switch (state) {
    case 'alert':
      return 'text-sky-700'
    case 'warn':
      return 'text-amber-600'
    case 'alarm':
      return 'text-red-600'
    case 'emergency':
      return 'bg-red-700 text-white rounded-sm px-1'
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

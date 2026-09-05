/**
 * Severity colours for alarm text (PFD fix: warn/alert used to be amber-500
 * vs amber-400, a one-shade near-miss pair indistinguishable at arm's length).
 * Each of the four live states now sits on its own hue, with emergency
 * filled rather than merely coloured, since it is the one state SignalK
 * will not let the operator silence or acknowledge away.
 *
 * This governs alarm TEXT only. gauge-tile, dial-ring, lamp-strip,
 * engine-cluster and cluster-readings colour gauge zones, a different
 * concern, and are untouched here.
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

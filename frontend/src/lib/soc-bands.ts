import type { AlarmRule } from '@/hooks/use-alarm-rules'

/**
 * Mirrors the backend's INFLUX_SOC_MEASUREMENT default (see the dawn
 * projection phase of the battery tile plan). A later phase reads the
 * pinned path from the backend rather than assuming this one, so every
 * function below takes socPath as a parameter and this constant is only
 * the value used until that wiring exists.
 */
export const DEFAULT_SOC_PATH = 'electrical.batteries.0.capacity.stateOfCharge'

/** State-of-charge alarm bands, in percent (0-100), or null when unset. */
export interface SocBands {
  warnBelow: number | null
  alarmBelow: number | null
}

// A rule's threshold is stored in the path's native SignalK unit. SoC on the
// bus is a 0..1 ratio, but an operator typing a rule by hand may reasonably
// enter "20" meaning 20%, so anything already over 1 is treated as already
// being a percent rather than a ratio that would produce an absurd band.
function toPercent(value: number): number {
  return value <= 1 ? value * 100 : value
}

/**
 * Derives the SoC bands from the alarm rules the operator has configured,
 * rather than a hardcoded constant: no rule means no band, and the tile
 * falls back to a plain, uncoloured reading (see AGENTS.md fallback policy).
 */
export function socBandsFromRules(rules: readonly AlarmRule[], socPath: string): SocBands {
  let warnBelow: number | null = null
  let alarmBelow: number | null = null

  for (const rule of rules) {
    if (!rule.enabled || rule.path !== socPath || rule.op !== 'below') continue

    const percent = toPercent(rule.value)
    if (rule.state === 'warn') {
      warnBelow = warnBelow === null ? percent : Math.max(warnBelow, percent)
    } else if (rule.state === 'alarm' || rule.state === 'emergency') {
      alarmBelow = alarmBelow === null ? percent : Math.max(alarmBelow, percent)
    }
  }

  return { warnBelow, alarmBelow }
}

/** Which band, if any, the current SoC reading has crossed. */
export function socSeverity(socPercent: number | null, bands: SocBands): 'alarm' | 'warn' | null {
  if (socPercent === null) return null
  if (bands.alarmBelow !== null && socPercent < bands.alarmBelow) return 'alarm'
  if (bands.warnBelow !== null && socPercent < bands.warnBelow) return 'warn'
  return null
}

export interface BandTarget {
  targetPercent: number
  hours: number
}

/**
 * The next band below the current SoC while discharging, and how long at
 * the live rate until it's reached. Returns null when charging (bands only
 * describe how far there is to fall), when the rate is unknown, or when
 * there is no lower band left to cross - which means "to empty" rather than
 * "to a threshold", and is the caller's job to label that way.
 */
export function hoursToBand(
  socPercent: number | null,
  ratePercentPerHour: number | null,
  bands: SocBands,
): BandTarget | null {
  if (socPercent === null || ratePercentPerHour === null || ratePercentPerHour >= 0) {
    return null
  }

  const targetPercent = bands.warnBelow !== null && socPercent > bands.warnBelow
    ? bands.warnBelow
    : bands.alarmBelow !== null && socPercent > bands.alarmBelow
      ? bands.alarmBelow
      : null

  if (targetPercent === null) return null

  const hours = (socPercent - targetPercent) / -ratePercentPerHour
  return { targetPercent, hours }
}

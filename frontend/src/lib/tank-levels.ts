/**
 * Tank levels (ADR 0214): the warn and alarm percentages that colour the Tanks
 * tile and the Engine Cluster's fuel bars and raise the tank alarms. The
 * server resolves them per tank (kind default, then the tank's own figures)
 * and sends them with each reading; nothing here holds a number of its own.
 */
import type { GaugeZone } from '@/lib/dashboard-widgets'

export type TankKind = 'water' | 'fuel' | 'waste'
export type TankTone = 'critical' | 'warn' | 'normal'

export interface TankLevels {
  warn_percent: number
  alarm_percent: number
}

/** The kind's default pair, as Settings edits it. */
export interface TankLevelPair {
  warn: number
  alarm: number
}

/** Waste inverts the rest of the fleet: a FULL tank is the bad one. */
export function isLowBadKind(kind: TankKind): boolean {
  return kind !== 'waste'
}

/** Past the level on the bad side, strictly, the same test the alarm engine applies. */
export function tankTone(kind: TankKind, percent: number, levels: TankLevels): TankTone {
  if (isLowBadKind(kind)) {
    if (percent < levels.alarm_percent) return 'critical'
    if (percent < levels.warn_percent) return 'warn'
    return 'normal'
  }
  if (percent > levels.alarm_percent) return 'critical'
  if (percent > levels.warn_percent) return 'warn'
  return 'normal'
}

/** The fuel bar's bands in percent of full: the alarm band at the foot, the warn band above it. */
export function fuelBarZones(kind: TankKind, levels: TankLevels): GaugeZone[] {
  if (isLowBadKind(kind)) {
    return [
      { from: 0, to: levels.alarm_percent, state: 'alarm' },
      { from: levels.alarm_percent, to: levels.warn_percent, state: 'warn' },
    ]
  }
  return [
    { from: levels.warn_percent, to: levels.alarm_percent, state: 'warn' },
    { from: levels.alarm_percent, to: 100, state: 'alarm' },
  ]
}

/**
 * Why a pair of levels cannot be saved, or null. A blank (null) takes the
 * kind's default, so the order is checked on what the tank would really use.
 * The server checks the same.
 */
export function tankLevelsError(
  kind: TankKind,
  warn: number | null,
  alarm: number | null,
  defaults: TankLevelPair,
): string | null {
  for (const v of [warn, alarm]) {
    if (v !== null && (!Number.isFinite(v) || v < 0 || v > 100)) {
      return 'Levels are percentages between 0 and 100.'
    }
  }
  const effectiveWarn = warn ?? defaults.warn
  const effectiveAlarm = alarm ?? defaults.alarm
  if (isLowBadKind(kind) && effectiveAlarm >= effectiveWarn) {
    return 'The alarm level must be lower than the warn level, because a low tank is the bad one.'
  }
  if (!isLowBadKind(kind) && effectiveAlarm <= effectiveWarn) {
    return 'The alarm level must be higher than the warn level, because a full tank is the bad one.'
  }
  return null
}

/** The card footer for a tank alarm: where the level has to get back to. `clear` is the server's clear_value, a ratio. */
export function tankClearText(op: 'above' | 'below', clear: number): string {
  const percent = Math.round(clear * 100)
  return `Clears ${op === 'below' ? 'above' : 'below'} ${percent}%`
}

/** Per-kind defaults as Settings edits them (ADR 0214). */
export interface TankLevelDefaults {
  fuel: TankLevelPair
  water: TankLevelPair
  waste: TankLevelPair
}

export const TANK_DEFAULT_KINDS: { kind: TankKind; label: string }[] = [
  { kind: 'fuel', label: 'Fuel' },
  { kind: 'water', label: 'Fresh water' },
  { kind: 'waste', label: 'Waste' },
]

/** A typed percentage: blank is null, anything that is not a number is NaN so the check below names it. */
export function parseLevelText(text: string): number | null {
  const trimmed = text.trim()
  if (trimmed === '') return null
  return Number(trimmed)
}

/** Why a default pair, as typed, cannot be saved. Both figures are required here: a default has nothing to fall back to. */
export function tankDefaultsError(kind: TankKind, warnText: string, alarmText: string): string | null {
  const warn = parseLevelText(warnText)
  const alarm = parseLevelText(alarmText)
  if (warn === null || alarm === null) return 'Enter both a warn and an alarm level.'
  return tankLevelsError(kind, warn, alarm, { warn, alarm })
}

/**
 * The kind of tank a SignalK source is, from its category: tanks.fuel.0 is
 * fuel, tanks.blackWater.1 is waste, everything else is water. The same
 * mapping the server applies (tankKindFromCategory). Null for a source that
 * is not a tank.
 */
export function tankKindOfSource(source: string): TankKind | null {
  const match = /^tanks\.([A-Za-z0-9_-]+)\.[A-Za-z0-9_-]+$/.exec(source)
  if (!match) return null
  const category = match[1].toLowerCase()
  if (category.includes('fuel')) return 'fuel'
  if (['black', 'grey', 'waste', 'sewage', 'holding'].some((word) => category.includes(word))) return 'waste'
  return 'water'
}

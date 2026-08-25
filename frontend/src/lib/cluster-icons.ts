import { Activity, Clock, Cog, Fuel, Gauge, Rabbit, Thermometer, Waves, Zap, type LucideIcon } from 'lucide-react'

/**
 * Icons for engine cluster boxes (ADR 0054).
 *
 * A small closed set rather than "any lucide name": the value is persisted and
 * validated server-side, and an allowlist means a saved config can never name
 * a component that does not exist.
 */
export const CLUSTER_ICONS: Record<string, LucideIcon> = {
  thermometer: Thermometer,
  cog: Cog,
  rabbit: Rabbit,
  clock: Clock,
  fuel: Fuel,
  gauge: Gauge,
  zap: Zap,
  waves: Waves,
  activity: Activity,
}

export const CLUSTER_ICON_NAMES = Object.keys(CLUSTER_ICONS)

export type ClusterIconName = string

/**
 * Path hints, checked in order. Order is the whole design here:
 * `transmission.oilTemperature` carries both "oil" and "temperature", and it
 * is a temperature, so temperature has to be tested first.
 */
const PATH_HINTS: [RegExp, string][] = [
  [/temperature|temp\b/, 'thermometer'],
  [/boost|manifold/, 'rabbit'],
  [/oil|lubric/, 'cog'],
  [/runtime|hours|runhours/, 'clock'],
  [/fuel|consumption/, 'fuel'],
  [/revolution|rpm|speed/, 'gauge'],
  [/volt|current|charge|alternator/, 'zap'],
  [/level|tank/, 'waves'],
  [/load|torque/, 'activity'],
]

/** Quantity is the fallback when the path name says nothing recognisable. */
const QUANTITY_ICONS: Record<string, string> = {
  temperature: 'thermometer',
  pressure: 'cog',
  duration: 'clock',
  volumetricFlow: 'fuel',
  frequency: 'gauge',
  volume: 'waves',
  ratio: 'activity',
}

/** The icon a slot gets when the operator has not chosen one. */
export function iconForSlot(slot: { path: string; quantity: string }): ClusterIconName {
  const path = slot.path.toLowerCase().replace(/_/g, '')

  for (const [pattern, icon] of PATH_HINTS) {
    if (pattern.test(path)) return icon
  }
  return QUANTITY_ICONS[slot.quantity] ?? 'gauge'
}

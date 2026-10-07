import {
  Anchor, BatteryMedium, CircleDot, Cog, Droplets, Flame, GlassWater, Lightbulb, Navigation,
  Plug, Snowflake, Thermometer, Waves, Zap, type LucideIcon,
} from 'lucide-react'

/**
 * Icons for the indicator ribbon's cells. A closed set, like CLUSTER_ICONS:
 * the name is persisted and validated server-side (lampIconNames in
 * backend/dashboard_pages.go must list the same names).
 */
export const LAMP_ICONS: Record<string, LucideIcon> = {
  engine: Cog,
  generator: Zap,
  plug: Plug,
  inverter: Zap,
  alternator: Zap,
  battery: BatteryMedium,
  fridge: Snowflake,
  thermometer: Thermometer,
  droplet: Droplets,
  watermaker: GlassWater,
  bilge: Waves,
  flame: Flame,
  lightbulb: Lightbulb,
  anchor: Anchor,
  navlight: Navigation,
  generic: CircleDot,
}

export const LAMP_ICON_NAMES = Object.keys(LAMP_ICONS)

/** Checked in order; the first match wins. */
const PATH_HINTS: [RegExp, string][] = [
  [/propulsion|engine/, 'engine'],
  [/generator/, 'generator'],
  [/inverter|acin|shore/, 'plug'],
  [/alternator/, 'alternator'],
  [/battery|batteries/, 'battery'],
  [/fridge|freezer|refriger/, 'fridge'],
  [/watermaker/, 'watermaker'],
  [/bilge/, 'bilge'],
  [/propane|lpg|gas/, 'flame'],
  [/temperature|heater/, 'thermometer'],
  [/pump|water|tank/, 'droplet'],
  [/anchor/, 'anchor'],
  [/navigation.*light|navlight/, 'navlight'],
  [/light|lamp/, 'lightbulb'],
]

/** The icon a lamp gets when the operator has not chosen one. */
export function lampIconName(lamp: { path: string; icon?: string }): string {
  if (lamp.icon && lamp.icon in LAMP_ICONS) return lamp.icon
  const path = lamp.path.toLowerCase()
  for (const [pattern, icon] of PATH_HINTS) {
    if (pattern.test(path)) return icon
  }
  return 'generic'
}

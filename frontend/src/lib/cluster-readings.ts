import type { GaugeWidgetConfig, GaugeZone } from '@/lib/dashboard-widgets'
import { convertFromSI, formatQuantity, unitOption } from '@/lib/quantities'

/**
 * Turning a bound path into something an instrument can draw (ADR 0054).
 *
 * Here rather than in engine-cluster-tile.tsx because the fuel rail needs the
 * same three, and a component the tile renders cannot import from the tile.
 * Lifted unchanged.
 */

/** One reading, converted and formatted, or the structural dash when absent. */
export function reading(slot: GaugeWidgetConfig, values: Record<string, number | null>) {
  const raw = values[slot.path] ?? null
  const converted = raw === null ? null : convertFromSI(raw, slot.quantity, slot.unit)
  return {
    text: formatQuantity(raw, slot.quantity, slot.unit, slot.decimals),
    unit: unitOption(slot.quantity, slot.unit).label,
    converted,
    zone: zoneFor(converted, slot.zones),
  }
}

/**
 * The band a reading falls in, or `outside` when bands exist and it is in none
 * of them. A value that has left its healthy range must not keep reading
 * normal — answering "is anything wrong" is the whole job here.
 */
export function zoneFor(
  value: number | null,
  zones: GaugeZone[] | undefined,
): GaugeZone['state'] | 'outside' | null {
  if (value === null || !zones || zones.length === 0) return null
  const hit = zones.find((zone) => value >= Math.min(zone.from, zone.to) && value <= Math.max(zone.from, zone.to))
  return hit ? hit.state : 'outside'
}

export function zoneTextClass(state: ReturnType<typeof zoneFor>): string {
  switch (state) {
    case 'emergency':
    case 'alarm':
      return 'text-red-500'
    case 'warn':
    case 'outside':
      return 'text-amber-500'
    case 'alert':
      return 'text-amber-400'
    default:
      return 'text-gauge-primary'
  }
}

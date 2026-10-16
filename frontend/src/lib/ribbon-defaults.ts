import type { TileItem } from '@/lib/equipment-tiles'

import { LAMP_LABEL_MAX_LENGTH, LAMP_STRIP_MAX_LAMPS, type LampConfig } from './dashboard-widgets'

/**
 * Default lamps for a fresh indicator ribbon (ADR 0085), built from the
 * equipment items that are mapped to a source (ADR 0187) rather than from
 * patterns over live paths. One lamp per engine, generator, charger, inverter
 * and alternator, labelled with the item's own name and reading its running
 * signal. A boat missing a generator or a second alternator simply gets a
 * shorter ribbon, and an item with no source gets no lamp: there is nothing to
 * read.
 */
interface RibbonKind {
  kind: TileItem['source_kind']
  /** The reading under the item's source that is non-zero while it runs. */
  suffix: string
  group: string
  icon: string
}

const RIBBON_KINDS: RibbonKind[] = [
  // Engines: non-zero while turning.
  { kind: 'engine', suffix: 'revolutions', group: 'Propulsion', icon: 'engine' },
  // Generator: 0 stopped, non-zero running.
  { kind: 'generator', suffix: 'stateNumber', group: 'Power', icon: 'generator' },
  // Shore power: current drawn through the charger's AC input.
  { kind: 'charger', suffix: 'acin.1.current', group: 'Power', icon: 'plug' },
  // Inverter: 1 when AC-in is present.
  { kind: 'inverter', suffix: 'acState.acIn1Available', group: 'Power', icon: 'plug' },
  // Alternators: 0 off, non-zero charging (bulk/absorption/float).
  { kind: 'alternator', suffix: 'chargingModeNumber', group: 'Power', icon: 'alternator' },
]

/** The readings a lamp can watch for each kind of equipment, offered in the lamp form. */
export const LAMP_READINGS: Partial<Record<NonNullable<TileItem['source_kind']>, string[]>> = Object.fromEntries(
  RIBBON_KINDS.map((k) => [k.kind, [k.suffix]]),
)

/** Port, then starboard, then the rest by name, so a twin-engine boat reads left to right. */
function sideRank(name: string): number {
  if (/\bport\b/i.test(name)) return 0
  if (/\b(starboard|stbd)\b/i.test(name)) return 1
  return 2
}

/**
 * Resolves the ribbon against the items this vessel has mapped. Never invents a
 * lamp: a kind with no mapped item contributes nothing. Capped at the strip's
 * own lamp limit.
 */
export function suggestRibbonLamps(items: readonly TileItem[]): LampConfig[] {
  const lamps: LampConfig[] = []
  for (const entry of RIBBON_KINDS) {
    const matching = items
      .filter((item) => item.source_kind === entry.kind && item.signalk_source !== '')
      .sort((a, b) => sideRank(a.name) - sideRank(b.name) || a.name.localeCompare(b.name))
    for (const item of matching) {
      lamps.push({
        path: '',
        equipmentId: item.id,
        suffix: entry.suffix,
        label: item.name.slice(0, LAMP_LABEL_MAX_LENGTH),
        group: entry.group,
        icon: entry.icon,
      })
    }
  }
  return lamps.slice(0, LAMP_STRIP_MAX_LAMPS)
}

import type { EngineProfile } from '@/lib/engine-profiles'
import type { TileItem } from '@/lib/equipment-tiles'
import type { SourceKind } from '@/lib/vessel-settings'

/**
 * Which inventory items could be a given kind of equipment, for the tile picker
 * and for Discovery's "Assign to item". An item with no source has no kind yet,
 * so the choice is by what the item says it is:
 *
 * - an engine: system Propulsion, or a profile of kind engine;
 * - a battery, alternator, solar controller, charger, inverter or generator:
 *   system Electrical, or a profile of the same kind (alternator, generator,
 *   battery);
 * - a tank: system Fuel or Water.
 *
 * Tools, consumables and spares are left out when the item model can tell: a
 * spare carries a part number or a target stock, a consumable is held in
 * numbers, and anything Stored is in a locker rather than installed. An item
 * that already owns a source is not a candidate.
 */
const SYSTEMS: Record<SourceKind, readonly string[]> = {
  engine: ['propulsion'],
  battery: ['electrical'],
  alternator: ['electrical'],
  solar: ['electrical'],
  charger: ['electrical'],
  inverter: ['electrical'],
  generator: ['electrical'],
  tank: ['fuel', 'water'],
}

/** The profile kinds that say an item is of a source kind. */
const PROFILE_KINDS: Partial<Record<SourceKind, string>> = {
  engine: 'engine',
  alternator: 'alternator',
  generator: 'generator',
  battery: 'battery',
}

function isInstalledEquipment(item: TileItem): boolean {
  if ((item.status ?? 'deployed') !== 'deployed') return false
  if ((item.part_number ?? '') !== '') return false
  if (item.required_quantity !== null && item.required_quantity !== undefined) return false
  return (item.quantity ?? 1) <= 1
}

export function candidateItems(kind: SourceKind, items: readonly TileItem[], profiles: readonly EngineProfile[]): TileItem[] {
  const profileKind = PROFILE_KINDS[kind]
  return items.filter((item) => {
    if (item.signalk_source !== '' || !isInstalledEquipment(item)) return false
    if (SYSTEMS[kind].includes(item.system ?? '')) return true
    const profile = item.profile_id ? profiles.find((p) => p.id === item.profile_id) : undefined
    return profileKind !== undefined && profile?.kind === profileKind
  })
}

function location(item: TileItem): string {
  return [item.zone_name, item.bin_code, item.location_detail].filter((part) => part && part.trim() !== '').join(' ')
}

/**
 * Option text for a list of candidates. A name that is alone is the name. Items
 * sharing a name are told apart by where they are and their serial number, and
 * by their place in the list when they have neither.
 */
export function candidateLabels(items: readonly TileItem[]): Map<string, string> {
  const byName = new Map<string, TileItem[]>()
  for (const item of items) {
    const key = item.name.trim().toLowerCase()
    byName.set(key, [...(byName.get(key) ?? []), item])
  }
  const labels = new Map<string, string>()
  for (const group of byName.values()) {
    group.forEach((item, index) => {
      if (group.length === 1) {
        labels.set(item.id, item.name)
        return
      }
      const details = [location(item), item.serial ? `serial ${item.serial}` : ''].filter((d) => d !== '')
      const base = [item.name, ...details].join(' · ')
      const unique = group.filter((other) => [other.name, ...[location(other), other.serial ? `serial ${other.serial}` : ''].filter((d) => d !== '')].join(' · ') === base).length === 1
      labels.set(item.id, details.length > 0 && unique ? base : `${base} (${index + 1} of ${group.length})`)
    })
  }
  return labels
}

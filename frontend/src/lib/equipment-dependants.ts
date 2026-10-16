/**
 * What depends on an equipment item, as the server reports it
 * (GET /api/inventory/equipment/:id/dependants), and the one sentence that
 * says so before the operator unmaps or deletes the item.
 */

export interface DependantTile {
  page_id: string
  page_name: string
  tile_id: string
  title: string
}

export interface DependantRole {
  role: string
  label: string
}

export type DependantRuleKind = 'state_of_charge' | 'zone' | 'rule'

export interface DependantAlarmRule {
  id: string
  label: string
  kind: DependantRuleKind
}

export interface EquipmentDependants {
  item_id: string
  tiles: DependantTile[]
  roles: DependantRole[]
  alarm_rules: DependantAlarmRule[]
  features: string[]
}

export function hasDependants(d: EquipmentDependants): boolean {
  return d.tiles.length + d.roles.length + d.alarm_rules.length + d.features.length > 0
}

function plural(n: number, one: string, many: string): string {
  return `${n} ${n === 1 ? one : many}`
}

/** A feature's name in running text: lower case, unless it opens with a name. */
function inSentence(label: string): string {
  if (/^(Mate|GNSS)\b/.test(label)) return label
  return label.charAt(0).toLowerCase() + label.slice(1)
}

function tileName(tile: DependantTile): string {
  // The ribbon is not on a page: its "page" is the ribbon itself.
  if (tile.tile_id === 'ribbon') return tile.page_name
  return `${tile.page_name}: ${tile.title}`
}

/**
 * "Used by 2 tiles (Underway: Port, Wall: Engines: Port), anchor auto-raise,
 * the engine checks, the House bank role, 3 state-of-charge alarm rules."
 * Empty when nothing depends on the item.
 */
export function describeDependants(d: EquipmentDependants): string {
  const parts: string[] = []
  if (d.tiles.length > 0) {
    parts.push(`${plural(d.tiles.length, 'tile', 'tiles')} (${d.tiles.map(tileName).join(', ')})`)
  }
  for (const feature of d.features) parts.push(inSentence(feature))
  for (const role of d.roles) parts.push(`the ${role.label} role`)
  const count = (kind: DependantRuleKind) => d.alarm_rules.filter((r) => r.kind === kind).length
  const soc = count('state_of_charge')
  if (soc > 0) parts.push(plural(soc, 'state-of-charge alarm rule', 'state-of-charge alarm rules'))
  const zones = count('zone')
  if (zones > 0) parts.push(plural(zones, 'gauge limit alarm', 'gauge limit alarms'))
  const rules = count('rule')
  if (rules > 0) parts.push(plural(rules, 'alarm rule', 'alarm rules'))
  return parts.length === 0 ? '' : `Used by ${parts.join(', ')}.`
}

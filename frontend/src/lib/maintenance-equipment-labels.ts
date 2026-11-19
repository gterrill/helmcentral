interface LabelledRule {
  equipment_id: string | null
  equipment_name: string
  equipment_alias: string | null
  equipment_location: string | null
}

/** The equipment name, plus what tells it apart from a same-named item. */
export interface EquipmentLabel {
  name: string
  detail: string | null
}

// Two names clash when they match once case and spacing are ignored.
function nameKey(name: string): string {
  return name.trim().replace(/\s+/g, ' ').toLowerCase()
}

/** One item as the labelling rule sees it. */
export interface LabelledItem {
  id: string
  name: string
  alias: string | null
  location: string | null
}

/**
 * The one clash rule. Returns the label per item id: a name shared by two or
 * more different items gets the item's alias, else its location, as the detail
 * so the operator can tell them apart. A name that is unique, or an item with
 * neither alias nor location, has no detail. The name is always the original,
 * never the normalised key.
 */
export function labelEquipmentList(items: readonly LabelledItem[]): Map<string, EquipmentLabel> {
  const idsByName = new Map<string, Set<string>>()
  for (const item of items) {
    const key = nameKey(item.name)
    const ids = idsByName.get(key) ?? new Set<string>()
    ids.add(item.id)
    idsByName.set(key, ids)
  }
  const labels = new Map<string, EquipmentLabel>()
  for (const item of items) {
    const detail = item.alias?.trim() || item.location?.trim() || null
    const isClash = (idsByName.get(nameKey(item.name))?.size ?? 0) > 1
    labels.set(item.id, { name: item.name, detail: isClash ? detail : null })
  }
  return labels
}

/** Returns a function giving the label for any rule in `rules`; see labelEquipmentList. */
export function equipmentLabeller(rules: readonly LabelledRule[]): (rule: LabelledRule) => EquipmentLabel {
  const labels = labelEquipmentList(
    rules.flatMap((r) => (r.equipment_id
      ? [{ id: r.equipment_id, name: r.equipment_name, alias: r.equipment_alias, location: r.equipment_location }]
      : [])),
  )
  return (rule) => (rule.equipment_id && labels.get(rule.equipment_id)) || { name: rule.equipment_name, detail: null }
}

/** The one-line text form, for dialogs: "Name (detail)". */
export function joinEquipmentLabel(label: EquipmentLabel): string {
  return label.detail ? `${label.name} (${label.detail})` : label.name
}

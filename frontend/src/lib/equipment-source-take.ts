import { sourceKindOf, type DiscoveryItemRef, type VesselDiscoveryResponse } from '@/lib/vessel-settings'

/** The item that owns source in what the boat reports, if any. */
export function sourceOwner(discovery: VesselDiscoveryResponse, source: string): DiscoveryItemRef | null {
  return discovery.groups.flatMap((g) => g.rows).find((r) => r.source === source)?.item ?? null
}

/**
 * What saving an item will do to the item that owns the source it picked.
 * A swap hands the owner this item's old source, which only makes sense when
 * both are the same kind of source (a tank for a tank). Anything else is a
 * move: the owner is left with none. The server applies the same rule.
 */
export interface SourceTakePlan {
  mode: 'swap' | 'move'
  ownerId: string
  ownerName: string
  /** What a move would clear from the owner: bank or tank figures. */
  ownerFigures: 'bank' | 'tank' | null
  newSource: string
  oldSource: string
}

export function sourceTakePlan(
  takeFrom: string | undefined,
  newSource: string,
  oldSource: string,
  discovery: VesselDiscoveryResponse,
): SourceTakePlan | null {
  if (!takeFrom || newSource === '') return null
  const owner = sourceOwner(discovery, newSource)
  if (!owner || owner.id !== takeFrom) return null
  const oldKind = sourceKindOf(oldSource)
  const swap = oldSource !== '' && oldKind !== null && oldKind === sourceKindOf(newSource)
  return { mode: swap ? 'swap' : 'move', ownerId: owner.id, ownerName: owner.name, ownerFigures: owner.figures ?? null, newSource, oldSource }
}

/** The sentence the Save bar shows while the swap or move is staged. */
export function sourceTakeMessage(plan: SourceTakePlan, itemName: string): string {
  const name = itemName.trim() === '' ? 'This item' : itemName.trim()
  if (plan.mode === 'swap') {
    return `Swap sources with ${plan.ownerName}. ${name} gets ${plan.newSource}, ${plan.ownerName} gets ${plan.oldSource}.`
  }
  const loses = plan.ownerFigures ? ` and loses its ${plan.ownerFigures} figures` : ''
  return `Move ${plan.newSource} from ${plan.ownerName}. ${plan.ownerName} will have no source${loses}.`
}

export function sourceTakeLabel(plan: SourceTakePlan): string {
  return plan.mode === 'swap' ? 'Swap sources' : 'Move source'
}

export function sourceTakeToast(plan: SourceTakePlan): string {
  return plan.mode === 'swap' ? 'Sources swapped' : 'Source moved'
}

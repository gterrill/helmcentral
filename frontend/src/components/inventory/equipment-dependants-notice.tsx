import type { DependantsState } from '@/hooks/use-equipment-dependants'
import { describeDependants } from '@/lib/equipment-dependants'

/** What deleting an item does to the things that use it. Nothing is removed with it. */
export const DELETE_CONSEQUENCE =
  'Tiles stay and show Equipment removed, the roles it plays are cleared, and its alarm rules stop watching it.'

/** What clearing an item's source does to the things that use it. */
export const CLEAR_SOURCE_CONSEQUENCE =
  'Tiles stay and show Not mapped, and the rest get no data until the item is mapped again.'

/** What moving an item to another source does to the things that use it. */
export const CHANGE_SOURCE_CONSEQUENCE = 'All of it follows the item to the new source.'

interface EquipmentDependantsNoticeProps {
  state: DependantsState
  /** What happens to the things listed, in a sentence after them. */
  consequence: string
}

/**
 * Says what uses an equipment item inside a confirmation. Shows nothing when
 * nothing uses it, so an unused item asks no more than it did before.
 */
export function EquipmentDependantsNotice({ state, consequence }: EquipmentDependantsNoticeProps) {
  if (state.status === 'idle') return null
  if (state.status === 'loading') {
    return <p className="text-sm text-muted-foreground">Checking what uses this item…</p>
  }
  if (state.status === 'error') {
    return (
      <p role="alert" className="text-sm text-destructive">
        Could not check what uses this item: {state.message}. Nothing can be changed until that works.
      </p>
    )
  }
  const sentence = describeDependants(state.dependants)
  if (sentence === '') return null
  return (
    <p data-testid="equipment-dependants" className="rounded-md border border-border bg-muted/40 px-3 py-2 text-sm">
      {sentence} {consequence}
    </p>
  )
}

/** True while a confirmation must not go ahead: the answer is not in yet, or failed. */
export function dependantsBlock(state: DependantsState): boolean {
  return state.status === 'loading' || state.status === 'error'
}

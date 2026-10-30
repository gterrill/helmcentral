import { useEffect, useState } from 'react'

import { fetchEquipmentDependants } from '@/hooks/use-inventory'
import type { EquipmentDependants } from '@/lib/equipment-dependants'
import { errorMessage } from '@/lib/errors'

export type DependantsState =
  | { status: 'idle' }
  | { status: 'loading' }
  | { status: 'error'; message: string }
  | { status: 'ready'; dependants: EquipmentDependants }

/**
 * Reads what depends on item `id` each time it changes to a non-null value,
 * for a dialog that is about to ask the operator to confirm. A failed read is
 * an error state, not "nothing depends on it": the caller keeps its confirm
 * button off until the answer is known.
 */
export function useEquipmentDependants(id: string | null): DependantsState {
  const [result, setResult] = useState<{ id: string; state: DependantsState } | null>(null)
  // An answer is good for one opening of the dialog. Whenever the id changes,
  // including to null and back to the same item, drop it in the same render,
  // so the earlier answer is never shown (with Confirm on) while the new one
  // is still being fetched.
  const [forId, setForId] = useState(id)
  if (forId !== id) {
    setForId(id)
    setResult(null)
  }

  useEffect(() => {
    if (id === null) return
    let cancelled = false
    fetchEquipmentDependants(id).then(
      (dependants) => { if (!cancelled) setResult({ id, state: { status: 'ready', dependants } }) },
      (err: unknown) => {
        if (!cancelled) setResult({ id, state: { status: 'error', message: errorMessage(err) } })
      },
    )
    return () => { cancelled = true }
  }, [id])

  if (id === null) return { status: 'idle' }
  return result !== null && result.id === id && forId === id ? result.state : { status: 'loading' }
}

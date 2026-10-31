import { GitMerge } from 'lucide-react'
import { useState } from 'react'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { mergeBlocker } from '@/lib/passage-merge'
import { passagesApi } from '@/lib/passages-api'
import type { PassageSummary } from '@/lib/passages'
import { errorMessage } from '@/lib/errors'

type Passage = Pick<PassageSummary, 'id' | 'start' | 'end' | 'cruise_id' | 'hidden'>

/** Merge for a selection of passages: one passage with a join at each place
 * they met, so every leg keeps its colour. Off, with the reason, unless they
 * are neighbours. */
export function MergePassagesButton({ selected, all, disabled = false, onMerged, onBusyChange }: {
  selected: readonly Passage[]
  /** Every passage in view, to find one in between. */
  all: readonly Passage[]
  disabled?: boolean
  onMerged: (id: string) => void
  /** Told when a merge starts and ends, so the rest of the bar can hold still. */
  onBusyChange?: (busy: boolean) => void
}) {
  const [busy, setBusy] = useState(false)
  const reason = mergeBlocker(selected, all)
  const merge = async () => {
    setBusy(true)
    onBusyChange?.(true)
    try {
      const merged = await passagesApi.merge([...selected].sort((a, b) => Date.parse(a.start) - Date.parse(b.start)).map((p) => p.id))
      onMerged(merged.id)
    } catch (err) {
      toast.error('Could not merge passages', { description: errorMessage(err) })
    } finally {
      setBusy(false)
      onBusyChange?.(false)
    }
  }
  return (
    <span title={reason ?? undefined}>
      <Button
        type="button"
        size="sm"
        variant="outline"
        disabled={disabled || busy || reason !== null}
        aria-description={reason ?? 'Joins the selected passages into one. Each keeps its own colour.'}
        onClick={() => void merge()}
      >
        <GitMerge className="h-4 w-4" data-icon="inline-start" aria-hidden="true" />
        Merge
      </Button>
    </span>
  )
}

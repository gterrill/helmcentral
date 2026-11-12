import { useEffect, useMemo, useState } from 'react'

import { ChoiceList } from '@/components/patterns/choice-list'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { ScrollArea } from '@/components/ui/scroll-area'
import { useEquipment } from '@/hooks/use-inventory'
import { useOpenJobs } from '@/hooks/use-open-jobs'
import { plural } from '@/lib/inbox-api'

// The picker for "Add to equipment" and "Add to a job": the operator searches
// the boat's own list and chooses the item or job; nothing is guessed. Mounted
// only while open, so each opening starts fresh, with the item used last time
// highlighted if it is among those shown.

export type InboxTargetKind = 'equipment' | 'job'

interface TargetOption {
  id: string
  name: string
  detail: string
}

interface TargetList {
  options: TargetOption[]
  loading: boolean
  error: string | null
}

/** How long the typing pauses before the box is asked. */
const SEARCH_DEBOUNCE_MS = 250

// The box searches equipment, so the picker finds what the Equipment screen
// finds: every word typed must hit the name, make, model, an alias or the
// zone name, in any order.
function useEquipmentTargets(query: string): TargetList {
  const [asked, setAsked] = useState('')
  useEffect(() => {
    const timer = setTimeout(() => setAsked(query.trim()), SEARCH_DEBOUNCE_MS)
    return () => clearTimeout(timer)
  }, [query])
  const { items, loading, error } = useEquipment({ q: asked })
  const options = useMemo<TargetOption[]>(() => items.map((i) => ({
    id: i.id,
    name: i.name,
    detail: [[i.manufacturer, i.model].filter(Boolean).join(' '), i.zone_name, i.status === 'stored' ? 'Stored' : ''].filter(Boolean).join(' · '),
  })), [items])
  return { options, loading, error }
}

// The box has no job search, and a boat has few open jobs: ask for them all,
// those on stored equipment too, and narrow here. The default listing is the
// open jobs, the only ones that take photos.
function useJobTargets(query: string): TargetList {
  const { jobs, loading, error } = useOpenJobs({ includeStored: true })
  const options = useMemo<TargetOption[]>(() => {
    const words = query.toLowerCase().split(/\s+/).filter(Boolean)
    return jobs
      .filter((j) => {
        const haystack = `${j.title} ${j.equipment_name}`.toLowerCase()
        return words.every((w) => haystack.includes(w))
      })
      .map((j) => ({ id: j.id, name: j.title, detail: j.equipment_name }))
  }, [jobs, query])
  return { options, loading, error }
}

const TARGETS = {
  equipment: { to: 'equipment', noun: 'equipment item', searchLabel: 'Search equipment', empty: 'No equipment matches.', useTargets: useEquipmentTargets },
  job: { to: 'a job', noun: 'job', searchLabel: 'Search jobs', empty: 'No open job matches.', useTargets: useJobTargets },
} as const

export interface InboxTargetDialogProps {
  kind: InboxTargetKind
  /** How many photos are being added. */
  count: number
  /** The item used last time on this device, only a starting highlight. */
  lastId: string | null
  onPick: (id: string, name: string) => void
  onClose: () => void
}

// A dialog is mounted for one kind at a time, so the hook it calls never changes.
export function InboxTargetDialog({ kind, count, lastId, onPick, onClose }: InboxTargetDialogProps) {
  const target = TARGETS[kind]
  const [query, setQuery] = useState('')
  const [pickedId, setPickedId] = useState<string | null>(null)
  const { options, loading, error } = target.useTargets(query)
  // Add here files only to an item on show: the choice, or failing that the
  // remembered item, counts only while it is among those listed. A search that
  // hides the choice clears it.
  const chosen = options.find((o) => o.id === (pickedId ?? lastId)) ?? null

  return (
    <Dialog open onOpenChange={(open) => { if (!open) onClose() }}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{`Add ${plural(count, 'photo')} to ${target.to}`}</DialogTitle>
          <DialogDescription>Search, then pick the {target.noun}.</DialogDescription>
        </DialogHeader>
        <div className="flex min-w-0 flex-col gap-2">
          <Input type="search" aria-label={target.searchLabel} placeholder="Search" value={query} onChange={(e) => setQuery(e.target.value)} />
          {error && <p role="alert" className="text-sm text-destructive">Could not read the list: {error}</p>}
          <ScrollArea className="h-56 w-full min-w-0 rounded-md border">
            <ChoiceList
              label={target.noun}
              labelHidden
              className="min-w-0 p-1"
              value={chosen?.id}
              onChange={setPickedId}
              choices={options.map((o) => ({
                value: o.id,
                label: <span className="block truncate">{o.name}</span>,
                details: o.detail ? <span className="block truncate">{o.detail}</span> : undefined,
              }))}
              empty={
                loading ? <p className="p-3 text-sm text-muted-foreground">Loading…</p>
                : !error ? <p className="p-3 text-sm text-muted-foreground">{target.empty}</p>
                : null
              }
            />
          </ScrollArea>
          <Button type="button" disabled={!chosen} onClick={() => { if (chosen) onPick(chosen.id, chosen.name) }}>Add here</Button>
        </div>
      </DialogContent>
    </Dialog>
  )
}

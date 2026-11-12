import { useMemo, useState } from 'react'

import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { ScrollArea } from '@/components/ui/scroll-area'
import { useEquipment } from '@/hooks/use-inventory'
import { useOpenJobs } from '@/hooks/use-open-jobs'
import { cn } from '@/lib/utils'

// The pickers for "Add to equipment" and "Add to a job": the operator searches
// the boat's own list and chooses the item or job; nothing is guessed. Both ask
// for the whole list (the Equipment and Open jobs screens read the same one, so
// it is usually cached) and narrow it as the operator types. Mounted only while
// open, so each opening starts fresh, with the item used last time highlighted
// if it is still in the list.

interface TargetOption {
  id: string
  name: string
  detail: string
  /** Everything a search matches against. */
  haystack: string
}

interface TargetDialogProps {
  /** How many photos are being added. */
  count: number
  /** The item used last time on this device, only a starting highlight. */
  lastId: string | null
  onPick: (id: string, name: string) => void
  onClose: () => void
}

function TargetDialog({ count, lastId, onPick, onClose, noun, title, searchLabel, noneText, options, loading, error }: TargetDialogProps & {
  noun: string
  title: string
  searchLabel: string
  noneText: string
  options: TargetOption[]
  loading: boolean
  error: string | null
}) {
  const [query, setQuery] = useState('')
  const [picked, setPicked] = useState<string | null>(null)
  const shown = useMemo(() => {
    const words = query.toLowerCase().split(/\s+/).filter(Boolean)
    return options.filter((o) => words.every((w) => o.haystack.includes(w)))
  }, [options, query])
  // The remembered item counts only while it is still there.
  const chosenId = picked ?? (lastId !== null && options.some((o) => o.id === lastId) ? lastId : null)
  const chosen = options.find((o) => o.id === chosenId)

  return (
    <Dialog open onOpenChange={(open) => { if (!open) onClose() }}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{title.replace('{photos}', count === 1 ? '1 photo' : `${count} photos`)}</DialogTitle>
          <DialogDescription>Search, then pick the {noun}.</DialogDescription>
        </DialogHeader>
        <div className="flex flex-col gap-2">
          <Input type="search" aria-label={searchLabel} placeholder="Search" value={query} onChange={(e) => setQuery(e.target.value)} />
          {error && <p role="alert" className="text-sm text-destructive">Could not read the list: {error}</p>}
          <ScrollArea className="h-56 rounded-md border">
            <div role="radiogroup" aria-label={noun} className="flex flex-col p-1">
              {loading && options.length === 0 && <p className="p-3 text-sm text-muted-foreground">Loading…</p>}
              {!loading && !error && shown.length === 0 && <p className="p-3 text-sm text-muted-foreground">{noneText}</p>}
              {shown.map((o) => (
                <button
                  key={o.id}
                  type="button"
                  role="radio"
                  aria-checked={o.id === chosenId}
                  onClick={() => setPicked(o.id)}
                  className={cn(
                    'flex min-w-0 flex-col rounded-sm px-2 py-1.5 text-left text-sm hover:bg-accent',
                    o.id === chosenId && 'bg-accent ring-1 ring-primary',
                  )}
                >
                  <span className="truncate">{o.name}</span>
                  {o.detail && <span className="truncate text-xs text-muted-foreground">{o.detail}</span>}
                </button>
              ))}
            </div>
          </ScrollArea>
          <Button type="button" disabled={!chosen} onClick={() => { if (chosen) onPick(chosen.id, chosen.name) }}>Add here</Button>
        </div>
      </DialogContent>
    </Dialog>
  )
}

export function InboxEquipmentDialog(props: TargetDialogProps) {
  const { items, loading, error } = useEquipment({})
  const options = useMemo<TargetOption[]>(() => items.map((i) => {
    const detail = [[i.manufacturer, i.model].filter(Boolean).join(' '), i.zone_name, i.status === 'stored' ? 'Stored' : ''].filter(Boolean).join(' · ')
    return { id: i.id, name: i.name, detail, haystack: [i.name, i.manufacturer, i.model, i.zone_name, ...i.aliases].join(' ').toLowerCase() }
  }), [items])
  return (
    <TargetDialog {...props} noun="equipment item" title="Add {photos} to equipment" searchLabel="Search equipment"
      noneText="No equipment matches." options={options} loading={loading} error={error} />
  )
}

export function InboxJobDialog(props: TargetDialogProps) {
  // The default listing is the open jobs, the only ones that take photos.
  const { jobs, loading, error } = useOpenJobs({})
  const options = useMemo<TargetOption[]>(() => jobs.map((j) => ({
    id: j.id, name: j.title, detail: j.equipment_name, haystack: `${j.title} ${j.equipment_name}`.toLowerCase(),
  })), [jobs])
  return (
    <TargetDialog {...props} noun="job" title="Add {photos} to a job" searchLabel="Search jobs"
      noneText="No open job matches." options={options} loading={loading} error={error} />
  )
}

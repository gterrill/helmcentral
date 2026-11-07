import { useState } from 'react'
import { ChevronRight } from 'lucide-react'

import { MaintenanceLogEntryDialog } from '@/components/inventory/maintenance-log-dialogs'
import {
  deleteMaintenanceLogEntry,
  updateMaintenanceLogEntry,
  useMaintenanceLogEntries,
  type MaintenanceLogEntry,
} from '@/hooks/use-maintenance'
import { cn } from '@/lib/utils'

// ADR 0138 and ADR 0269: the service log's rows, shared by an item's own page
// and the boat-wide list below. A line is its description (or kind), its date
// and kind, and its cost when there is one; pressing it opens the entry dialog.

interface ServiceLogRowsProps {
  entries: MaintenanceLogEntry[]
  onOpen: (entry: MaintenanceLogEntry) => void
}

export function ServiceLogRows({ entries, onOpen }: ServiceLogRowsProps) {
  return (
    <>
      {entries.map((entry) => (
        <button
          key={entry.id}
          type="button"
          onClick={() => onOpen(entry)}
          className="flex items-center justify-between gap-2 rounded-md border border-border px-3 py-2 text-left hover:bg-muted/40"
        >
          <div className="min-w-0">
            <p className="truncate text-sm font-medium">{entry.description || entry.kind}</p>
            <p className="truncate text-xs text-muted-foreground">{entry.performed_at} - {entry.kind}</p>
          </div>
          {entry.cost != null && (
            <span className="shrink-0 text-xs tabular-nums text-muted-foreground">{entry.cost} {entry.currency}</span>
          )}
        </button>
      ))}
    </>
  )
}

const FIRST_PAGE = 10

/** The lines no item owns: a finished open job that was never on an item, and
 * a calendar-only rule's completions. Nothing else lists them. Collapsed under
 * its heading, and left out when there are none. Open, edit, delete and photos
 * work as they do on an item's page. */
export function BoatServiceLog({ canWrite = true }: { canWrite?: boolean }) {
  const { entries, error } = useMaintenanceLogEntries({ noItem: true })
  const [expanded, setExpanded] = useState(false)
  const [showAll, setShowAll] = useState(false)
  const [editing, setEditing] = useState<MaintenanceLogEntry | null>(null)

  if (entries.length === 0 && !error) return null

  return (
    <div className="flex flex-col gap-2">
      <button
        type="button"
        aria-expanded={expanded}
        onClick={() => setExpanded((v) => !v)}
        className="flex items-center gap-1 self-start text-[10px] font-semibold tracking-wider text-muted-foreground uppercase hover:text-foreground"
      >
        <ChevronRight className={cn('h-3 w-3 transition-transform', expanded && 'rotate-90')} aria-hidden="true" />
        Service log, not on an item ({entries.length})
      </button>
      {error && <p role="alert" className="text-sm text-destructive">{error}</p>}
      {expanded && (
        <div className="flex max-h-72 flex-col gap-2 overflow-y-auto">
          <p className="text-xs text-muted-foreground">
            Lines with no item, such as a finished job that was never on one.
          </p>
          <ServiceLogRows entries={showAll ? entries : entries.slice(0, FIRST_PAGE)} onOpen={setEditing} />
          {!showAll && entries.length > FIRST_PAGE && (
            <button type="button" onClick={() => setShowAll(true)} className="self-start text-xs text-muted-foreground underline-offset-2 hover:underline">
              Show all {entries.length}
            </button>
          )}
        </div>
      )}

      <MaintenanceLogEntryDialog
        entry={editing}
        equipmentId=""
        open={editing !== null}
        onCancel={() => setEditing(null)}
        onSave={async (input, existingId) => {
          if (!existingId) throw new Error('A line with no item is made by finishing a job, not added here.')
          return updateMaintenanceLogEntry(existingId, input)
        }}
        onDelete={canWrite ? async (id) => { await deleteMaintenanceLogEntry(id) } : undefined}
      />
    </div>
  )
}

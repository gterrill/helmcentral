import { Button } from '@/components/ui/button'
import {
  tallyRun,
  unchosenLogEntries,
  unsettledFiles,
  type DecisionTally,
  type ImportSummary,
  type ImportSummaryRecord,
} from '@/lib/import-run'
import { StepHeading, type StepProps } from '@/components/import/import-shared'
import { formatAppLocation } from '@/lib/app-location'

// Step 9 and what follows it: the last look at what will be written, what is
// still outstanding, and then the result with links to what was created.

export interface Outstanding {
  stepId: string
  text: string
}

export function outstandingItems(props: Pick<StepProps, 'run' | 'decisions'>): Outstanding[] {
  const { run, decisions } = props
  const items: Outstanding[] = []
  const unchosen = unchosenLogEntries(run, decisions)
  if (unchosen.length > 0) {
    items.push({
      stepId: 'log',
      text: `${unchosen.length} service ${unchosen.length === 1 ? 'entry needs' : 'entries need'} its equipment chosen: ${unchosen.slice(0, 3).map((e) => e.title).join(', ')}${unchosen.length > 3 ? ' and more' : ''}.`,
    })
  }
  for (const file of unsettledFiles(run, decisions)) {
    items.push({ stepId: `file:${file.key}`, text: `${file.label} has not been uploaded or skipped.` })
  }
  return items
}

const ROW_LABELS: Record<string, string> = {
  particulars: 'Vessel particulars',
  zones: 'Locations',
  equipment: 'Engines and equipment',
  spares: 'Spares',
  bins: 'Lockers',
  log_entries: 'Service log entries',
  notes: 'Notes',
  files: 'Photos and documents',
}

function TallyTable({ tallies }: { tallies: Record<string, DecisionTally> }) {
  return (
    <table className="w-full text-sm">
      <thead>
        <tr className="text-left text-[10px] uppercase tracking-wider text-muted-foreground">
          <th className="py-1 font-medium">Section</th>
          <th className="py-1 text-right font-medium">Create</th>
          <th className="py-1 text-right font-medium">Use existing</th>
          <th className="py-1 text-right font-medium">Skip</th>
        </tr>
      </thead>
      <tbody>
        {Object.entries(ROW_LABELS).map(([id, label]) => (
          <tr key={id} className="border-t border-border">
            <th scope="row" className="py-1.5 text-left font-normal">{label}</th>
            <td className="py-1.5 text-right tabular-nums">{tallies[id].create}</td>
            <td className="py-1.5 text-right tabular-nums">{tallies[id].match}</td>
            <td className="py-1.5 text-right tabular-nums">{tallies[id].skip}</td>
          </tr>
        ))}
      </tbody>
    </table>
  )
}

export interface ImportConfirmStepProps extends StepProps {
  onGoToStep: (stepId: string) => void
  /** The server's own refusal of the commit, shown as it came. */
  commitError: string | null
}

export function ImportConfirmStep(props: ImportConfirmStepProps) {
  const { run, decisions, onGoToStep, commitError } = props
  const tallies = tallyRun(run, decisions)
  const outstanding = outstandingItems(props)

  return (
    <div className="space-y-4">
      <StepHeading title="Confirm">
        Nothing has been added yet. Importing writes everything below in one go, and either all of it arrives or none of it does.
      </StepHeading>
      <div className="rounded-md border border-border bg-card p-3">
        <TallyTable tallies={tallies} />
      </div>

      {(outstanding.length > 0 || commitError !== null) && (
        <div role="alert" className="space-y-2 rounded-md border border-destructive/40 bg-destructive/5 p-3 text-sm">
          {commitError !== null && <p className="text-destructive">{commitError}</p>}
          {outstanding.length > 0 && (
            <>
              <p className="font-medium">Still to do</p>
              <ul className="space-y-1">
                {outstanding.map((item) => (
                  <li key={item.stepId} className="flex min-w-0 items-center justify-between gap-2">
                    <span className="min-w-0">{item.text}</span>
                    <Button type="button" variant="outline" size="sm" onClick={() => onGoToStep(item.stepId)}>
                      Go there
                    </Button>
                  </li>
                ))}
              </ul>
            </>
          )}
        </div>
      )}
    </div>
  )
}

// --- Result ---------------------------------------------------------------

const KIND_LABEL: Record<ImportSummaryRecord['kind'], string> = {
  zone: 'Locations',
  bin: 'Lockers',
  equipment: 'Equipment and spares',
  log_entry: 'Service log entries',
  note: 'Notes',
  document: 'Photos and documents',
}

const COUNT_LABEL: Record<string, string> = { ...ROW_LABELS, spares: 'Spares' }

function recordHref(record: ImportSummaryRecord): string {
  const ctx = { firstPageId: null }
  switch (record.kind) {
    case 'equipment':
      return formatAppLocation({ panel: 'inventory', inventorySection: 'equipment', equipmentEditId: record.id }, ctx)
    case 'bin':
      return formatAppLocation({ panel: 'inventory', inventorySection: 'locations', binCode: record.label }, ctx)
    case 'zone':
      return formatAppLocation({ panel: 'inventory', inventorySection: 'locations' }, ctx)
    case 'log_entry':
      return formatAppLocation({ panel: 'inventory', inventorySection: 'maintenance' }, ctx)
    case 'note':
    case 'document':
      return formatAppLocation({ panel: 'documents', documentId: record.id }, ctx)
  }
}

export function ImportResult({ summary }: { summary: ImportSummary }) {
  const kinds = (Object.keys(KIND_LABEL) as Array<ImportSummaryRecord['kind']>).filter((kind) =>
    summary.records.some((r) => r.kind === kind),
  )

  return (
    <div className="space-y-4">
      <StepHeading title="Import finished">Everything you chose is now in Helmcentral.</StepHeading>
      <table className="w-full text-sm">
        <thead>
          <tr className="text-left text-[10px] uppercase tracking-wider text-muted-foreground">
            <th className="py-1 font-medium">Section</th>
            <th className="py-1 text-right font-medium">Created</th>
            <th className="py-1 text-right font-medium">Used existing</th>
            <th className="py-1 text-right font-medium">Skipped</th>
          </tr>
        </thead>
        <tbody>
          {Object.entries(summary.counts).map(([id, count]) => (
            <tr key={id} className="border-t border-border">
              <th scope="row" className="py-1.5 text-left font-normal">{COUNT_LABEL[id] ?? id}</th>
              <td className="py-1.5 text-right tabular-nums">{count.created}</td>
              <td className="py-1.5 text-right tabular-nums">{count.matched}</td>
              <td className="py-1.5 text-right tabular-nums">{count.skipped}</td>
            </tr>
          ))}
        </tbody>
      </table>

      <div className="space-y-2">
        {kinds.map((kind) => {
          const records = summary.records.filter((r) => r.kind === kind)
          return (
            <details key={kind} className="rounded-md border border-border bg-card p-3">
              <summary className="cursor-pointer text-sm font-medium">{KIND_LABEL[kind]} ({records.length})</summary>
              <ul className="mt-2 space-y-1">
                {records.map((record) => (
                  <li key={`${record.kind}-${record.key}`} className="min-w-0 truncate text-sm">
                    <a href={recordHref(record)} className="text-primary underline-offset-4 hover:underline">{record.label}</a>
                    {record.action === 'matched' && <span className="text-xs text-muted-foreground"> (used existing)</span>}
                  </li>
                ))}
              </ul>
            </details>
          )
        })}
      </div>
    </div>
  )
}

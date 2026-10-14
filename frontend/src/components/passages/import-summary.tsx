import { X } from 'lucide-react'

import { Button } from '@/components/ui/button'
import type { ImportSummary } from '@/lib/passages'

function plural(n: number, one: string, many: string) {
  return `${n} ${n === 1 ? one : many}`
}

/** What a GPX import did, in the operator's words. Shown until dismissed. */
export function ImportSummaryCard({ summary, onDismiss }: { summary: ImportSummary; onDismiss: () => void }) {
  const lines: string[] = [
    `${plural(summary.passages_created, 'new passage', 'new passages')} found in ${plural(summary.files.length, 'file', 'files')}.`,
  ]
  if (summary.passages_skipped > 0) {
    lines.push(`${plural(summary.passages_skipped, 'passage', 'passages')} skipped because a passage already covers that time.`)
  }
  if (summary.points_already_imported > 0) {
    lines.push(`${plural(summary.points_already_imported, 'position', 'positions')} had been imported before.`)
  }
  if (summary.points_in_recorded_log > 0) {
    lines.push(`${plural(summary.points_in_recorded_log, 'position', 'positions')} covered a stretch Helmcentral had already recorded itself, so the recorded track was kept.`)
  }
  if (summary.points_rejected > 0) {
    lines.push(`${plural(summary.points_rejected, 'position', 'positions')} were out of range or implied an impossible jump and were left out.`)
  }
  return (
    <div role="status" data-testid="import-summary" className="flex items-start gap-2 rounded-md border border-border bg-card p-3 text-sm">
      <div className="flex min-w-0 flex-1 flex-col gap-1">
        <p className="font-medium text-foreground">Import finished</p>
        {lines.map((l) => <p key={l} className="text-muted-foreground">{l}</p>)}
        <p className="text-xs text-muted-foreground">
          {summary.files.map((f) => `${f.name} (${f.points})`).join(', ')}
        </p>
      </div>
      <Button type="button" variant="ghost" size="icon-sm" aria-label="Dismiss import summary" onClick={onDismiss}>
        <X className="h-4 w-4" aria-hidden="true" />
      </Button>
    </div>
  )
}

import { useEffect, useState } from 'react'

import { FormSection, SettingsLayout } from '@/components/patterns'
import { Button } from '@/components/ui/button'
import { fetchImportRun, ImportApiError, recalledDraftRun, rememberDraftRun, type ImportRun } from '@/lib/import-run'

// Settings -> System -> Import: where bringing records across from another
// boat app starts. YachtWave is the only source so far.

export interface ImportSectionProps {
  /** Opens the wizard: "new" for the upload page, or a draft's run id. */
  onOpenImport: (runId: string) => void
}

export function ImportSection({ onOpenImport }: ImportSectionProps) {
  const [draft, setDraft] = useState<ImportRun | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    const id = recalledDraftRun()
    if (id === null) return
    let cancelled = false
    fetchImportRun(id).then(
      (run) => {
        if (cancelled) return
        if (run.status === 'draft') setDraft(run)
        else rememberDraftRun(null)
      },
      (err: unknown) => {
        if (cancelled) return
        // A run the server no longer knows is simply gone; anything else is
        // shown as it came.
        if (err instanceof ImportApiError && err.status === 404) rememberDraftRun(null)
        else setError(err instanceof Error ? err.message : String(err))
      },
    )
    return () => { cancelled = true }
  }, [])

  return (
    <SettingsLayout
      title="Import"
      description="Bring a boat's records across from another app instead of typing them in again. You check each page before anything is added."
    >
      <FormSection title="Sources">
      <div className="flex min-w-0 flex-col gap-3 rounded-md border border-border bg-card p-3 sm:flex-row sm:items-center sm:justify-between">
        <div className="min-w-0">
          <p className="text-sm font-medium">YachtWave</p>
          <p className="text-sm text-muted-foreground">
            Equipment, spares, the service log, notes and tasks, and the vessel&apos;s particulars, from a Vessel Export.
          </p>
        </div>
        <Button type="button" className="shrink-0" onClick={() => onOpenImport('new')}>
          Import from YachtWave
        </Button>
      </div>

      {error !== null && <p role="alert" className="text-sm text-destructive">{error}</p>}
      </FormSection>

      {draft !== null && (
        <FormSection title="In progress">
        <div className="flex min-w-0 flex-col gap-3 rounded-md border border-border bg-card p-3 sm:flex-row sm:items-center sm:justify-between">
          <div className="min-w-0">
            <p className="text-sm font-medium">Import in progress</p>
            <p className="truncate text-sm text-muted-foreground">{draft.source_label}</p>
          </div>
          <Button type="button" variant="outline" className="shrink-0" onClick={() => onOpenImport(draft.id)}>
            Resume
          </Button>
        </div>
        </FormSection>
      )}
    </SettingsLayout>
  )
}

import { useMemo, useState } from 'react'

import { Button } from '@/components/ui/button'
import { ConfirmDelete } from '@/components/patterns'
import { useImportRun } from '@/hooks/use-import-run'
import {
  abandonImportRun,
  commitImportRun,
  rememberDraftRun,
  stepBlocker,
  type ImportRun,
  type ImportSummary,
} from '@/lib/import-run'
import { cn } from '@/lib/utils'
import { ImportConfirmStep, ImportResult, outstandingItems } from '@/components/import/import-confirm-step'
import { ImportEquipmentStep } from '@/components/import/import-equipment-step'
import { ImportFileStep } from '@/components/import/import-file-step'
import { ImportLocationsStep } from '@/components/import/import-locations-step'
import { ImportLogStep } from '@/components/import/import-log-step'
import { ImportNotesStep } from '@/components/import/import-notes-step'
import { ImportReviewStep } from '@/components/import/import-review-step'
import { ImportUploadStep } from '@/components/import/import-upload-step'
import { ImportVesselStep } from '@/components/import/import-vessel-step'

// The import wizard: full-page, stepped, with the draft kept on the server.
// `runId` of "new" is the upload page; once the export is read the URL moves
// to the run and the operator never sees that page again.

export const NEW_IMPORT_RUN_ID = 'new'

interface Group {
  id: string
  label: string
}

const FIXED_GROUPS: Group[] = [
  { id: 'upload', label: 'Upload' },
  { id: 'review', label: 'Review' },
  { id: 'vessel', label: 'Vessel' },
  { id: 'locations', label: 'Locations' },
  { id: 'equipment', label: 'Equipment' },
  { id: 'log', label: 'Service log' },
  { id: 'notes', label: 'Notes' },
]

function groupOf(stepId: string): string {
  return stepId.startsWith('file:') ? 'files' : stepId
}

function StepIndicator({ groups, currentGroup, onSelect }: { groups: Group[]; currentGroup: string; onSelect?: (groupId: string) => void }) {
  const currentIndex = groups.findIndex((g) => g.id === currentGroup)
  return (
    <ol aria-label="Import steps" className="flex flex-wrap gap-x-1 gap-y-2">
      {groups.map((group, index) => {
        const state = index < currentIndex ? 'done' : index === currentIndex ? 'current' : 'todo'
        const reachable = state === 'done' && onSelect !== undefined && group.id !== 'upload'
        return (
          <li key={group.id} aria-current={state === 'current' ? 'step' : undefined}>
            <button
              type="button"
              disabled={!reachable}
              onClick={() => onSelect?.(group.id)}
              className={cn(
                'flex h-8 items-center gap-1.5 rounded-md border px-2 text-xs',
                state === 'current' && 'border-primary bg-primary/10 font-medium text-foreground',
                state === 'done' && 'border-border text-foreground',
                state === 'todo' && 'border-transparent text-muted-foreground',
                reachable && 'hover:bg-muted',
              )}
            >
              <span className="tabular-nums text-muted-foreground">{index + 1}</span>
              {group.label}
            </button>
          </li>
        )
      })}
    </ol>
  )
}

function Shell({ title, subtitle, indicator, children }: { title: string; subtitle?: string; indicator: React.ReactNode; children: React.ReactNode }) {
  return (
    <div className="mx-auto flex max-w-5xl flex-col gap-4">
      <div className="space-y-1">
        <h1 className="text-lg font-semibold text-foreground">{title}</h1>
        {subtitle && <p className="truncate text-xs text-muted-foreground">{subtitle}</p>}
      </div>
      {indicator}
      {children}
    </div>
  )
}

export interface ImportWizardProps {
  runId: string
  /** The URL follows the run: called with the new run's id after upload. */
  onRunChange: (runId: string) => void
  /** Back to the Import section. */
  onExit: () => void
}

export function ImportWizard({ runId, onRunChange, onExit }: ImportWizardProps) {
  if (runId === NEW_IMPORT_RUN_ID) {
    return (
      <Shell
        title="Import from YachtWave"
        indicator={<StepIndicator groups={[{ id: 'upload', label: 'Upload' }, ...FIXED_GROUPS.slice(1)]} currentGroup="upload" />}
      >
        <ImportUploadStep
          onCreated={(run) => {
            rememberDraftRun(run.id)
            onRunChange(run.id)
          }}
        />
        <div className="flex justify-start">
          <Button type="button" variant="ghost" onClick={onExit}>Cancel</Button>
        </div>
      </Shell>
    )
  }
  return <RunWizard key={runId} runId={runId} onExit={onExit} />
}

function RunWizard({ runId, onExit }: { runId: string; onExit: () => void }) {
  const { run, decisions, loadError, saving, setDecision, flush, adoptUpload, adoptRun } = useImportRun(runId)
  const [stepIndex, setStepIndex] = useState(0)
  const [stepError, setStepError] = useState<string | null>(null)
  const [commitError, setCommitError] = useState<string | null>(null)
  const [committing, setCommitting] = useState(false)
  const [summary, setSummary] = useState<ImportSummary | null>(null)
  const [confirmAbandon, setConfirmAbandon] = useState(false)
  const [abandoning, setAbandoning] = useState(false)
  const [abandonError, setAbandonError] = useState<string | null>(null)

  const stepIds = useMemo(() => {
    if (run === null) return []
    return ['review', 'vessel', 'locations', 'equipment', 'log', 'notes', ...run.staged.files.map((f) => `file:${f.key}`), 'confirm']
  }, [run])

  const groups = useMemo<Group[]>(() => {
    const hasFiles = run !== null && run.staged.files.length > 0
    return [...FIXED_GROUPS, ...(hasFiles ? [{ id: 'files', label: 'Photos and documents' }] : []), { id: 'confirm', label: 'Confirm' }]
  }, [run])

  if (loadError !== null) {
    return (
      <Shell title="Import from YachtWave" indicator={null}>
        <p role="alert" className="text-sm text-destructive">{loadError}</p>
        <div><Button type="button" variant="outline" onClick={onExit}>Back to Import</Button></div>
      </Shell>
    )
  }
  if (run === null || decisions === null) {
    return <p className="text-sm text-muted-foreground">Loading the import</p>
  }

  const stepId = stepIds[Math.min(stepIndex, stepIds.length - 1)]
  const props = { run, decisions, setDecision }

  if (run.status !== 'draft' || summary !== null) {
    return (
      <Shell title="Import from YachtWave" subtitle={run.source_label} indicator={null}>
        {summary !== null ? (
          <ImportResult summary={summary} />
        ) : (
          <p className="text-sm text-muted-foreground">
            {run.status === 'committed'
              ? 'This import has already been completed.'
              : 'This import was abandoned. Nothing from it was added.'}
          </p>
        )}
        <div><Button type="button" onClick={onExit}>Back to Import</Button></div>
      </Shell>
    )
  }

  const blocker = stepBlocker(stepId, run, decisions)
  const isConfirm = stepId === 'confirm'
  const confirmBlocked = isConfirm && outstandingItems(props).length > 0

  const goTo = async (targetIndex: number) => {
    setStepError(null)
    try {
      await flush()
    } catch (err) {
      setStepError(err instanceof Error ? err.message : String(err))
      return
    }
    setCommitError(null)
    setStepIndex(targetIndex)
  }

  const goToStepId = (id: string) => {
    const target = stepIds.indexOf(id)
    if (target >= 0) void goTo(target)
  }

  const back = () => {
    if (stepIndex === 0) {
      // Saving first so a half-made choice is not lost on the way out.
      void flush().then(onExit, (err: unknown) => setStepError(err instanceof Error ? err.message : String(err)))
      return
    }
    void goTo(stepIndex - 1)
  }

  const commit = async () => {
    setCommitError(null)
    setStepError(null)
    setCommitting(true)
    try {
      await flush()
      const result = await commitImportRun(run.id)
      rememberDraftRun(null)
      adoptRun(result.run)
      setSummary(result.summary)
    } catch (err) {
      setCommitError(err instanceof Error ? err.message : String(err))
    } finally {
      setCommitting(false)
    }
  }

  const abandon = async () => {
    setAbandoning(true)
    setAbandonError(null)
    try {
      await abandonImportRun(run.id)
      rememberDraftRun(null)
      setConfirmAbandon(false)
      onExit()
    } catch (err) {
      setAbandonError(err instanceof Error ? err.message : String(err))
    } finally {
      setAbandoning(false)
    }
  }

  const selectGroup = (groupId: string) => {
    const target = stepIds.findIndex((id) => groupOf(id) === groupId)
    if (target >= 0) void goTo(target)
  }

  let body: React.ReactNode
  if (stepId === 'review') body = <ImportReviewStep {...props} />
  else if (stepId === 'vessel') body = <ImportVesselStep {...props} />
  else if (stepId === 'locations') body = <ImportLocationsStep {...props} />
  else if (stepId === 'equipment') body = <ImportEquipmentStep {...props} />
  else if (stepId === 'log') body = <ImportLogStep {...props} />
  else if (stepId === 'notes') body = <ImportNotesStep {...props} />
  else if (stepId === 'confirm') body = <ImportConfirmStep {...props} onGoToStep={goToStepId} commitError={commitError} />
  else {
    const key = stepId.slice('file:'.length)
    const fileIndex = run.staged.files.findIndex((f) => f.key === key)
    body = (
      <ImportFileStep
        key={key}
        {...props}
        file={run.staged.files[fileIndex]}
        position={{ index: fileIndex, total: run.staged.files.length }}
        onUploaded={(updated: ImportRun, fileKey: string) => adoptUpload(updated, fileKey)}
      />
    )
  }

  return (
    <Shell
      title="Import from YachtWave"
      subtitle={run.source_label}
      indicator={<StepIndicator groups={groups} currentGroup={groupOf(stepId)} onSelect={selectGroup} />}
    >
      {body}

      {stepError !== null && <p role="alert" className="text-sm text-destructive">{stepError}</p>}

      <div className="flex flex-wrap items-center justify-between gap-2 border-t border-border pt-4">
        <Button type="button" variant="ghost" onClick={() => setConfirmAbandon(true)}>Abandon import</Button>
        <div className="flex min-w-0 items-center gap-2">
          {blocker !== null && <span role="status" className="min-w-0 text-xs text-muted-foreground">{blocker}</span>}
          <Button type="button" variant="outline" onClick={back} disabled={saving || committing}>Back</Button>
          {isConfirm ? (
            <Button type="button" onClick={() => void commit()} disabled={committing || saving || confirmBlocked}>
              {committing ? 'Importing' : 'Import'}
            </Button>
          ) : (
            <Button type="button" onClick={() => void goTo(stepIndex + 1)} disabled={saving || blocker !== null}>
              {saving ? 'Saving' : 'Next'}
            </Button>
          )}
        </div>
      </div>

      <ConfirmDelete
        open={confirmAbandon}
        onOpenChange={setConfirmAbandon}
        title="Abandon this import?"
        description={
          <>
            Your choices on these pages are thrown away and nothing is added to Helmcentral. Files you have already
            uploaded stay in Documents.
            {abandonError !== null && <span role="alert" className="mt-2 block text-destructive">{abandonError}</span>}
          </>
        }
        confirmLabel="Abandon import"
        cancelLabel="Keep working"
        onConfirm={() => void abandon()}
        deleting={abandoning}
      />
    </Shell>
  )
}

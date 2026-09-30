import { useState } from 'react'
import { BookOpen } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { Field, FieldLabel } from '@/components/ui/field'
import {
  createMaintenanceProcedureNote,
  setMaintenanceRuleProcedureNote,
  type MaintenanceRule,
} from '@/hooks/use-maintenance'
import { documentViewerHref } from '@/lib/document-citation'

// The Procedure note link shared by the hand-rule dialog and the profile-job
// dialog (spec §8): open the linked note, remove the link, or create a fresh
// note titled from the rule. Works on a profile job's id unchanged (ADR 0148).

interface ProcedureNoteFieldProps {
  rule: MaintenanceRule
  onChanged: (rule: MaintenanceRule) => void
  onError: (message: string | null) => void
}

export function ProcedureNoteField({ rule, onChanged, onError }: ProcedureNoteFieldProps) {
  const [busy, setBusy] = useState(false)

  const run = async (action: () => Promise<MaintenanceRule>) => {
    setBusy(true)
    onError(null)
    try {
      onChanged(await action())
    } catch (err) {
      onError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <Field>
      <FieldLabel>Procedure note</FieldLabel>
      {rule.procedure_note_id ? (
        <div className="flex items-center gap-2">
          <a
            href={documentViewerHref(rule.procedure_note_id)}
            className="inline-flex items-center gap-1.5 text-sm text-primary hover:underline"
          >
            <BookOpen className="h-4 w-4" aria-hidden="true" />
            Open procedure note
          </a>
          <Button type="button" variant="ghost" size="sm" disabled={busy} onClick={() => { void run(() => setMaintenanceRuleProcedureNote(rule.id, '')) }}>
            Remove
          </Button>
        </div>
      ) : (
        <Button
          type="button"
          variant="outline"
          size="sm"
          className="self-start"
          disabled={busy}
          onClick={() => { void run(async () => (await createMaintenanceProcedureNote(rule.id)).rule) }}
        >
          {busy ? 'Creating...' : 'Create procedure note'}
        </Button>
      )}
    </Field>
  )
}

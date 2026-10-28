import { useState } from 'react'
import { ConfirmDelete, FormSection } from '@/components/patterns'
import { routinesCapError, type RegularSettingsDraft } from '@/components/settings/settings-draft'
import { Button } from '@/components/ui/button'
import { Field, FieldContent, FieldDescription, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import { formatUsd, routineSchedule, useMateRoutines, type MateRoutine } from '@/hooks/use-mate-routines'
import { formatConversationRelativeTime } from '@/lib/assistant-conversation-search'

interface MateRoutinesProps {
  draft: RegularSettingsDraft
  onChange: (patch: Partial<RegularSettingsDraft>) => void
}

function lastRunText(r: MateRoutine): string {
  // A skip that is not a run (no boat position) can be newer than the last run.
  const latest = [r.last_run_at, r.last_skip_at].filter((t): t is string => !!t).sort().pop()
  if (!latest) return 'Has not run yet'
  const when = formatConversationRelativeTime(latest)
  return r.last_note ? `${when}: ${r.last_note}` : when
}

/**
 * Routines in Mate settings (ADR 0227): the one switch that lets them run
 * unattended, the daily limit, what they have spent, and the routines
 * themselves with an on/off switch and Delete. Asking Mate is how one is made.
 */
export function MateRoutines({ draft, onChange }: MateRoutinesProps) {
  const { routines, max, spent24hUsd, pausedUntil, problem, error, setEnabled, remove } = useMateRoutines()
  const [pendingDelete, setPendingDelete] = useState<MateRoutine | null>(null)
  const [deleting, setDeleting] = useState(false)
  const [actionError, setActionError] = useState<string | null>(null)
  const capError = routinesCapError(draft)

  const run = async (action: () => Promise<void>) => {
    setActionError(null)
    try {
      await action()
    } catch (err) {
      setActionError(err instanceof Error ? err.message : 'That did not work')
    }
  }

  return (
    <FormSection title="Routines">
      <FieldGroup>
        <Field orientation="horizontal">
          <Switch
            checked={draft.assistantRoutinesEnabled}
            onCheckedChange={(assistantRoutinesEnabled) => onChange({ assistantRoutinesEnabled })}
            aria-label="Let routines run on their own"
          />
          <FieldContent>
            <FieldLabel>Let routines run on their own</FieldLabel>
            <FieldDescription>
              A routine is a check Mate makes on a schedule, such as every hour or each morning. Runs happen with nobody
              watching and are paid for from your OpenRouter key, up to the daily limit below. What a routine finds is
              kept in its own conversation.
            </FieldDescription>
          </FieldContent>
        </Field>

        <Field>
          <FieldLabel htmlFor="assistant-routines-daily-cap">Daily limit (US dollars)</FieldLabel>
          <Input
            id="assistant-routines-daily-cap"
            type="text"
            inputMode="decimal"
            value={draft.assistantRoutinesDailyCap}
            aria-invalid={capError !== null || undefined}
            onChange={(e) => onChange({ assistantRoutinesDailyCap: e.target.value })}
            className="h-9 w-28 min-w-0 px-2 text-sm tabular-nums"
          />
          <FieldDescription>
            {capError !== null ? `Daily limit: ${capError}.` : 'Routines stop starting new runs once they have spent this much in the last 24 hours.'}
          </FieldDescription>
          <p className="text-sm tabular-nums text-muted-foreground" data-testid="routines-spend">
            {spent24hUsd === null ? 'Routines spent -- in the last 24 hours' : `Routines spent ${formatUsd(spent24hUsd)} in the last 24 hours`}
          </p>
          {pausedUntil !== null ? (
            <p role="status" className="text-xs text-muted-foreground" data-testid="routines-paused">
              A run failed and what it cost is not known, so routines are paused until{' '}
              {new Date(pausedUntil).toLocaleString([], { hour: '2-digit', minute: '2-digit', day: 'numeric', month: 'short' })}.
            </p>
          ) : null}
        </Field>

        {problem !== null ? (
          <p role="alert" className="text-sm text-destructive" data-testid="routines-problem">{problem}</p>
        ) : null}

        <div className="flex min-w-0 flex-col gap-2" aria-label="Routines">
          {error ? <p role="alert" className="text-xs text-destructive">Routines could not be loaded: {error}</p> : null}
          {actionError ? <p role="alert" className="text-xs text-destructive">{actionError}</p> : null}
          {routines.length === 0 && !error ? (
            <p className="text-sm text-muted-foreground">
              No routines yet. Ask Mate, for example &ldquo;check the boat every hour&rdquo; or &ldquo;give me a summary at
              06:00 while we are at anchor&rdquo;, and tap Apply on the routine it proposes. Up to {max} routines.
            </p>
          ) : (
            routines.map((r) => (
              <div key={r.id} className="flex min-w-0 items-center gap-3 rounded-md border border-border p-3" data-testid="routine-row">
                <Switch
                  checked={r.enabled}
                  onCheckedChange={(on) => void run(() => setEnabled(r.id, on))}
                  aria-label={`Run ${r.name}`}
                />
                <div className="min-w-0 flex-1">
                  <div className="truncate text-sm font-medium">{r.name}</div>
                  <div className="truncate text-xs text-muted-foreground">{routineSchedule(r)}</div>
                  <div className="line-clamp-2 text-xs text-muted-foreground">{lastRunText(r)}</div>
                </div>
                <Button variant="ghost" size="sm" aria-label={`Delete ${r.name}`} onClick={() => setPendingDelete(r)}>
                  Delete
                </Button>
              </div>
            ))
          )}
        </div>
      </FieldGroup>

      <ConfirmDelete
        open={pendingDelete !== null}
        onOpenChange={(open) => {
          if (!open) setPendingDelete(null)
        }}
        title={pendingDelete ? `Delete ${pendingDelete.name}?` : 'Delete routine?'}
        description="The routine stops running. What it found stays in its conversation until you delete that."
        deleting={deleting}
        onConfirm={() => {
          if (!pendingDelete) return
          setDeleting(true)
          void run(() => remove(pendingDelete.id)).finally(() => {
            setDeleting(false)
            setPendingDelete(null)
          })
        }}
      />
    </FormSection>
  )
}

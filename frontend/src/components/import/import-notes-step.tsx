import { Checkbox } from '@/components/ui/checkbox'
import { recordAction } from '@/lib/import-run'
import { AlreadyImported, Pill, RowCard, StepHeading, type StepProps } from '@/components/import/import-shared'

// Step 7: notes, and YachtWave tasks that arrive as notes. A note that looks
// like it holds a password or wifi key is listed as left behind: it has no
// tick box because nothing the operator decides can bring it across.

export function ImportNotesStep({ run, decisions, setDecision }: StepProps) {
  const notes = run.staged.notes
  return (
    <div className="space-y-4">
      <StepHeading title="Notes">
        Tick the notes to bring across. Each YachtWave task becomes a note that lists its priority, who it was assigned
        to, when it was due and whether it was done.
      </StepHeading>
      {notes.length === 0 && <p className="text-sm text-muted-foreground">The export has no notes.</p>}
      <ul className="grid grid-cols-1 gap-2 md:grid-cols-2">
        {notes.map((note) =>
          note.skip ? (
            <RowCard key={note.key}>
              <div className="flex min-w-0 items-center gap-2">
                <span className="min-w-0 flex-1 truncate text-sm font-medium">{note.title}</span>
                <Pill tone="warning">Not imported</Pill>
              </div>
              {note.date !== '' && <p className="text-xs tabular-nums text-muted-foreground">{note.date}</p>}
              <p className="text-sm text-muted-foreground">{note.skip_reason}</p>
            </RowCard>
          ) : (
            <RowCard key={note.key}>
              <div className="flex min-w-0 items-center gap-3">
                <Checkbox
                  aria-label={`Include ${note.title}`}
                  checked={recordAction(decisions, note.key) === 'create'}
                  onCheckedChange={(checked) =>
                    setDecision('records', note.key, { action: checked ? 'create' : 'skip', target_id: '' })
                  }
                />
                <div className="min-w-0 flex-1">
                  <p className="truncate text-sm font-medium">{note.title}</p>
                  {note.date !== '' && <p className="text-xs tabular-nums text-muted-foreground">{note.date}</p>}
                </div>
                <Pill>{note.kind === 'task' ? 'Task' : 'Note'}</Pill>
                <AlreadyImported run={run} keyName={note.key} />
              </div>
              {note.body !== '' && <p className="line-clamp-3 whitespace-pre-line text-sm text-muted-foreground">{note.body}</p>}
            </RowCard>
          ),
        )}
      </ul>
    </div>
  )
}

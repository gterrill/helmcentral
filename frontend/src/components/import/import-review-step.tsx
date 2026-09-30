import { stagedLabel, type StagedIssue } from '@/lib/import-run'
import { Pill, StepHeading, type StepProps } from '@/components/import/import-shared'

// Step 2: what the export holds, what will be left behind and why, and the
// data-quality problems worth a look before the operator decides anything.

// Shown under "Left behind": password notes (from the note records, so the
// secret-adjacent text is never echoed), checklists and sections the import
// does not read. An empty section is already visible as 0 of 0 above.
const LEFT_BEHIND = new Set(['checklist_without_steps', 'section_not_imported'])
const NOT_DATA_QUALITY = new Set([...LEFT_BEHIND, 'password_note_skipped', 'empty_section'])

function IssueLine({ issue, name }: { issue: StagedIssue; name: string }) {
  return (
    <li className="flex min-w-0 flex-col gap-0.5 rounded-md border border-border bg-card px-3 py-2">
      <div className="flex min-w-0 items-center gap-2">
        <Pill tone={issue.severity === 'warning' ? 'warning' : 'muted'}>{issue.section}</Pill>
        {name !== '' && <span className="min-w-0 truncate text-sm font-medium">{name}</span>}
      </div>
      <p className="text-sm text-muted-foreground">{issue.message}</p>
    </li>
  )
}

export function ImportReviewStep({ run }: StepProps) {
  const { staged } = run
  const leftBehind = staged.issues.filter((i) => LEFT_BEHIND.has(i.code))
  const passwordNotes = staged.notes.filter((n) => n.skip)
  const quality = staged.issues.filter((i) => !NOT_DATA_QUALITY.has(i.code))

  return (
    <div className="space-y-6">
      <StepHeading title="What is in the export">
        Check this over first. The pages that follow let you change what happens to each record.
      </StepHeading>

      {run.already_imported.length > 0 && (
        <p className="rounded-md border border-border bg-muted/40 px-3 py-2 text-sm">
          {run.already_imported.length} {run.already_imported.length === 1 ? 'record' : 'records'} from this export
          {' '}{run.already_imported.length === 1 ? 'is' : 'are'} already in Helmcentral from an earlier import. They are
          marked on each page and left out by default.
        </p>
      )}

      <section aria-label="Records per section" className="space-y-2">
        <h3 className="text-xs font-semibold uppercase tracking-[0.16em] text-muted-foreground">Sections</h3>
        <ul className="grid grid-cols-1 gap-2 sm:grid-cols-2 lg:grid-cols-3">
          {staged.sections.map((section) => (
            <li key={section.name} className="flex min-w-0 items-center justify-between gap-2 rounded-md border border-border bg-card px-3 py-2">
              <span className="min-w-0 truncate text-sm">{section.name}</span>
              <span className="shrink-0 text-xs tabular-nums text-muted-foreground">
                {section.imported} of {section.listed}
              </span>
            </li>
          ))}
        </ul>
        <p className="text-xs text-muted-foreground">Figures show how many records in each section can be imported.</p>
      </section>

      {(passwordNotes.length > 0 || leftBehind.length > 0) && (
        <section aria-label="Left behind" className="space-y-2">
          <h3 className="text-xs font-semibold uppercase tracking-[0.16em] text-muted-foreground">Left behind</h3>
          <ul className="space-y-2">
            {passwordNotes.map((note) => (
              <li key={note.key} className="flex min-w-0 flex-col gap-0.5 rounded-md border border-border bg-card px-3 py-2">
                <div className="flex min-w-0 items-center gap-2">
                  <Pill tone="warning">Note not imported</Pill>
                  <span className="min-w-0 truncate text-sm font-medium">{note.title}</span>
                  {note.date !== '' && <span className="shrink-0 text-xs tabular-nums text-muted-foreground">{note.date}</span>}
                </div>
                <p className="text-sm text-muted-foreground">{note.skip_reason}</p>
              </li>
            ))}
            {leftBehind.map((issue, index) => (
              <IssueLine key={`${issue.code}-${issue.key}-${index}`} issue={issue} name={stagedLabel(staged, issue.key)} />
            ))}
          </ul>
        </section>
      )}

      {quality.length > 0 && (
        <section aria-label="Worth a look" className="space-y-2">
          <h3 className="text-xs font-semibold uppercase tracking-[0.16em] text-muted-foreground">Worth a look</h3>
          <ul className="space-y-2">
            {quality.map((issue, index) => (
              <IssueLine key={`${issue.code}-${issue.key}-${index}`} issue={issue} name={stagedLabel(staged, issue.key)} />
            ))}
          </ul>
        </section>
      )}
    </div>
  )
}

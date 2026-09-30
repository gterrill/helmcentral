import { Checkbox } from '@/components/ui/checkbox'
import { issuesFor, recordAction, usableEquipment, type StagedLogEntry } from '@/lib/import-run'
import { AlreadyImported, ImportSelect, Pill, RowCard, StepHeading, type StepProps } from '@/components/import/import-shared'

// Step 6: the maintenance log. Each entry is ticked to come across or not.
// Where the export names equipment that fits more than one item (two engines
// both called "Cummins QSB 6.7"), the operator says which one it was.

const NONE = 'none'
const CHOOSE = ''

export function ImportLogStep(props: StepProps) {
  const { run, decisions, setDecision } = props
  const { staged } = run
  const equipment = usableEquipment(run, decisions)

  const choiceFor = (entry: StagedLogEntry): string => {
    const chosen = decisions.log_equipment[entry.key]
    if (chosen === undefined) return CHOOSE
    if (chosen.equipment_key !== '') return `key:${chosen.equipment_key}`
    if (chosen.equipment_id !== '') return `id:${chosen.equipment_id}`
    return NONE
  }

  const choose = (entry: StagedLogEntry, value: string) => {
    if (value === CHOOSE) return
    if (value === NONE) setDecision('log_equipment', entry.key, { equipment_key: '', equipment_id: '' })
    else if (value.startsWith('key:')) setDecision('log_equipment', entry.key, { equipment_key: value.slice(4), equipment_id: '' })
  }

  return (
    <div className="space-y-4">
      <StepHeading title="Maintenance log">
        Tick the service entries to bring across. They are added to the service log of the equipment they were for.
      </StepHeading>
      {staged.log_entries.length === 0 && <p className="text-sm text-muted-foreground">The export has no service entries.</p>}
      <ul className="space-y-2">
        {staged.log_entries.map((entry, entryIndex) => {
          const included = recordAction(decisions, entry.key) === 'create'
          const ambiguous = entry.candidates.length > 1
          const choice = choiceFor(entry)
          const flagged = issuesFor(staged, entry.key).filter((i) => i.code === 'duplicate')
          const single = entry.equipment_key !== '' ? staged.equipment.find((e) => e.key === entry.equipment_key) : undefined
          return (
            <RowCard key={entry.key}>
              <div className="flex min-w-0 items-center gap-3">
                <Checkbox
                  aria-label={`Include entry ${entryIndex + 1}: ${entry.title} ${entry.date}`}
                  checked={included}
                  onCheckedChange={(checked) =>
                    setDecision('records', entry.key, { action: checked ? 'create' : 'skip', target_id: '' })
                  }
                />
                <div className="min-w-0 flex-1">
                  <p className="truncate text-sm font-medium">{entry.title}</p>
                  <p className="truncate text-xs tabular-nums text-muted-foreground">
                    {[entry.date || 'No date', entry.type, entry.hours !== null && `${entry.hours} h`].filter(Boolean).join(' · ')}
                  </p>
                </div>
                <AlreadyImported run={run} keyName={entry.key} />
                {flagged.length > 0 && <Pill tone="warning">Looks like a duplicate</Pill>}
              </div>
              {entry.body !== '' && <p className="line-clamp-2 text-sm text-muted-foreground">{entry.body}</p>}
              {ambiguous ? (
                <label className="flex min-w-0 flex-col gap-1">
                  <span className="text-[10px] font-medium uppercase tracking-wider text-muted-foreground">
                    Which equipment: the export says &ldquo;{entry.equipment_name}&rdquo;
                  </span>
                  <ImportSelect
                    aria-label={`Equipment for entry ${entryIndex + 1}: ${entry.title} ${entry.date}`}
                    value={choice}
                    onChange={(e) => choose(entry, e.target.value)}
                  >
                    <option value={CHOOSE}>Choose the equipment</option>
                    {entry.candidates.map((key) => {
                      const candidate = equipment.find((e) => e.key === key)
                      return candidate === undefined ? null : (
                        <option key={key} value={`key:${key}`}>
                          {[candidate.name, candidate.detail].filter(Boolean).join(', ')}
                        </option>
                      )
                    })}
                    <option value={NONE}>No equipment</option>
                  </ImportSelect>
                  {included && choice === CHOOSE && (
                    <span role="alert" className="text-xs text-destructive">Pick the equipment, or untick this entry.</span>
                  )}
                </label>
              ) : (
                <p className="truncate text-xs text-muted-foreground">
                  Equipment: {single ? single.name : entry.equipment_name || 'none named'}
                </p>
              )}
            </RowCard>
          )
        })}
      </ul>
    </div>
  )
}

import { useMemo } from 'react'

import { useEquipment, useInventoryZones } from '@/hooks/use-inventory'
import { STOCK_STATE_LABEL, stockState } from '@/lib/equipment-stock'
import { issuesFor, recordAction, zoneAction, type StagedEquipment, type StagedSpare } from '@/lib/import-run'
import {
  AlreadyImported,
  decodeChoice,
  encodeChoice,
  ImportSelect,
  Pill,
  RowCard,
  StepHeading,
  type StepProps,
} from '@/components/import/import-shared'

// Step 5: equipment and spares. One choice per row: create it, match it to
// something already here (nothing is written to the matched record, the import
// only remembers the pair), or skip it.

interface ChoiceSelectProps extends StepProps {
  recordKey: string
  name: string
  matches: Array<{ id: string; label: string }>
  matchNoun: string
}

function ChoiceSelect({ decisions, setDecision, recordKey, name, matches, matchNoun }: ChoiceSelectProps) {
  const decision = decisions.records[recordKey]
  const value = encodeChoice(recordAction(decisions, recordKey), decision?.target_id ?? '')
  return (
    <ImportSelect
      aria-label={`What to do with ${name}`}
      value={value}
      onChange={(e) => {
        const choice = decodeChoice(e.target.value)
        setDecision('records', recordKey, { action: choice.action, target_id: choice.target })
      }}
    >
      <option value="create">Create new</option>
      {matches.map((m) => (
        <option key={m.id} value={`match:${m.id}`}>{`Same as existing ${matchNoun}: ${m.label}`}</option>
      ))}
      {value.startsWith('match:') && !matches.some((m) => `match:${m.id}` === value) && (
        <option value={value}>{`Existing ${matchNoun} (not found)`}</option>
      )}
      <option value="skip">Skip</option>
    </ImportSelect>
  )
}

function FlagPills({ props, recordKey }: { props: StepProps; recordKey: string }) {
  return (
    <>
      {issuesFor(props.run.staged, recordKey).map((issue) => (
        <Pill key={issue.code} tone={issue.severity === 'warning' ? 'warning' : 'muted'}>{issueLabel(issue.code)}</Pill>
      ))}
    </>
  )
}

function issueLabel(code: string): string {
  switch (code) {
    case 'duplicate': return 'Looks like a duplicate'
    case 'junk_name': return 'Name looks like a typo'
    case 'unknown_part_number': return 'No part number'
    default: return 'Check this'
  }
}

function EquipmentRow({ props, item, matches }: { props: StepProps; item: StagedEquipment; matches: ChoiceSelectProps['matches'] }) {
  const flagged = issuesFor(props.run.staged, item.key)
  return (
    <RowCard>
      <div className="flex min-w-0 flex-wrap items-center gap-2">
        <span className="min-w-0 truncate text-sm font-medium">{item.name}</span>
        <Pill>{item.kind === 'engine' ? 'Engine' : 'Equipment'}</Pill>
        <AlreadyImported run={props.run} keyName={item.key} />
        <FlagPills props={props} recordKey={item.key} />
      </div>
      <p className="truncate text-xs text-muted-foreground">
        {[item.detail, item.manufacturer, item.serial && `Serial ${item.serial}`].filter(Boolean).join(' · ') || '--'}
      </p>
      {flagged.filter((i) => i.severity === 'warning').map((issue) => (
        <p key={issue.code} className="text-xs text-amber-700 dark:text-amber-400">{issue.message}</p>
      ))}
      <ChoiceSelect {...props} recordKey={item.key} name={item.name} matches={matches} matchNoun="equipment" />
    </RowCard>
  )
}

function SpareRow({
  props,
  spare,
  equipmentMatches,
  binMatches,
  locationDecided,
}: {
  props: StepProps
  spare: StagedSpare
  equipmentMatches: ChoiceSelectProps['matches']
  binMatches: ChoiceSelectProps['matches']
  locationDecided: boolean
}) {
  const state = stockState(spare.on_hand, spare.required)
  const isBin = spare.kind === 'bin'
  const creating = recordAction(props.decisions, spare.key) === 'create'
  return (
    <RowCard>
      <div className="flex min-w-0 flex-wrap items-center gap-2">
        <span className="min-w-0 truncate text-sm font-medium">{spare.name}</span>
        <Pill>{isBin ? 'Locker' : 'Spare'}</Pill>
        <AlreadyImported run={props.run} keyName={spare.key} />
        <FlagPills props={props} recordKey={spare.key} />
      </div>
      {!isBin && (
        <p className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1 text-xs text-muted-foreground">
          <span>Part number {spare.part_number === '' ? '--' : spare.part_number}</span>
          <span className="tabular-nums">
            On hand {spare.on_hand} / required {spare.required ?? '--'}
          </span>
          {state !== null && (
            <span className={state === 'out' ? 'font-medium text-red-600 dark:text-red-400' : 'font-medium text-amber-600 dark:text-amber-400'}>
              {STOCK_STATE_LABEL[state]}
            </span>
          )}
        </p>
      )}
      <p className="truncate text-xs text-muted-foreground">
        {[spare.location, spare.detail, spare.category].filter(Boolean).join(' · ') || '--'}
      </p>
      {isBin && spare.items.length > 0 && (
        <ul aria-label={`Items in ${spare.name}`} className="list-disc space-y-0.5 pl-5 text-sm">
          {spare.items.map((line, i) => <li key={`${i}-${line}`} className="truncate">{line}</li>)}
        </ul>
      )}
      {isBin && creating && !locationDecided && (
        <p role="alert" className="text-xs text-destructive">
          A locker needs a location. Create or match its location on the Locations page first.
        </p>
      )}
      <ChoiceSelect
        {...props}
        recordKey={spare.key}
        name={spare.name}
        matches={isBin ? binMatches : equipmentMatches}
        matchNoun={isBin ? 'locker' : 'equipment'}
      />
    </RowCard>
  )
}

export function ImportEquipmentStep(props: StepProps) {
  const { run, decisions } = props
  const { items } = useEquipment({})
  const { zones } = useInventoryZones()

  const equipmentMatches = useMemo(
    () => items.map((i) => ({ id: i.id, label: [i.name, i.model].filter(Boolean).join(' ') })),
    [items],
  )
  const binMatches = useMemo(
    () => zones.flatMap((z) => z.bins.map((b) => ({ id: b.id, label: `${z.name}, ${b.code}` }))),
    [zones],
  )

  const locationAction = (name: string) => {
    const location = run.staged.locations.find((l) => l.name.toLowerCase() === name.toLowerCase())
    return location !== undefined && name !== '' && zoneAction(decisions, location.key) !== 'skip'
  }

  return (
    <div className="space-y-6">
      <StepHeading title="Equipment and spares">
        Choose what happens to each item. Matching to something you already have adds nothing to that record.
      </StepHeading>

      <section aria-label="Equipment" className="space-y-2">
        <h3 className="text-xs font-semibold uppercase tracking-[0.16em] text-muted-foreground">Engines and equipment</h3>
        <ul className="grid grid-cols-1 gap-2 md:grid-cols-2">
          {run.staged.equipment.map((item) => (
            <EquipmentRow key={item.key} props={props} item={item} matches={equipmentMatches} />
          ))}
        </ul>
      </section>

      <section aria-label="Spares and lockers" className="space-y-2">
        <h3 className="text-xs font-semibold uppercase tracking-[0.16em] text-muted-foreground">Spares and lockers</h3>
        <ul className="grid grid-cols-1 gap-2 md:grid-cols-2">
          {run.staged.spares.map((spare) => (
            <SpareRow
              key={spare.key}
              props={props}
              spare={spare}
              equipmentMatches={equipmentMatches}
              binMatches={binMatches}
              locationDecided={locationAction(spare.location)}
            />
          ))}
        </ul>
      </section>
    </div>
  )
}

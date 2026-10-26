import type { Dispatch, SetStateAction } from 'react'

import { FormRow, FormSection } from '@/components/patterns'
import { Field, FieldDescription, FieldLabel } from '@/components/ui/field'
import { InputGroup, InputGroupAddon, InputGroupInput, InputGroupText } from '@/components/ui/input-group'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import type { EquipmentInput } from '@/hooks/use-inventory'
import { useTankLevelDefaults } from '@/hooks/use-tank-levels'
import { isLowBadKind, tankKindOfSource, tankLevelsError } from '@/lib/tank-levels'

interface EquipmentTankLevelsSectionProps {
  draft: EquipmentInput
  setDraft: Dispatch<SetStateAction<EquipmentInput>>
  canWrite: boolean
}

/** A typed percentage; blank, or anything that is not a positive number, is 0, which means "use the default". */
function levelFrom(text: string): number {
  const parsed = Number.parseFloat(text)
  return Number.isFinite(parsed) && parsed > 0 ? parsed : 0
}

/**
 * A tank's own alarm levels (ADR 0214). Blank means the kind's default from
 * Settings, shown as the placeholder. The Tanks tile and the fuel bars colour
 * by these levels and the alarms use them too. "Alarms off" silences only the
 * alarms.
 */
export function EquipmentTankLevelsSection({ draft, setDraft, canWrite }: EquipmentTankLevelsSectionProps) {
  const kind = tankKindOfSource(draft.signalk_source)
  const { defaults } = useTankLevelDefaults()
  if (kind === null) return null

  const kindDefault = defaults?.[kind] ?? null
  const problem = kindDefault
    ? tankLevelsError(
        kind,
        draft.tank_warn_percent > 0 ? draft.tank_warn_percent : null,
        draft.tank_alarm_percent > 0 ? draft.tank_alarm_percent : null,
        kindDefault,
      )
    : null
  const lowBad = isLowBadKind(kind)

  return (
    <FormSection
      title="Tank levels"
      description={
        lowBad
          ? 'Where this tank turns amber (warn) and red (alarm) as it empties, as a percentage of full. Leave a level empty to use the default for this kind of tank.'
          : 'Where this tank turns amber (warn) and red (alarm) as it fills, as a percentage of full. Leave a level empty to use the default for this kind of tank.'
      }
    >
      <FormRow>
        <Field>
          <FieldLabel htmlFor="equipment-tank-warn">Warn at</FieldLabel>
          <InputGroup>
            <InputGroupInput
              id="equipment-tank-warn"
              type="number" inputMode="decimal" step="any" min={0} max={100}
              value={draft.tank_warn_percent || ''}
              placeholder={kindDefault ? String(kindDefault.warn) : ''}
              disabled={!canWrite}
              onChange={(e) => setDraft((p) => ({ ...p, tank_warn_percent: levelFrom(e.target.value) }))}
            />
            <InputGroupAddon align="inline-end">
              <InputGroupText>%</InputGroupText>
            </InputGroupAddon>
          </InputGroup>
        </Field>
        <Field>
          <FieldLabel htmlFor="equipment-tank-alarm">Alarm at</FieldLabel>
          <InputGroup>
            <InputGroupInput
              id="equipment-tank-alarm"
              type="number" inputMode="decimal" step="any" min={0} max={100}
              value={draft.tank_alarm_percent || ''}
              placeholder={kindDefault ? String(kindDefault.alarm) : ''}
              disabled={!canWrite}
              onChange={(e) => setDraft((p) => ({ ...p, tank_alarm_percent: levelFrom(e.target.value) }))}
            />
            <InputGroupAddon align="inline-end">
              <InputGroupText>%</InputGroupText>
            </InputGroupAddon>
          </InputGroup>
        </Field>
      </FormRow>
      {problem && <p role="alert" className="text-sm text-destructive">{problem}</p>}
      <Field orientation="horizontal">
        <Switch
          id="equipment-tank-alarms-off"
          checked={draft.tank_alarms_off}
          disabled={!canWrite}
          onCheckedChange={(checked) => setDraft((p) => ({ ...p, tank_alarms_off: checked }))}
        />
        <Label htmlFor="equipment-tank-alarms-off">Alarms off</Label>
      </Field>
      <FieldDescription>
        {lowBad ? 'Raised when the level falls below a level.' : 'Raised when the level rises above a level.'}{' '}
        With alarms off the Tanks tile still shows this tank's level and colours; only the alarm is silenced.
      </FieldDescription>
    </FormSection>
  )
}

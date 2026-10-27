import { useEffect, useState, type Dispatch, type SetStateAction } from 'react'

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

/**
 * What a typed level means: blank is 0 ("use the default"), a number is itself,
 * and anything else is not a level yet. The text itself stays in the box, so
 * "0.5" and "-5" can be typed and the error says what is wrong with them.
 */
function parseLevel(text: string): number | null {
  const trimmed = text.trim()
  if (trimmed === '') return 0
  const value = Number(trimmed)
  return Number.isFinite(value) ? value : null
}

const asText = (value: number): string => (value === 0 ? '' : String(value))

/**
 * A tank's own alarm levels (ADR 0214). Blank means the kind's default from
 * Settings, shown as the placeholder. The Tanks tile and the fuel bars colour
 * by these levels and the alarms use them too. "Alarms off" silences only the
 * alarms.
 */
export function EquipmentTankLevelsSection({ draft, setDraft, canWrite }: EquipmentTankLevelsSectionProps) {
  const kind = tankKindOfSource(draft.signalk_source)
  const { defaults } = useTankLevelDefaults()
  const [warnText, setWarnText] = useState(asText(draft.tank_warn_percent))
  const [alarmText, setAlarmText] = useState(asText(draft.tank_alarm_percent))

  // Discard and a fresh load change the draft from outside; the boxes follow.
  useEffect(() => {
    setWarnText((text) => (parseLevel(text) === draft.tank_warn_percent ? text : asText(draft.tank_warn_percent)))
  }, [draft.tank_warn_percent])
  useEffect(() => {
    setAlarmText((text) => (parseLevel(text) === draft.tank_alarm_percent ? text : asText(draft.tank_alarm_percent)))
  }, [draft.tank_alarm_percent])

  if (kind === null) return null

  const typedWarn = parseLevel(warnText)
  const typedAlarm = parseLevel(alarmText)
  const notANumber = typedWarn === null || typedAlarm === null

  const kindDefault = defaults?.[kind] ?? null
  const problem = notANumber
    ? 'Enter a number between 0 and 100, or leave the box empty to use the default.'
    : kindDefault
      ? tankLevelsError(kind, typedWarn === 0 ? null : typedWarn, typedAlarm === 0 ? null : typedAlarm, kindDefault)
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
              type="text" inputMode="decimal"
              value={warnText}
              placeholder={kindDefault ? String(kindDefault.warn) : ''}
              disabled={!canWrite}
              onChange={(e) => {
                setWarnText(e.target.value)
                const value = parseLevel(e.target.value)
                if (value !== null) setDraft((p) => ({ ...p, tank_warn_percent: value }))
              }}
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
              type="text" inputMode="decimal"
              value={alarmText}
              placeholder={kindDefault ? String(kindDefault.alarm) : ''}
              disabled={!canWrite}
              onChange={(e) => {
                setAlarmText(e.target.value)
                const value = parseLevel(e.target.value)
                if (value !== null) setDraft((p) => ({ ...p, tank_alarm_percent: value }))
              }}
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

import { FormSection, SettingsLayout } from '@/components/patterns'
import { Field, FieldLabel } from '@/components/ui/field'
import { Select, SelectItem, SelectPopup, SelectTrigger, SelectValue } from '@/components/ui/select'
import type { RegularSettingsDraft } from '@/components/settings/settings-draft'

interface GeneralSectionProps {
  draft: RegularSettingsDraft
  onChange: (patch: Partial<RegularSettingsDraft>) => void
}

export function GeneralSection({ draft, onChange }: GeneralSectionProps) {
  return (
    <SettingsLayout title="General">
      <FormSection title="Units">
        <Field>
          <FieldLabel htmlFor="distance-units">Units</FieldLabel>
          <Select
            value={draft.distanceUnits}
            onValueChange={(value) => value && onChange({ distanceUnits: value as 'metric' | 'imperial' })}
          >
            <SelectTrigger id="distance-units" aria-label="Distance units">
              <SelectValue />
            </SelectTrigger>
            <SelectPopup>
              <SelectItem value="metric">Metric</SelectItem>
              <SelectItem value="imperial">Imperial</SelectItem>
            </SelectPopup>
          </Select>
        </Field>
      </FormSection>
    </SettingsLayout>
  )
}

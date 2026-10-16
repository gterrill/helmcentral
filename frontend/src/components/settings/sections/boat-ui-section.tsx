import { FormRow, FormSection, SettingsLayout } from '@/components/patterns'
import { VesselParticularsForm } from '@/components/settings/sections/vessel-particulars-form'
import { Field, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import type { RegularSettingsDraft } from '@/components/settings/settings-draft'

interface BoatUiSectionProps {
  draft: RegularSettingsDraft
  onChange: (patch: Partial<RegularSettingsDraft>) => void
}

export function BoatUiSection({ draft, onChange }: BoatUiSectionProps) {
  return (
    <SettingsLayout title="Vessel">
      <FormSection title="Identity">
        <FormRow>
          <Field>
            <FieldLabel htmlFor="vessel-prefix">Vessel Prefix</FieldLabel>
            <Input
              id="vessel-prefix"
              value={draft.vesselPrefix}
              onChange={(e) => onChange({ vesselPrefix: e.target.value })}
              aria-label="Vessel prefix"
            />
          </Field>

          <Field>
            <FieldLabel htmlFor="boat-model">Boat Model</FieldLabel>
            <Input
              id="boat-model"
              value={draft.boatModel}
              onChange={(e) => onChange({ boatModel: e.target.value })}
              aria-label="Boat model"
            />
          </Field>
        </FormRow>
      </FormSection>

      <VesselParticularsForm />
    </SettingsLayout>
  )
}

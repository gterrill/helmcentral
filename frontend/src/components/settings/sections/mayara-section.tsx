import { FormRow, FormSection, SettingsLayout } from '@/components/patterns'
import { Field, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import type { RegularSettingsDraft } from '@/components/settings/settings-draft'

interface MayaraSectionProps {
  draft: RegularSettingsDraft
  onChange: (patch: Partial<RegularSettingsDraft>) => void
}

export function MayaraSection({ draft, onChange }: MayaraSectionProps) {
  return (
    <SettingsLayout title="Mayara">
      <FormSection
        title="Mayara Radar Server"
        description="This address is only needed for the radar picture overlay on the anchor watch map. ARPA radar targets need no configuration here: they already arrive through the SignalK plugin. That plugin does not proxy the spoke stream, so the picture needs a direct address to mayara-server."
      >
        <FormRow>
          <Field>
            <FieldLabel htmlFor="mayara-address">Address</FieldLabel>
            <Input
              id="mayara-address"
              value={draft.mayaraAddress}
              onChange={(e) => onChange({ mayaraAddress: e.target.value })}
              aria-label="Mayara address"
            />
          </Field>

          <Field>
            <FieldLabel htmlFor="mayara-port">Port</FieldLabel>
            <Input
              id="mayara-port"
              value={draft.mayaraPort}
              onChange={(e) => onChange({ mayaraPort: e.target.value })}
              aria-label="Mayara port"
            />
          </Field>
        </FormRow>
      </FormSection>
    </SettingsLayout>
  )
}

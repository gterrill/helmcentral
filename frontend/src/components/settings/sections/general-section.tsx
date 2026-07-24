import { Field, FieldLabel, FieldLegend, FieldSet } from '@/components/ui/field'
import { Select, SelectItem, SelectPopup, SelectTrigger, SelectValue } from '@/components/ui/select'
import type { RegularSettingsDraft } from '@/components/settings/settings-draft'

interface GeneralSectionProps {
  draft: RegularSettingsDraft
  onChange: (patch: Partial<RegularSettingsDraft>) => void
}

export function GeneralSection({ draft, onChange }: GeneralSectionProps) {
  return (
    <div className="mx-auto max-w-3xl space-y-4 rounded-lg border bg-background/60 p-4">
      <FieldSet>
        <FieldLegend variant="label">Units</FieldLegend>
        <div className="mt-3 grid grid-cols-1 gap-2 md:grid-cols-2">
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
        </div>
      </FieldSet>
    </div>
  )
}

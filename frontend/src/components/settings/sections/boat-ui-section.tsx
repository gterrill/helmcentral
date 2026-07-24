import { Field, FieldLabel, FieldLegend, FieldSet } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import type { RegularSettingsDraft } from '@/components/settings/settings-draft'

interface BoatUiSectionProps {
  draft: RegularSettingsDraft
  onChange: (patch: Partial<RegularSettingsDraft>) => void
}

export function BoatUiSection({ draft, onChange }: BoatUiSectionProps) {
  return (
    <div className="mx-auto max-w-3xl space-y-4 rounded-lg border bg-background/60 p-4">
      <FieldSet>
        <FieldLegend variant="label">Vessel</FieldLegend>
        <div className="mt-3 grid grid-cols-1 gap-2 md:grid-cols-2">
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
        </div>
      </FieldSet>

      <FieldSet>
        <FieldLegend variant="label">Power</FieldLegend>
        <div className="mt-3 grid grid-cols-1 gap-2 md:grid-cols-2">
          <Field>
            <FieldLabel htmlFor="battery-ah">Battery Ah</FieldLabel>
            <Input
              id="battery-ah"
              value={draft.houseBatteryCapacityAh}
              onChange={(e) => onChange({ houseBatteryCapacityAh: e.target.value })}
              aria-label="House battery capacity"
            />
          </Field>
        </div>
      </FieldSet>
    </div>
  )
}

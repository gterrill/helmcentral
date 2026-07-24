import { FieldDescription, FieldLegend, FieldSet } from '@/components/ui/field'
import { SecretFieldGroup } from '@/components/settings/secret-field-group'

export function GeonamesSection() {
  return (
    <div className="mx-auto max-w-3xl space-y-4 rounded-lg border bg-background/60 p-4">
      <FieldSet>
        <FieldLegend variant="label">GeoNames</FieldLegend>
        <FieldDescription>Used for reverse place-name lookups near the vessel&apos;s position.</FieldDescription>
        <div className="mt-3">
          <SecretFieldGroup fields={[{ key: 'GEONAMES_USERNAME', label: 'GeoNames Username' }]} />
        </div>
      </FieldSet>
    </div>
  )
}

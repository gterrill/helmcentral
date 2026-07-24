import { Field, FieldLabel, FieldLegend, FieldSet } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import { SecretFieldGroup } from '@/components/settings/secret-field-group'
import type { RegularSettingsDraft } from '@/components/settings/settings-draft'

interface InfluxdbSectionProps {
  draft: RegularSettingsDraft
  onChange: (patch: Partial<RegularSettingsDraft>) => void
}

export function InfluxdbSection({ draft, onChange }: InfluxdbSectionProps) {
  return (
    <div className="mx-auto max-w-3xl space-y-4 rounded-lg border bg-background/60 p-4">
      <FieldSet>
        <FieldLegend variant="label">InfluxDB (optional)</FieldLegend>
        <div className="mt-3 space-y-3">
          <Field orientation="horizontal">
            <Switch checked={draft.influxdbEnabled} onCheckedChange={(checked) => onChange({ influxdbEnabled: checked })} />
            <FieldLabel>Use InfluxDB for wind-gust and depth-trend history instead of the built-in in-memory buffer</FieldLabel>
          </Field>

          <div className="grid grid-cols-1 gap-2 md:grid-cols-3">
            <Field>
              <FieldLabel htmlFor="influxdb-url">URL</FieldLabel>
              <Input
                id="influxdb-url"
                value={draft.influxdbUrl}
                onChange={(e) => onChange({ influxdbUrl: e.target.value })}
                aria-label="InfluxDB URL"
              />
            </Field>

            <Field>
              <FieldLabel htmlFor="influxdb-org">Org</FieldLabel>
              <Input
                id="influxdb-org"
                value={draft.influxdbOrg}
                onChange={(e) => onChange({ influxdbOrg: e.target.value })}
                aria-label="InfluxDB organization"
              />
            </Field>

            <Field>
              <FieldLabel htmlFor="influxdb-bucket">Bucket</FieldLabel>
              <Input
                id="influxdb-bucket"
                value={draft.influxdbBucket}
                onChange={(e) => onChange({ influxdbBucket: e.target.value })}
                aria-label="InfluxDB bucket"
              />
            </Field>
          </div>
        </div>
      </FieldSet>

      <FieldSet>
        <FieldLegend variant="label">InfluxDB Token</FieldLegend>
        <div className="mt-3">
          <SecretFieldGroup fields={[{ key: 'INFLUXDB_TOKEN', label: 'InfluxDB Token' }]} />
        </div>
      </FieldSet>
    </div>
  )
}

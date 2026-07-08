import { FieldSet, FieldLegend, FieldGroup, Field, FieldLabel, Input } from 'helmcentral-dashboard'

export function Default() {
  return (
    <FieldSet>
      <FieldLegend>Tide station</FieldLegend>
      <FieldGroup>
        <Field>
          <FieldLabel htmlFor="tide-port">Reference port</FieldLabel>
          <Input id="tide-port" defaultValue="Auckland" />
        </Field>
      </FieldGroup>
    </FieldSet>
  )
}

export function LabelVariant() {
  return (
    <FieldSet>
      <FieldLegend variant="label">Wind source</FieldLegend>
      <FieldGroup>
        <Field>
          <FieldLabel htmlFor="wind-mast">Masthead sensor</FieldLabel>
          <Input id="wind-mast" defaultValue="NMEA2000 bus 1" />
        </Field>
      </FieldGroup>
    </FieldSet>
  )
}

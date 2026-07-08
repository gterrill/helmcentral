import { FieldSet, FieldLegend, FieldGroup, Field, FieldLabel, Input } from 'helmcentral-dashboard'

export function Default() {
  return (
    <FieldSet>
      <FieldLegend>Anchor alarm</FieldLegend>
      <FieldGroup>
        <Field>
          <FieldLabel htmlFor="anchor-radius-set">Watch radius</FieldLabel>
          <Input id="anchor-radius-set" defaultValue="30m" />
        </Field>
        <Field>
          <FieldLabel htmlFor="anchor-sensitivity">Sensitivity</FieldLabel>
          <Input id="anchor-sensitivity" defaultValue="High" />
        </Field>
      </FieldGroup>
    </FieldSet>
  )
}

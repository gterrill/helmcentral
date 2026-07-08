import { Field, FieldGroup, FieldLabel, FieldDescription, Input } from 'helmcentral-dashboard'

export function Default() {
  return (
    <FieldGroup>
      <Field>
        <FieldLabel htmlFor="depth-alarm">Shallow depth alarm</FieldLabel>
        <Input id="depth-alarm" defaultValue="2.5m" />
      </Field>
      <Field>
        <FieldLabel htmlFor="deep-alarm">Deep depth alarm</FieldLabel>
        <Input id="deep-alarm" defaultValue="40m" />
        <FieldDescription>Alerts when depth exceeds this reading.</FieldDescription>
      </Field>
    </FieldGroup>
  )
}

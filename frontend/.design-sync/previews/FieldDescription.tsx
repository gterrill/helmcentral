import { Field, FieldLabel, FieldDescription, Input } from 'helmcentral-dashboard'

export function Default() {
  return (
    <Field>
      <FieldLabel htmlFor="anchor-radius">Watch radius</FieldLabel>
      <Input id="anchor-radius" defaultValue="30m" />
      <FieldDescription>Alert triggers once the vessel drifts past this distance.</FieldDescription>
    </Field>
  )
}

export function ShortNote() {
  return (
    <Field>
      <FieldLabel htmlFor="callsign">Callsign</FieldLabel>
      <Input id="callsign" defaultValue="ZMK4821" />
      <FieldDescription>Shown on AIS broadcasts.</FieldDescription>
    </Field>
  )
}

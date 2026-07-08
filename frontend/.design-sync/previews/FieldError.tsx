import { Field, FieldLabel, FieldError, Input } from 'helmcentral-dashboard'

export function Default() {
  return (
    <Field data-invalid="true">
      <FieldLabel htmlFor="watch-radius">Watch radius (m)</FieldLabel>
      <Input id="watch-radius" defaultValue="-5" aria-invalid />
      <FieldError errors={[{ message: 'Radius must be a positive number.' }]} />
    </Field>
  )
}

export function MultipleErrors() {
  return (
    <Field data-invalid="true">
      <FieldLabel htmlFor="mmsi-invalid">MMSI</FieldLabel>
      <Input id="mmsi-invalid" defaultValue="12" aria-invalid />
      <FieldError
        errors={[
          { message: 'MMSI must be exactly 9 digits.' },
          { message: 'MMSI cannot start with 0.' },
        ]}
      />
    </Field>
  )
}

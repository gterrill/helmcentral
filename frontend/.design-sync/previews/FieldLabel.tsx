import { Field, FieldLabel, Input } from 'helmcentral-dashboard'

export function Default() {
  return (
    <Field>
      <FieldLabel htmlFor="vessel-name">Vessel name</FieldLabel>
      <Input id="vessel-name" defaultValue="Windlass" />
    </Field>
  )
}

export function Required() {
  return (
    <Field>
      <FieldLabel htmlFor="mmsi">
        MMSI <span style={{ color: 'var(--destructive, #dc2626)' }}>*</span>
      </FieldLabel>
      <Input id="mmsi" placeholder="9 digits" />
    </Field>
  )
}

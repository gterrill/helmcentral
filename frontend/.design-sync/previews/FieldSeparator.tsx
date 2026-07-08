import { FieldGroup, Field, FieldLabel, FieldSeparator, Input } from 'helmcentral-dashboard'

export function WithLabel() {
  return (
    <FieldGroup>
      <Field>
        <FieldLabel htmlFor="route-name">Route name</FieldLabel>
        <Input id="route-name" defaultValue="Marina to Anchorage" />
      </Field>
      <FieldSeparator>or</FieldSeparator>
      <Field>
        <FieldLabel htmlFor="route-waypoint">Import from waypoint</FieldLabel>
        <Input id="route-waypoint" placeholder="Select waypoint" />
      </Field>
    </FieldGroup>
  )
}

export function Plain() {
  return (
    <FieldGroup>
      <Field>
        <FieldLabel htmlFor="fuel-tank-1">Tank 1 level</FieldLabel>
        <Input id="fuel-tank-1" defaultValue="72%" />
      </Field>
      <FieldSeparator />
      <Field>
        <FieldLabel htmlFor="fuel-tank-2">Tank 2 level</FieldLabel>
        <Input id="fuel-tank-2" defaultValue="65%" />
      </Field>
    </FieldGroup>
  )
}

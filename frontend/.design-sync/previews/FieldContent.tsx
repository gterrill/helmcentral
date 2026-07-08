import { FieldLabel, FieldContent, FieldTitle, FieldDescription, Switch } from 'helmcentral-dashboard'

export function Default() {
  return (
    <FieldLabel htmlFor="night-mode">
      <FieldContent>
        <FieldTitle>Night mode display</FieldTitle>
        <FieldDescription>Dims the chartplotter for low-light watches.</FieldDescription>
      </FieldContent>
      <Switch id="night-mode" defaultChecked />
    </FieldLabel>
  )
}

export function TitleOnly() {
  return (
    <FieldLabel htmlFor="ais-transmit">
      <FieldContent>
        <FieldTitle>AIS transmit</FieldTitle>
      </FieldContent>
      <Switch id="ais-transmit" />
    </FieldLabel>
  )
}

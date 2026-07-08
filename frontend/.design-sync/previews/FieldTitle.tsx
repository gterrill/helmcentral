import { FieldLabel, FieldContent, FieldTitle, FieldDescription, Switch } from 'helmcentral-dashboard'

export function Default() {
  return (
    <FieldLabel htmlFor="autopilot-standby">
      <FieldContent>
        <FieldTitle>Autopilot standby chime</FieldTitle>
        <FieldDescription>Plays a tone when autopilot disengages.</FieldDescription>
      </FieldContent>
      <Switch id="autopilot-standby" defaultChecked />
    </FieldLabel>
  )
}

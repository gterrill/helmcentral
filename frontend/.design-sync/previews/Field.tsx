import {
  Field,
  FieldContent,
  FieldDescription,
  FieldError,
  FieldGroup,
  FieldLabel,
  FieldLegend,
  FieldSeparator,
  FieldSet,
  FieldTitle,
  Input,
  Switch,
} from 'helmcentral-dashboard'

export function Default() {
  return (
    <Field>
      <FieldLabel htmlFor="email">Email</FieldLabel>
      <Input id="email" type="email" placeholder="you@example.com" />
      <FieldDescription>We'll never share your email with anyone else.</FieldDescription>
    </Field>
  )
}

export function WithError() {
  return (
    <Field data-invalid="true">
      <FieldLabel htmlFor="email-invalid">Email</FieldLabel>
      <Input id="email-invalid" type="email" defaultValue="not-an-email" aria-invalid />
      <FieldError errors={[{ message: 'Enter a valid email address.' }]} />
    </Field>
  )
}

export function Horizontal() {
  return (
    <Field orientation="horizontal">
      <FieldLabel htmlFor="units">Distance units</FieldLabel>
      <Input id="units" defaultValue="Nautical miles" />
    </Field>
  )
}

export function ToggleRow() {
  return (
    <FieldLabel htmlFor="marketing-emails">
      <FieldContent>
        <FieldTitle>Marketing emails</FieldTitle>
        <FieldDescription>Receive occasional updates about new features.</FieldDescription>
      </FieldContent>
      <Switch id="marketing-emails" />
    </FieldLabel>
  )
}

export function FieldSetGroup() {
  return (
    <FieldSet>
      <FieldLegend>Notifications</FieldLegend>
      <FieldGroup>
        <FieldLabel htmlFor="push">
          <FieldContent>
            <FieldTitle>Push notifications</FieldTitle>
            <FieldDescription>Get notified on this device.</FieldDescription>
          </FieldContent>
          <Switch id="push" defaultChecked />
        </FieldLabel>
        <FieldSeparator>or</FieldSeparator>
        <FieldLabel htmlFor="email-notifications">
          <FieldContent>
            <FieldTitle>Email notifications</FieldTitle>
            <FieldDescription>Get a daily summary by email.</FieldDescription>
          </FieldContent>
          <Switch id="email-notifications" />
        </FieldLabel>
      </FieldGroup>
    </FieldSet>
  )
}

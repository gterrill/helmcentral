import { Switch } from 'helmcentral-dashboard'

export function Unchecked() {
  return <Switch aria-label="Enable autopilot" />
}

export function Checked() {
  return <Switch defaultChecked aria-label="Enable autopilot" />
}

export function Disabled() {
  return (
    <div style={{ display: 'flex', gap: 16 }}>
      <Switch disabled aria-label="Disabled unchecked" />
      <Switch disabled defaultChecked aria-label="Disabled checked" />
    </div>
  )
}

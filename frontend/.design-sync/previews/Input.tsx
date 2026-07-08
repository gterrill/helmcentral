import { Input } from 'helmcentral-dashboard'

export function Default() {
  return <Input defaultValue="Serenity" style={{ width: 240 }} />
}

export function Placeholder() {
  return <Input placeholder="Enter vessel name" style={{ width: 240 }} />
}

export function Disabled() {
  return <Input defaultValue="Auckland Marina" disabled style={{ width: 240 }} />
}

export function Invalid() {
  return (
    <Input
      defaultValue="not-a-real-mmsi"
      aria-invalid
      style={{ width: 240 }}
      className="aria-[invalid=true]:border-destructive aria-[invalid=true]:ring-destructive/20"
    />
  )
}

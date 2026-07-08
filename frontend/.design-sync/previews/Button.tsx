import { Button } from 'helmcentral-dashboard'

export function Default() {
  return <Button>Save changes</Button>
}

export function Variants() {
  return (
    <div style={{ display: 'flex', gap: 12, flexWrap: 'wrap' }}>
      <Button variant="default">Default</Button>
      <Button variant="secondary">Secondary</Button>
      <Button variant="outline">Outline</Button>
      <Button variant="ghost">Ghost</Button>
    </div>
  )
}

export function Sizes() {
  return (
    <div style={{ display: 'flex', gap: 12, alignItems: 'center', flexWrap: 'wrap' }}>
      <Button size="sm">Small</Button>
      <Button size="default">Default</Button>
      <Button size="lg">Large</Button>
      <Button size="icon" aria-label="Add">
        +
      </Button>
    </div>
  )
}

export function Disabled() {
  return (
    <div style={{ display: 'flex', gap: 12 }}>
      <Button disabled>Save changes</Button>
      <Button variant="outline" disabled>
        Cancel
      </Button>
    </div>
  )
}

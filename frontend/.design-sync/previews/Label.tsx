import { Input, Label } from 'helmcentral-dashboard'

export function Default() {
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 6, width: 260 }}>
      <Label htmlFor="vessel-name">Vessel name</Label>
      <Input id="vessel-name" placeholder="Serenity" />
    </div>
  )
}

export function Required() {
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 6, width: 260 }}>
      <Label htmlFor="mmsi">
        MMSI number <span style={{ color: 'var(--destructive)' }}>*</span>
      </Label>
      <Input id="mmsi" placeholder="366123456" />
    </div>
  )
}

export function Disabled() {
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 6, width: 260 }}>
      <Label htmlFor="home-port" className="peer-disabled:cursor-not-allowed peer-disabled:opacity-70">
        Home port
      </Label>
      <Input id="home-port" defaultValue="Auckland" disabled className="peer" />
    </div>
  )
}

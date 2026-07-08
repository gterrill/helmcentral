import { Separator } from 'helmcentral-dashboard'

export function Horizontal() {
  return (
    <div style={{ width: 280 }}>
      <div style={{ fontSize: 14, fontWeight: 600 }}>Helmcentral</div>
      <div style={{ fontSize: 13, color: 'var(--muted-foreground)' }}>Marine dashboard</div>
      <Separator style={{ marginTop: 12, marginBottom: 12 }} />
      <div style={{ fontSize: 13, color: 'var(--muted-foreground)' }}>Wind · Depth · Position</div>
    </div>
  )
}

export function Vertical() {
  return (
    <div style={{ display: 'flex', height: 40, alignItems: 'center', gap: 12 }}>
      <span style={{ fontSize: 13 }}>Wind 12kts</span>
      <Separator orientation="vertical" />
      <span style={{ fontSize: 13 }}>Depth 4.2m</span>
      <Separator orientation="vertical" />
      <span style={{ fontSize: 13 }}>SOG 6.1kts</span>
    </div>
  )
}

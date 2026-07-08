import { Button, Popover, PopoverTrigger, PopoverContent } from 'helmcentral-dashboard'

export function Default() {
  return (
    <Popover defaultOpen>
      <PopoverTrigger
        render={<Button variant="outline">Set anchor radius</Button>}
      />
      <PopoverContent style={{ padding: 16, width: 240 }}>
        <p style={{ fontWeight: 600, marginBottom: 4 }}>Anchor watch radius</p>
        <p style={{ fontSize: 14 }}>Currently set to 30m. Drag the ring on the chart to adjust.</p>
      </PopoverContent>
    </Popover>
  )
}

export function Confirmation() {
  return (
    <Popover defaultOpen>
      <PopoverTrigger render={<Button variant="outline">Delete waypoint</Button>} />
      <PopoverContent style={{ padding: 16, width: 220 }}>
        <p style={{ fontWeight: 600, marginBottom: 8 }}>Delete this waypoint?</p>
        <div style={{ display: 'flex', gap: 8 }}>
          <Button variant="outline" size="sm">
            Cancel
          </Button>
          <Button size="sm">Delete</Button>
        </div>
      </PopoverContent>
    </Popover>
  )
}

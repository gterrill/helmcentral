import { Button, Popover, PopoverTrigger, PopoverContent } from 'helmcentral-dashboard'

export function Default() {
  return (
    <Popover defaultOpen>
      <PopoverTrigger render={<Button variant="outline">Wind details</Button>} />
      <PopoverContent style={{ padding: 16, width: 240 }}>
        <p style={{ fontWeight: 600, marginBottom: 4 }}>True wind</p>
        <p style={{ fontSize: 14 }}>14kts from 220°, gusting to 19kts.</p>
      </PopoverContent>
    </Popover>
  )
}

export function WithActions() {
  return (
    <Popover defaultOpen>
      <PopoverTrigger render={<Button variant="outline">Tide station</Button>} />
      <PopoverContent style={{ padding: 16, width: 260 }}>
        <p style={{ fontWeight: 600, marginBottom: 4 }}>Auckland reference port</p>
        <p style={{ fontSize: 14, marginBottom: 12 }}>Next high tide 14:52, 1.8m above datum.</p>
        <Button size="sm">Change station</Button>
      </PopoverContent>
    </Popover>
  )
}

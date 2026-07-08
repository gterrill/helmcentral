import { Button, Popover, PopoverTrigger, PopoverContent } from 'helmcentral-dashboard'

export function Default() {
  return (
    <Popover>
      <PopoverTrigger render={<Button variant="outline">Route options</Button>} />
      <PopoverContent style={{ padding: 16, width: 220 }}>
        <p>Route options menu.</p>
      </PopoverContent>
    </Popover>
  )
}

export function GhostTrigger() {
  return (
    <Popover>
      <PopoverTrigger render={<Button variant="ghost">Tank details</Button>} />
      <PopoverContent style={{ padding: 16, width: 200 }}>
        <p>Tank levels.</p>
      </PopoverContent>
    </Popover>
  )
}

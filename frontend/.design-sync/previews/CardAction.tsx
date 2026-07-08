import { Button, Card, CardHeader, CardTitle, CardDescription, CardAction } from 'helmcentral-dashboard'

export function Default() {
  return (
    <Card style={{ width: 340 }}>
      <CardHeader>
        <CardTitle>Anchor Watch</CardTitle>
        <CardDescription>Holding within a 12m radius.</CardDescription>
        <CardAction>
          <Button variant="ghost" size="sm">
            Reset
          </Button>
        </CardAction>
      </CardHeader>
    </Card>
  )
}

export function IconAction() {
  return (
    <Card style={{ width: 300 }}>
      <CardHeader>
        <CardTitle>Bilge Pump</CardTitle>
        <CardDescription>Auto mode enabled</CardDescription>
        <CardAction>
          <Button variant="outline" size="icon" aria-label="Pump settings">
            ⚙
          </Button>
        </CardAction>
      </CardHeader>
    </Card>
  )
}

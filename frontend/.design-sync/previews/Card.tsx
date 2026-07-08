import {
  Button,
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from 'helmcentral-dashboard'

export function Simple() {
  return (
    <Card style={{ width: 320 }}>
      <CardHeader>
        <CardTitle>Anchor Watch</CardTitle>
        <CardDescription>Monitoring drift radius from the drop point.</CardDescription>
      </CardHeader>
      <CardContent>
        <p>Vessel is holding within a 12m radius.</p>
      </CardContent>
    </Card>
  )
}

export function WithAction() {
  return (
    <Card style={{ width: 360 }}>
      <CardHeader>
        <CardTitle>Route to Marina</CardTitle>
        <CardDescription>4.2nm · ETA 38 min</CardDescription>
        <CardAction>
          <Button variant="ghost" size="sm">
            Edit
          </Button>
        </CardAction>
      </CardHeader>
      <CardContent>
        <p>Wind 12kts SW · Current favorable</p>
      </CardContent>
      <CardFooter>
        <Button size="sm">Start navigation</Button>
      </CardFooter>
    </Card>
  )
}

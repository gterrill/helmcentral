import { Button, Card, CardHeader, CardTitle, CardContent, CardFooter } from 'helmcentral-dashboard'

export function Default() {
  return (
    <Card style={{ width: 340 }}>
      <CardHeader>
        <CardTitle>Route to Marina</CardTitle>
      </CardHeader>
      <CardContent>
        <p>4.2nm · ETA 38 min</p>
      </CardContent>
      <CardFooter>
        <Button size="sm">Start navigation</Button>
      </CardFooter>
    </Card>
  )
}

export function TwoActions() {
  return (
    <Card style={{ width: 340 }}>
      <CardHeader>
        <CardTitle>Anchor Alarm Triggered</CardTitle>
      </CardHeader>
      <CardContent>
        <p>Vessel has drifted 18m outside the set radius.</p>
      </CardContent>
      <CardFooter style={{ gap: 8 }}>
        <Button variant="outline" size="sm">
          Snooze
        </Button>
        <Button size="sm">Acknowledge</Button>
      </CardFooter>
    </Card>
  )
}

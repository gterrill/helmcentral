import { Card, CardHeader, CardTitle, CardDescription } from 'helmcentral-dashboard'

export function Default() {
  return (
    <Card style={{ width: 320 }}>
      <CardHeader>
        <CardTitle>Wind &amp; Tide</CardTitle>
        <CardDescription>Live conditions at the mooring.</CardDescription>
      </CardHeader>
    </Card>
  )
}

export function TitleOnly() {
  return (
    <Card style={{ width: 280 }}>
      <CardHeader>
        <CardTitle>Fuel Tanks</CardTitle>
      </CardHeader>
    </Card>
  )
}

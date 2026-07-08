import { Card, CardHeader, CardTitle } from 'helmcentral-dashboard'

export function Default() {
  return (
    <Card style={{ width: 280 }}>
      <CardHeader>
        <CardTitle>Battery Bank</CardTitle>
      </CardHeader>
    </Card>
  )
}

export function LongTitle() {
  return (
    <Card style={{ width: 320 }}>
      <CardHeader>
        <CardTitle>Engine Room Temperature</CardTitle>
      </CardHeader>
    </Card>
  )
}

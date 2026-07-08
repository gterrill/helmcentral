import { Card, CardHeader, CardTitle, CardContent } from 'helmcentral-dashboard'

export function Default() {
  return (
    <Card style={{ width: 320 }}>
      <CardHeader>
        <CardTitle>Speed Over Ground</CardTitle>
      </CardHeader>
      <CardContent>
        <p style={{ fontSize: 28, fontWeight: 600 }}>6.4 kts</p>
        <p>Steady on heading 210°</p>
      </CardContent>
    </Card>
  )
}

export function TextOnly() {
  return (
    <Card style={{ width: 320 }}>
      <CardContent>
        <p>Rode paid out: 28m at a 4:1 scope. Windlass reports no faults.</p>
      </CardContent>
    </Card>
  )
}

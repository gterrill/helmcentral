import { Card, CardHeader, CardTitle, CardDescription } from 'helmcentral-dashboard'

export function Default() {
  return (
    <Card style={{ width: 320 }}>
      <CardHeader>
        <CardTitle>Tide Station</CardTitle>
        <CardDescription>Next high tide at 14:52, 1.8m above chart datum.</CardDescription>
      </CardHeader>
    </Card>
  )
}

export function ShortDescription() {
  return (
    <Card style={{ width: 260 }}>
      <CardHeader>
        <CardTitle>Depth</CardTitle>
        <CardDescription>4.6m under keel</CardDescription>
      </CardHeader>
    </Card>
  )
}

import { Select, SelectItem, SelectPopup, SelectTrigger, SelectValue } from 'helmcentral-dashboard'

export function Default() {
  return (
    <Select defaultValue="nm">
      <SelectTrigger aria-label="Distance units" style={{ width: 220 }}>
        <SelectValue />
      </SelectTrigger>
      <SelectPopup>
        <SelectItem value="nm">Nautical miles</SelectItem>
        <SelectItem value="km">Kilometres</SelectItem>
      </SelectPopup>
    </Select>
  )
}

export function Placeholder() {
  return (
    <Select>
      <SelectTrigger aria-label="Hull type" style={{ width: 220 }}>
        <SelectValue placeholder="Select hull type…" />
      </SelectTrigger>
      <SelectPopup>
        <SelectItem value="power_cat">power_cat</SelectItem>
        <SelectItem value="sail_mono">sail_mono</SelectItem>
      </SelectPopup>
    </Select>
  )
}

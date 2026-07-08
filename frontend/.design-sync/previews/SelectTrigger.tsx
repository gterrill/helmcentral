import { Select, SelectItem, SelectPopup, SelectTrigger, SelectValue } from 'helmcentral-dashboard'

export function Default() {
  return (
    <Select defaultValue="power_cat">
      <SelectTrigger aria-label="Hull type" style={{ width: 220 }}>
        <SelectValue />
      </SelectTrigger>
      <SelectPopup>
        <SelectItem value="power_cat">power_cat</SelectItem>
        <SelectItem value="sail_mono">sail_mono</SelectItem>
        <SelectItem value="power_mono">power_mono</SelectItem>
        <SelectItem value="sail_cat">sail_cat</SelectItem>
      </SelectPopup>
    </Select>
  )
}

export function Placeholder() {
  return (
    <Select>
      <SelectTrigger aria-label="Tide provider" style={{ width: 220 }}>
        <SelectValue placeholder="Select a tide provider…" />
      </SelectTrigger>
      <SelectPopup>
        <SelectItem value="bom">Bureau of Meteorology</SelectItem>
        <SelectItem value="linz">LINZ (NZ)</SelectItem>
      </SelectPopup>
    </Select>
  )
}

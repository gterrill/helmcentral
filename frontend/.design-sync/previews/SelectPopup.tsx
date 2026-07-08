import { Select, SelectItem, SelectPopup, SelectTrigger, SelectValue } from 'helmcentral-dashboard'

export function Default() {
  return (
    <Select defaultValue="bom" defaultOpen>
      <SelectTrigger aria-label="Tide provider" style={{ width: 240 }}>
        <SelectValue />
      </SelectTrigger>
      <SelectPopup>
        <SelectItem value="bom">Bureau of Meteorology</SelectItem>
        <SelectItem value="linz">LINZ (NZ)</SelectItem>
        <SelectItem value="noaa">NOAA</SelectItem>
        <SelectItem value="ukho">UKHO</SelectItem>
      </SelectPopup>
    </Select>
  )
}

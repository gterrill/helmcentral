import {
  Select,
  SelectGroup,
  SelectGroupLabel,
  SelectItem,
  SelectPopup,
  SelectSeparator,
  SelectTrigger,
  SelectValue,
} from 'helmcentral-dashboard'

export function TideProviders() {
  return (
    <Select defaultValue="bom" defaultOpen>
      <SelectTrigger aria-label="Tide provider" style={{ width: 240 }}>
        <SelectValue />
      </SelectTrigger>
      <SelectPopup>
        <SelectGroup>
          <SelectGroupLabel>Regional</SelectGroupLabel>
          <SelectItem value="bom">Bureau of Meteorology</SelectItem>
          <SelectItem value="linz">LINZ (NZ)</SelectItem>
        </SelectGroup>
      </SelectPopup>
    </Select>
  )
}

export function TwoGroups() {
  return (
    <Select defaultValue="nm" defaultOpen>
      <SelectTrigger aria-label="Units" style={{ width: 220 }}>
        <SelectValue />
      </SelectTrigger>
      <SelectPopup>
        <SelectGroup>
          <SelectGroupLabel>Distance</SelectGroupLabel>
          <SelectItem value="nm">Nautical miles</SelectItem>
          <SelectItem value="km">Kilometres</SelectItem>
        </SelectGroup>
        <SelectSeparator />
        <SelectGroup>
          <SelectGroupLabel>Speed</SelectGroupLabel>
          <SelectItem value="kts">Knots</SelectItem>
          <SelectItem value="kmh">km/h</SelectItem>
        </SelectGroup>
      </SelectPopup>
    </Select>
  )
}

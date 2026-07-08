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

export function BetweenGroups() {
  return (
    <Select defaultValue="nm" defaultOpen>
      <SelectTrigger aria-label="Units" style={{ width: 220 }}>
        <SelectValue />
      </SelectTrigger>
      <SelectPopup>
        <SelectGroup>
          <SelectGroupLabel>Metric</SelectGroupLabel>
          <SelectItem value="km">Kilometres</SelectItem>
          <SelectItem value="m">Metres</SelectItem>
        </SelectGroup>
        <SelectSeparator />
        <SelectGroup>
          <SelectGroupLabel>Imperial</SelectGroupLabel>
          <SelectItem value="nm">Nautical miles</SelectItem>
          <SelectItem value="ft">Feet</SelectItem>
        </SelectGroup>
      </SelectPopup>
    </Select>
  )
}

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

export function HullType() {
  return (
    <Select defaultValue="power_cat" defaultOpen>
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

export function DistanceUnitsGrouped() {
  return (
    <Select defaultValue="nm" defaultOpen>
      <SelectTrigger aria-label="Distance units" style={{ width: 220 }}>
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

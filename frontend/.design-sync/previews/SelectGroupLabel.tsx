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

export function Default() {
  return (
    <Select defaultValue="power_cat" defaultOpen>
      <SelectTrigger aria-label="Hull type" style={{ width: 220 }}>
        <SelectValue />
      </SelectTrigger>
      <SelectPopup>
        <SelectGroup>
          <SelectGroupLabel>Power</SelectGroupLabel>
          <SelectItem value="power_cat">power_cat</SelectItem>
          <SelectItem value="power_mono">power_mono</SelectItem>
        </SelectGroup>
        <SelectSeparator />
        <SelectGroup>
          <SelectGroupLabel>Sail</SelectGroupLabel>
          <SelectItem value="sail_mono">sail_mono</SelectItem>
          <SelectItem value="sail_cat">sail_cat</SelectItem>
        </SelectGroup>
      </SelectPopup>
    </Select>
  )
}

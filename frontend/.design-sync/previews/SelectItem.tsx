import { Select, SelectItem, SelectPopup, SelectTrigger, SelectValue } from 'helmcentral-dashboard'

export function Default() {
  return (
    <Select defaultValue="sail_mono" defaultOpen>
      <SelectTrigger aria-label="Hull type" style={{ width: 220 }}>
        <SelectValue />
      </SelectTrigger>
      <SelectPopup>
        <SelectItem value="power_cat">power_cat</SelectItem>
        <SelectItem value="sail_mono">sail_mono</SelectItem>
        <SelectItem value="power_mono">power_mono</SelectItem>
        <SelectItem value="sail_cat">sail_cat</SelectItem>
        <SelectItem value="trimaran" disabled>
          trimaran (unsupported)
        </SelectItem>
      </SelectPopup>
    </Select>
  )
}

import { Field, FieldLabel, FieldLegend, FieldSet } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Select, SelectItem, SelectPopup, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import type { HullType, RegularSettingsDraft } from '@/components/settings/settings-draft'

interface AnchorWatchOptionsSectionProps {
  draft: RegularSettingsDraft
  onChange: (patch: Partial<RegularSettingsDraft>) => void
  autoCloseAnchorWatchEnabled?: boolean
  onAutoCloseAnchorWatchToggle?: (enabled: boolean) => void
}

/**
 * Combines Anchor configuration (regular settings) and Anchor Watch options
 * (local-storage-backed toggle owned by App.tsx). The anchor fields go through
 * useSettingsForm/the settings API, while the auto-close toggle does not.
 */
export function AnchorWatchOptionsSection({
  draft,
  onChange,
  autoCloseAnchorWatchEnabled = true,
  onAutoCloseAnchorWatchToggle,
}: AnchorWatchOptionsSectionProps) {
  return (
    <div className="mx-auto max-w-3xl space-y-4 rounded-lg border bg-background/60 p-4">
      <FieldSet>
        <FieldLegend variant="label">Anchor</FieldLegend>
        <div className="mt-3 grid grid-cols-1 gap-2 md:grid-cols-2">
          <Field>
            <FieldLabel htmlFor="bow-roller-height">Bow Roller M</FieldLabel>
            <Input
              id="bow-roller-height"
              value={draft.bowRollerHeightM}
              onChange={(e) => onChange({ bowRollerHeightM: e.target.value })}
              aria-label="Bow roller height"
            />
          </Field>

          <Field>
            <FieldLabel htmlFor="chain-size">Chain Size Mm</FieldLabel>
            <Input
              id="chain-size"
              value={draft.chainSizeMm}
              onChange={(e) => onChange({ chainSizeMm: e.target.value })}
              aria-label="Chain size"
            />
          </Field>

          <Field>
            <FieldLabel htmlFor="chain-onboard">Chain Onboard M</FieldLabel>
            <Input
              id="chain-onboard"
              value={draft.chainOnboardM}
              onChange={(e) => onChange({ chainOnboardM: e.target.value })}
              aria-label="Chain onboard length"
            />
          </Field>

          <Field>
            <FieldLabel htmlFor="windage-area">Windage M2</FieldLabel>
            <Input
              id="windage-area"
              value={draft.windageAreaM2}
              onChange={(e) => onChange({ windageAreaM2: e.target.value })}
              aria-label="Windage area"
            />
          </Field>

          <Field className="md:col-span-2">
            <FieldLabel htmlFor="hull-type">Hull Type</FieldLabel>
            <Select value={draft.hullType} onValueChange={(value) => value && onChange({ hullType: value as HullType })}>
              <SelectTrigger id="hull-type" aria-label="Hull type">
                <SelectValue />
              </SelectTrigger>
              <SelectPopup>
                <SelectItem value="power_cat">power_cat</SelectItem>
                <SelectItem value="sail_mono">sail_mono</SelectItem>
                <SelectItem value="power_mono">power_mono</SelectItem>
                <SelectItem value="sail_cat">sail_cat</SelectItem>
              </SelectPopup>
            </Select>
          </Field>
        </div>
      </FieldSet>

      <FieldSet>
        <FieldLegend variant="label">Anchor Watch Options</FieldLegend>
        <div className="mt-3 space-y-3">
          <Field orientation="horizontal">
            <Switch
              checked={autoCloseAnchorWatchEnabled}
              onCheckedChange={(checked) => onAutoCloseAnchorWatchToggle?.(checked)}
            />
            <FieldLabel>
              Auto-close anchor watch when engines start (if outside circle for 5+ seconds)
            </FieldLabel>
          </Field>
        </div>
      </FieldSet>
    </div>
  )
}

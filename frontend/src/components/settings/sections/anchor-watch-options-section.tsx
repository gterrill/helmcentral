import { Field, FieldDescription, FieldLabel, FieldLegend, FieldSet } from '@/components/ui/field'
import { InputGroup, InputGroupAddon, InputGroupInput, InputGroupText } from '@/components/ui/input-group'
import { Select, SelectItem, SelectPopup, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import type { HullType, RegularSettingsDraft, ScopeMethod } from '@/components/settings/settings-draft'

interface AnchorWatchOptionsSectionProps {
  draft: RegularSettingsDraft
  onChange: (patch: Partial<RegularSettingsDraft>) => void
}

/**
 * Combines Anchor configuration and Anchor Watch options. Both go through
 * useSettingsForm/the settings API now (ADR 0099 moved auto-raise off the
 * browser's localStorage-backed toggle and onto a server setting, the same
 * anchor.auto_raise_on_motoring the auto-raise watcher itself reads).
 */
export function AnchorWatchOptionsSection({
  draft,
  onChange,
}: AnchorWatchOptionsSectionProps) {
  return (
    <div className="mx-auto max-w-3xl space-y-4 rounded-lg border bg-background/60 p-4">
      <FieldSet>
        <FieldLegend variant="label">Anchor</FieldLegend>
        <div className="mt-3 grid grid-cols-1 gap-2 md:grid-cols-2">
          <Field>
            <FieldLabel htmlFor="bow-roller-height">Bow Roller</FieldLabel>
            <InputGroup>
              <InputGroupInput
                id="bow-roller-height"
                inputMode="decimal"
                value={draft.bowRollerHeightM}
                onChange={(e) => onChange({ bowRollerHeightM: e.target.value })}
                aria-label="Bow roller height in metres"
              />
              <InputGroupAddon align="inline-end">
                <InputGroupText>m</InputGroupText>
              </InputGroupAddon>
            </InputGroup>
          </Field>

          <Field>
            <FieldLabel htmlFor="gps-from-bow">GPS aft of Bow Roller</FieldLabel>
            <InputGroup>
              <InputGroupInput
                id="gps-from-bow"
                inputMode="decimal"
                value={draft.gpsFromBowM}
                onChange={(e) => onChange({ gpsFromBowM: e.target.value })}
                aria-label="GPS antenna distance aft of bow roller in metres"
              />
              <InputGroupAddon align="inline-end">
                <InputGroupText>m</InputGroupText>
              </InputGroupAddon>
            </InputGroup>
          </Field>

          <Field>
            <FieldLabel htmlFor="loa">Length Overall</FieldLabel>
            <InputGroup>
              <InputGroupInput
                id="loa"
                inputMode="decimal"
                value={draft.loaM}
                onChange={(e) => onChange({ loaM: e.target.value })}
                aria-label="Length overall in metres"
              />
              <InputGroupAddon align="inline-end">
                <InputGroupText>m</InputGroupText>
              </InputGroupAddon>
            </InputGroup>
          </Field>

          <Field>
            <FieldLabel htmlFor="chain-size">Chain Size</FieldLabel>
            <InputGroup>
              <InputGroupInput
                id="chain-size"
                inputMode="decimal"
                value={draft.chainSizeMm}
                onChange={(e) => onChange({ chainSizeMm: e.target.value })}
                aria-label="Chain size in millimetres"
              />
              <InputGroupAddon align="inline-end">
                <InputGroupText>mm</InputGroupText>
              </InputGroupAddon>
            </InputGroup>
          </Field>

          <Field>
            <FieldLabel htmlFor="chain-onboard">Chain Onboard</FieldLabel>
            <InputGroup>
              <InputGroupInput
                id="chain-onboard"
                inputMode="decimal"
                value={draft.chainOnboardM}
                onChange={(e) => onChange({ chainOnboardM: e.target.value })}
                aria-label="Chain onboard length in metres"
              />
              <InputGroupAddon align="inline-end">
                <InputGroupText>m</InputGroupText>
              </InputGroupAddon>
            </InputGroup>
          </Field>

          <Field>
            <FieldLabel htmlFor="windage-area">Windage</FieldLabel>
            <InputGroup>
              <InputGroupInput
                id="windage-area"
                inputMode="decimal"
                value={draft.windageAreaM2}
                onChange={(e) => onChange({ windageAreaM2: e.target.value })}
                aria-label="Windage area in square metres"
              />
              <InputGroupAddon align="inline-end">
                <InputGroupText>m²</InputGroupText>
              </InputGroupAddon>
            </InputGroup>
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

          <Field className="md:col-span-2">
            <FieldLabel htmlFor="scope-method">Scope Method</FieldLabel>
            <Select value={draft.scopeMethod} onValueChange={(value) => value && onChange({ scopeMethod: value as ScopeMethod })}>
              <SelectTrigger id="scope-method" aria-label="Scope method">
                <SelectValue />
              </SelectTrigger>
              <SelectPopup>
                <SelectItem value="ratio">Ratio — 5:1, 7:1 in a blow</SelectItem>
                <SelectItem value="catenary">Catenary — chain, windage, hull</SelectItem>
              </SelectPopup>
            </Select>
            <FieldDescription>
              Method the Anchor Watch tile uses for its recommended rode.
            </FieldDescription>
          </Field>
        </div>
      </FieldSet>

      <FieldSet>
        <FieldLegend variant="label">Anchor Watch Options</FieldLegend>
        <div className="mt-3 space-y-3">
          <Field orientation="horizontal">
            <Switch
              checked={draft.autoRaiseOnMotoring}
              onCheckedChange={(checked) => onChange({ autoRaiseOnMotoring: checked })}
            />
            <FieldLabel>
              Auto-raise anchor watch when under way (engines running, outside the circle,
              3+ knots SOG for 15 seconds)
            </FieldLabel>
          </Field>
        </div>
      </FieldSet>
    </div>
  )
}

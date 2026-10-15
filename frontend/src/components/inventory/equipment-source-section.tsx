import type { Dispatch, SetStateAction } from 'react'

import { FormRow, FormSection } from '@/components/patterns'
import { ApplyGaugeZones } from '@/components/inventory/apply-gauge-zones'
import { Field, FieldDescription, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { InputGroup, InputGroupAddon, InputGroupInput, InputGroupText } from '@/components/ui/input-group'
import type { EquipmentInput } from '@/hooks/use-inventory'
import {
  SOURCE_KINDS,
  SOURCE_KIND_LABELS,
  sourceKindOf,
  type DiscoveryRow,
  type VesselDiscoveryResponse,
} from '@/lib/vessel-settings'

interface EquipmentSourceSectionProps {
  /** The saved item's id; null on a brand new draft. */
  id: string | null
  draft: EquipmentInput
  setDraft: Dispatch<SetStateAction<EquipmentInput>>
  discovery: VesselDiscoveryResponse
  /** The profile the item has saved, for Apply gauge zones. */
  savedProfileId: string
  /** The source the item has saved. Apply gauge zones needs it unchanged. */
  savedSource: string
  canWrite: boolean
}

function rowLabel(row: DiscoveryRow): string {
  const reading = row.metrics.map((m) => m.text).join(' · ')
  if (!row.reporting) return `${row.source} · not reporting`
  return reading === '' ? row.source : `${row.source} · ${reading}`
}

/**
 * The home of an equipment item's SignalK source: the one node the boat reports
 * that this item owns. An engine, a battery, an alternator, a solar controller,
 * a tank, a generator, a charger or an inverter owns one; other gear owns none.
 * The list is what the boat is reporting now plus whatever this item already
 * owns, so a source cannot be typed in. A source another item owns is shown
 * but cannot be taken from it here.
 *
 * A battery item also carries its bank's capacity, cell count and optional
 * voltage overrides, which no instrument reports reliably.
 */
export function EquipmentSourceSection({ id, draft, setDraft, discovery, savedProfileId, savedSource, canWrite }: EquipmentSourceSectionProps) {
  const kind = sourceKindOf(draft.signalk_source)
  const known = discovery.groups.some((g) => g.rows.some((r) => r.source === draft.signalk_source))
  const noData = !discovery.connected && discovery.groups.length === 0

  const pick = (source: string) => {
    setDraft((prev) => {
      const next = { ...prev, signalk_source: source }
      // Bank figures belong to a battery; the server refuses them anywhere else.
      if (sourceKindOf(source) !== 'battery') {
        next.capacity_ah = 0
        next.cells = 0
        next.warn_voltage = 0
        next.high_voltage = 0
      }
      return next
    })
  }

  const number = (value: string, parse: (v: string) => number): number => {
    const parsed = parse(value)
    return Number.isFinite(parsed) && parsed > 0 ? parsed : 0
  }

  return (
    <FormSection
      title="SignalK source"
      description="The one thing the boat reports that this item is: an engine, a battery, a tank and so on. Other gear leaves it empty. The item's name is what tiles, alarms and Mate call the source."
    >
      <FieldGroup>
        <Field>
          <FieldLabel htmlFor="equipment-signalk-source">Source</FieldLabel>
          <select
            id="equipment-signalk-source"
            className="h-9 w-full min-w-0 rounded-md border bg-transparent px-3 text-sm"
            value={draft.signalk_source}
            disabled={!canWrite}
            onChange={(e) => pick(e.target.value)}
          >
            <option value="">None</option>
            {SOURCE_KINDS.map((k) => {
              const group = discovery.groups.find((g) => g.kind === k)
              if (!group || group.rows.length === 0) return null
              return (
                <optgroup key={k} label={SOURCE_KIND_LABELS[k]}>
                  {group.rows.map((row) => {
                    const takenBy = row.item && row.item.id !== id ? row.item.name : null
                    return (
                      <option key={row.source} value={row.source} disabled={takenBy !== null}>
                        {takenBy ? `${row.source} · assigned to ${takenBy}` : rowLabel(row)}
                      </option>
                    )
                  })}
                </optgroup>
              )
            })}
            {draft.signalk_source !== '' && !known && <option value={draft.signalk_source}>{draft.signalk_source} · not reporting</option>}
          </select>
          {noData ? (
            <FieldDescription>Helmcentral has no data from SignalK yet, so there is nothing to pick from. Check the connection in Settings.</FieldDescription>
          ) : (
            <FieldDescription>Only sources the boat reports can be chosen. A source belongs to one item.</FieldDescription>
          )}
        </Field>

        {kind === 'battery' && (
          <FormRow>
            <Field>
              <FieldLabel htmlFor="equipment-bank-cells">Cells</FieldLabel>
              <Input
                id="equipment-bank-cells"
                inputMode="numeric"
                value={draft.cells || ''}
                disabled={!canWrite}
                onChange={(e) => setDraft((p) => ({ ...p, cells: Math.trunc(number(e.target.value, Number.parseInt.bind(Number) as (v: string) => number)) }))}
                aria-label="Bank cell count"
              />
            </Field>
            <Field>
              <FieldLabel htmlFor="equipment-bank-capacity">Capacity</FieldLabel>
              <InputGroup>
                <InputGroupInput
                  id="equipment-bank-capacity"
                  inputMode="decimal"
                  value={draft.capacity_ah || ''}
                  disabled={!canWrite}
                  onChange={(e) => setDraft((p) => ({ ...p, capacity_ah: number(e.target.value, Number.parseFloat) }))}
                  aria-label="Bank capacity in amp-hours"
                />
                <InputGroupAddon align="inline-end">
                  <InputGroupText>Ah</InputGroupText>
                </InputGroupAddon>
              </InputGroup>
            </Field>
            <Field>
              <FieldLabel htmlFor="equipment-bank-warn">Warn override</FieldLabel>
              <InputGroup>
                <InputGroupInput
                  id="equipment-bank-warn"
                  inputMode="decimal"
                  value={draft.warn_voltage || ''}
                  placeholder="profile"
                  disabled={!canWrite}
                  onChange={(e) => setDraft((p) => ({ ...p, warn_voltage: number(e.target.value, Number.parseFloat) }))}
                  aria-label="Pack warn voltage override"
                />
                <InputGroupAddon align="inline-end">
                  <InputGroupText>V</InputGroupText>
                </InputGroupAddon>
              </InputGroup>
            </Field>
            <Field>
              <FieldLabel htmlFor="equipment-bank-high">High override</FieldLabel>
              <InputGroup>
                <InputGroupInput
                  id="equipment-bank-high"
                  inputMode="decimal"
                  value={draft.high_voltage || ''}
                  placeholder="profile"
                  disabled={!canWrite}
                  onChange={(e) => setDraft((p) => ({ ...p, high_voltage: number(e.target.value, Number.parseFloat) }))}
                  aria-label="Pack high voltage override"
                />
                <InputGroupAddon align="inline-end">
                  <InputGroupText>V</InputGroupText>
                </InputGroupAddon>
              </InputGroup>
            </Field>
          </FormRow>
        )}
        {kind === 'battery' && (
          <FieldDescription>
            Cells and capacity describe this bank; no instrument reports them reliably. Leave the overrides empty to use the profile's
            thresholds, multiplied out by the cell count.
          </FieldDescription>
        )}

        {canWrite && id !== null && kind === 'engine' && savedProfileId !== '' && draft.signalk_source === savedSource && (
          <div>
            <ApplyGaugeZones profileId={savedProfileId} source={savedSource} />
          </div>
        )}
      </FieldGroup>
    </FormSection>
  )
}

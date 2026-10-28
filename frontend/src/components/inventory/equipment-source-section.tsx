import type { Dispatch, SetStateAction } from 'react'

import { FormRow, FormSection } from '@/components/patterns'
import { Field, FieldDescription, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { InputGroup, InputGroupAddon, InputGroupInput, InputGroupText } from '@/components/ui/input-group'
import { sourceOwner } from '@/lib/equipment-source-take'
import type { EquipmentInput } from '@/hooks/use-inventory'
import { useVesselStateSelector } from '@/hooks/use-vessel-state'
import { selectTimeZone } from '@/lib/vessel-time'
import {
  SOURCE_KINDS,
  SOURCE_KIND_LABELS,
  discoveryRowFor,
  notReportingText,
  signatureWarning,
  trackingNote,
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
  canWrite: boolean
}

function rowLabel(row: DiscoveryRow, timeZone: string | undefined): string {
  const reading = row.metrics.map((m) => m.text).join(' · ')
  if (!row.reporting) return `${row.source} · ${notReportingText(row, timeZone)}`
  return reading === '' ? row.source : `${row.source} · ${reading}`
}

/**
 * The home of an equipment item's SignalK source: the one node the boat reports
 * that this item owns. An engine, a battery, an alternator, a solar controller,
 * a tank, a generator, a charger or an inverter owns one; other gear owns none.
 * The list is what the boat is reporting now plus whatever this item already
 * owns, so a source cannot be typed in. A source another item owns is listed
 * with its reading and its owner; choosing it stages a swap or move, which the
 * editor confirms in its Save bar. A new item cannot take one.
 *
 * A battery item also carries its bank's capacity, cell count and optional
 * voltage overrides, which no instrument reports reliably.
 */
export function EquipmentSourceSection({ id, draft, setDraft, discovery, canWrite }: EquipmentSourceSectionProps) {
  const kind = sourceKindOf(draft.signalk_source)
  const known = discovery.groups.some((g) => g.rows.some((r) => r.source === draft.signalk_source))
  const timeZone = useVesselStateSelector(selectTimeZone)
  const warning = signatureWarning(discoveryRowFor(discovery, draft.signalk_source))
  const noData = !discovery.connected && discovery.groups.length === 0

  const pick = (source: string) => {
    const owner = sourceOwner(discovery, source)
    setDraft((prev) => {
      const next = { ...prev, signalk_source: source }
      // Choosing a source another item owns asks the server to take it from
      // that item when this one is saved.
      if (owner && owner.id !== id) next.take_source_from = owner.id
      else delete next.take_source_from
      // Bank figures belong to a battery; the server refuses them anywhere else.
      if (sourceKindOf(source) !== 'battery') {
        next.capacity_ah = 0
        next.cells = 0
        next.warn_voltage = 0
        next.high_voltage = 0
      }
      // Tank levels belong to a tank, and the server refuses them anywhere else.
      if (sourceKindOf(source) !== 'tank') {
        next.tank_warn_percent = 0
        next.tank_alarm_percent = 0
        next.tank_alarms_off = false
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
      description="The one thing the boat reports that this item is: an engine, a battery, a tank and so on. Other gear leaves it empty. Tiles showing this item follow its source, and its name is what tiles, alarms and Mate call it."
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
                    // A new item has no saved record to swap with, so it cannot take one.
                    return (
                      <option key={row.source} value={row.source} disabled={takenBy !== null && id === null}>
                        {takenBy ? `${rowLabel(row, timeZone)} · on ${takenBy}` : rowLabel(row, timeZone)}
                      </option>
                    )
                  })}
                </optgroup>
              )
            })}
            {draft.signalk_source !== '' && !known && <option value={draft.signalk_source}>{draft.signalk_source} · not reporting now</option>}
          </select>
          {noData ? (
            <FieldDescription>Helmcentral has no data from SignalK yet, so there is nothing to pick from. Check the connection in Settings.</FieldDescription>
          ) : (
            <FieldDescription>
              Only sources that report what this kind of item needs, now or when Helmcentral last saw them, can be chosen. A source belongs to one item: pick one that another item has to swap with it, or to move it here.
            </FieldDescription>
          )}
          {warning !== '' && <p role="alert" className="text-sm text-destructive">{warning}</p>}
          {!noData && trackingNote(discovery.tracking) !== '' && (
            <FieldDescription>{trackingNote(discovery.tracking)}</FieldDescription>
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

      </FieldGroup>
    </FormSection>
  )
}

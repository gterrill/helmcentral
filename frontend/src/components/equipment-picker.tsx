import { useState } from 'react'

import { Button } from '@/components/ui/button'
import { Field, FieldDescription, FieldLabel } from '@/components/ui/field'
import { useVesselDiscovery } from '@/hooks/use-vessel-discovery'
import { fetchEquipment, toEquipmentInput, updateEquipment } from '@/hooks/use-inventory'
import { formatAppLocation } from '@/lib/app-location'
import { candidateItems, candidateLabels } from '@/lib/equipment-candidates'
import type { EquipmentTileContext } from '@/lib/equipment-tiles'
import { SOURCE_KIND_NOUNS, discoveryRows, type SourceKind } from '@/lib/vessel-settings'

const SELECT_CLASS = 'h-9 w-full rounded-md border bg-transparent px-3 text-sm'

interface EquipmentPickerProps {
  /** Namespaces the element ids. */
  idPrefix: string
  label: string
  /** The kind of equipment this tile can show; items mapped to anything else are not offered. */
  kind: SourceKind
  /** The chosen item's id, '' for none. */
  value: string
  onChange: (id: string) => void
  ctx: EquipmentTileContext
}

/**
 * Chooses the equipment item a tile shows (ADR 0185). Mapped items of the right
 * kind are listed first. An item that is not mapped to any source yet is also
 * offered, and choosing one opens a picker over what the boat reports that no
 * item owns, so the item can be mapped without leaving the dialog. Mapping
 * changes the item itself, for every tile and feature that uses it, and the
 * notice says so. It is the same field as the source on the item's own page.
 */
export function EquipmentPicker({ idPrefix, label, kind, value, onChange, ctx }: EquipmentPickerProps) {
  const mapped = ctx.items.filter((i) => i.source_kind === kind)
  const chosen = ctx.items.find((i) => i.id === value)
  // Items with no source are offered only if they could be this kind of
  // equipment (an engine for an engine tile), never the whole inventory. The one
  // already chosen stays listed so the field never goes blank under the operator.
  const unmapped = candidateItems(kind, ctx.items, ctx.profiles)
  if (chosen && chosen.signalk_source === '' && !unmapped.some((i) => i.id === chosen.id)) unmapped.unshift(chosen)
  const labels = candidateLabels(unmapped)
  const noun = SOURCE_KIND_NOUNS[kind]

  return (
    <div className="grid gap-2">
      <Field>
        <FieldLabel htmlFor={`${idPrefix}-equipment`}>{label}</FieldLabel>
        <select
          id={`${idPrefix}-equipment`}
          className={SELECT_CLASS}
          value={chosen ? chosen.id : ''}
          onChange={(e) => onChange(e.target.value)}
        >
          <option value="">Pick {noun}…</option>
          {chosen && chosen.signalk_source !== '' && chosen.source_kind !== kind && (
            <option value={chosen.id}>{chosen.name} (not {noun})</option>
          )}
          {mapped.length > 0 && (
            <optgroup label="Mapped">
              {mapped.map((i) => <option key={i.id} value={i.id}>{i.name}</option>)}
            </optgroup>
          )}
          {unmapped.length > 0 && (
            <optgroup label="Not mapped yet">
              {unmapped.map((i) => <option key={i.id} value={i.id}>{labels.get(i.id) ?? i.name}</option>)}
            </optgroup>
          )}
        </select>
        {mapped.length === 0 && unmapped.length === 0 && (
          <FieldDescription data-testid="no-candidates">
            No {noun} is set up yet. Add one in{' '}
            <a className="underline" href={formatAppLocation({ panel: 'inventory', route: { view: 'index', section: 'equipment' } }, { firstPageId: null })}>Inventory</a>
            , or create one from{' '}
            <a className="underline" href={formatAppLocation({ panel: 'settings', route: { view: 'section', section: 'signalk' } }, { firstPageId: null })}>Settings, SignalK, Discovery</a>.
          </FieldDescription>
        )}
      </Field>
      {chosen && chosen.signalk_source === '' && <MapHere itemId={chosen.id} itemName={chosen.name} kind={kind} idPrefix={idPrefix} />}
    </div>
  )
}

function MapHere({ itemId, itemName, kind, idPrefix }: { itemId: string; itemName: string; kind: SourceKind; idPrefix: string }) {
  const { discovery, loading } = useVesselDiscovery(true)
  const [source, setSource] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const rows = discoveryRows(discovery, kind).filter((r) => r.item === null && r.reporting)

  const map = async () => {
    setBusy(true)
    setError(null)
    try {
      const item = await fetchEquipment(itemId)
      await updateEquipment(item.id, { ...toEquipmentInput(item), signalk_source: source })
      setSource('')
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Request failed')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div data-testid="map-here" className="grid gap-2 rounded-md border border-border p-3">
      <p className="text-sm">{itemName} is not mapped to anything the boat reports, so a tile cannot read it yet.</p>
      <Field>
        <FieldLabel htmlFor={`${idPrefix}-map-source`}>Map {itemName} to</FieldLabel>
        <select id={`${idPrefix}-map-source`} className={SELECT_CLASS} value={source} onChange={(e) => setSource(e.target.value)}>
          <option value="">{loading ? 'Looking…' : rows.length === 0 ? 'Nothing unmapped is reporting' : 'Choose what it is…'}</option>
          {rows.map((r) => (
            <option key={r.source} value={r.source}>
              {r.source}{r.metrics.length > 0 ? ` · ${r.metrics.map((m) => m.text).join(' · ')}` : ''}
            </option>
          ))}
        </select>
        <FieldDescription>
          This changes {itemName} itself, for every tile and feature that uses it: alarms, maintenance hours and Mate read the same source.
        </FieldDescription>
      </Field>
      {error && <p role="alert" className="text-sm text-destructive">{error}</p>}
      <Button type="button" variant="secondary" className="w-fit" disabled={source === '' || busy} onClick={() => { void map() }}>
        Map {itemName}
      </Button>
    </div>
  )
}

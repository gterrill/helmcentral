import { useMemo, useState } from 'react'

import { FormSection } from '@/components/patterns'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import type { EngineProfile } from '@/lib/engine-profiles'
import { useOptionalAppRouter } from '@/hooks/use-app-location'
import {
  BLANK_DRAFT,
  createEquipment,
  fetchEquipment,
  toEquipmentInput,
  updateEquipment,
  useEquipment,
  type EquipmentItem,
} from '@/hooks/use-inventory'
import { useEquipmentProfiles } from '@/hooks/use-equipment-profiles'
import { useVesselDiscovery } from '@/hooks/use-vessel-discovery'
import { useVesselTimeZone } from '@/hooks/use-vessel-identity'
import { candidateItems, candidateLabels } from '@/lib/equipment-candidates'
import {
  SOURCE_KINDS,
  SOURCE_KIND_LABELS,
  SOURCE_KIND_NOUNS,
  systemForKind,
  notReportingText,
  signatureWarning,
  trackingNote,
  type DiscoveryGroup,
  type DiscoveryRow,
  type SourceKind,
} from '@/lib/vessel-settings'

/**
 * Settings -> SignalK -> Discovery: what the boat reports, grouped by kind,
 * with the equipment item that owns each source. A source is assigned to an
 * existing item or becomes a new one (a name and a kind, nothing else). The
 * item then carries the name every tile, alarm and Mate answer uses for it.
 *
 * Assigning and creating write the item at once, like every other change made
 * on an item's own page; nothing here waits for the Save bar.
 */
export function SignalKDiscoverySection() {
  const { discovery, loading, error } = useVesselDiscovery()
  const { items } = useEquipment({})

  const { profiles } = useEquipmentProfiles(true)

  const groups = useMemo(
    () => SOURCE_KINDS
      .map((kind) => discovery.groups.find((g) => g.kind === kind))
      .filter((g): g is DiscoveryGroup => g !== undefined && g.rows.length > 0),
    [discovery.groups],
  )

  // With no connection to SignalK nothing is being reported, but the mappings
  // already saved are still real: list them, by kind, each marked Not reporting,
  // so they never seem to have vanished.
  const savedGroups = useMemo(
    () => SOURCE_KINDS
      .map((kind): DiscoveryGroup => ({
        kind,
        mappable: true,
        signature_label: '',
        rows: items
          .filter((item) => item.signalk_source !== '' && item.source_kind === kind)
          .map((item): DiscoveryRow => ({
            source: item.signalk_source,
            instance: item.signalk_source.split('.').slice(1).join('.'),
            bus_name: '',
            charger_input: false,
            reporting: false,
            metrics: [],
            item: { id: item.id, name: item.name },
            last_seen: null,
            signature: 'unknown',
          })),
      }))
      .filter((g) => g.rows.length > 0),
    [items],
  )
  const shownGroups = discovery.connected ? groups : savedGroups

  return (
    <FormSection
      title="Discovery"
      description="What the boat reports, by kind. Assign each source to an equipment item, or create one for it: the item gives the source its name on tiles, alarms and in Mate's answers. A source no item owns shows up under its number."
    >
      {loading && <p className="text-sm text-muted-foreground">Loading…</p>}
      {error && <p role="alert" className="text-sm text-destructive">Could not read what the boat reports: {error}</p>}
      {!loading && !error && trackingNote(discovery.tracking) !== '' && (
        <p role="status" className="text-sm text-muted-foreground">{trackingNote(discovery.tracking)}</p>
      )}
      {!loading && !error && !discovery.connected && (
        <p className="text-sm text-muted-foreground">
          Helmcentral has no data from SignalK yet. Discovery lists what the boat reports once the connection above is working.
        </p>
      )}
      {!loading && !error && discovery.connected && groups.length === 0 && (
        <p id="discovery-engine" tabIndex={-1} className="text-sm text-muted-foreground outline-none focus-visible:ring-2 focus-visible:ring-ring">The boat is not reporting any engines, batteries, tanks or other equipment yet.</p>
      )}
      {/* The setup checklist lands here for engines, so the anchor exists even
          when the boat reports none: here when other kinds are listed, on the
          general empty message when nothing is. */}
      {!loading && !error && discovery.connected && groups.length > 0 && !groups.some((g) => g.kind === 'engine') && (
        <div id="discovery-engine" tabIndex={-1} className="flex flex-col gap-2 outline-none focus-visible:ring-2 focus-visible:ring-ring">
          <h3 className="text-xs font-semibold uppercase tracking-[0.12em] text-muted-foreground">{SOURCE_KIND_LABELS.engine}</h3>
          <p className="text-sm text-muted-foreground">The boat isn&apos;t reporting any engines.</p>
        </div>
      )}
      {shownGroups.map((group) => (
        <div key={group.kind} id={`discovery-${group.kind}`} tabIndex={-1} className="flex flex-col gap-2 outline-none focus-visible:ring-2 focus-visible:ring-ring">
          <h3 className="text-xs font-semibold uppercase tracking-[0.12em] text-muted-foreground">
            {SOURCE_KIND_LABELS[group.kind as SourceKind]}
          </h3>
          {group.rows.map((row) => (
            <DiscoveryRowView key={row.source} row={row} kind={group.kind as SourceKind} signatureLabel={group.signature_label} items={items} profiles={profiles} />
          ))}
        </div>
      ))}
    </FormSection>
  )
}

interface DiscoveryRowViewProps {
  row: DiscoveryRow
  kind: SourceKind
  signatureLabel: string
  items: EquipmentItem[]
  profiles: EngineProfile[]
}

function DiscoveryRowView({ row, kind, signatureLabel, items, profiles }: DiscoveryRowViewProps) {
  // Only items that could be this kind of equipment, not the whole inventory.
  const unassigned = useMemo(
    () => candidateItems(kind, items, profiles).sort((a, b) => a.name.localeCompare(b.name)),
    [kind, items, profiles],
  )
  const labels = useMemo(() => candidateLabels(unassigned), [unassigned])
  const router = useOptionalAppRouter()
  const [mode, setMode] = useState<'idle' | 'assign' | 'create'>('idle')
  const [pickedId, setPickedId] = useState('')
  const [newName, setNewName] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const warning = signatureWarning({ row, label: signatureLabel })
  const timeZone = useVesselTimeZone()
  const reading = row.metrics.map((m) => `${m.label} ${m.text}`).join(' · ')

  const run = async (action: () => Promise<unknown>) => {
    setBusy(true)
    setError(null)
    try {
      await action()
      setMode('idle')
      setPickedId('')
      setNewName('')
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Request failed')
    } finally {
      setBusy(false)
    }
  }

  const assign = () => run(async () => {
    const item = await fetchEquipment(pickedId)
    await updateEquipment(item.id, { ...toEquipmentInput(item), signalk_source: row.source })
  })

  const create = () => run(() => createEquipment({
    ...BLANK_DRAFT,
    name: newName.trim(),
    system: systemForKind(kind),
    signalk_source: row.source,
  }))

  const openItem = () => {
    if (!row.item || !router) return
    router.navigate({ panel: 'inventory', route: { view: 'equipment', id: row.item.id } })
  }

  return (
    <div className="grid min-w-0 grid-cols-1 gap-2 rounded-md border border-border/60 bg-background/40 p-3 sm:grid-cols-[minmax(0,1fr)_minmax(0,1fr)]">
      <div className="min-w-0">
        {/* A source that reports nothing is secondary: it is usually a bare
            numbered duplicate of an engine that does carry figures. */}
        <p className={`truncate font-mono text-xs ${row.reporting && row.metrics.length === 0 ? 'text-muted-foreground' : 'text-foreground'}`}>{row.source}</p>
        <p className="mt-1 truncate text-xs tabular-nums text-muted-foreground">
          {row.reporting ? (reading === '' ? 'Reporting, no figures yet' : reading) : sentenceCase(notReportingText(row, timeZone))}
        </p>
        {warning !== '' && <p role="alert" className="text-xs text-destructive">{warning}</p>}
        {kind === 'battery' && row.reporting && (
          <p className="truncate text-xs text-muted-foreground">
            {row.bus_name ? `The boat calls it ${row.bus_name}` : 'The boat gives it no name'}
            {row.charger_input ? ' · Charger input, not a battery' : ''}
          </p>
        )}
      </div>

      <div className="flex min-w-0 flex-col items-start gap-2 sm:items-end">
        {row.item ? (
          <div className="flex min-w-0 items-center gap-2">
            <span className="min-w-0 truncate text-sm font-medium">{row.item.name}</span>
            {router && (
              <Button type="button" variant="ghost" size="sm" onClick={openItem} aria-label={`Open ${row.item.name}`}>
                Open
              </Button>
            )}
          </div>
        ) : mode === 'idle' ? (
          <div className="flex flex-wrap gap-2">
            <Button type="button" variant="outline" size="sm" onClick={() => setMode('assign')} disabled={unassigned.length === 0}>
              Assign to item
            </Button>
            <Button type="button" variant="outline" size="sm" onClick={() => setMode('create')}>
              Create item
            </Button>
          </div>
        ) : mode === 'assign' ? (
          <div className="flex w-full min-w-0 flex-wrap items-center gap-2 sm:justify-end">
            <select
              className="h-8 min-w-0 flex-1 rounded-md border bg-transparent px-2 text-sm"
              aria-label={`Item for ${row.source}`}
              value={pickedId}
              onChange={(e) => setPickedId(e.target.value)}
            >
              <option value="">Choose an item…</option>
              {unassigned.map((item) => <option key={item.id} value={item.id}>{labels.get(item.id) ?? item.name}</option>)}
            </select>
            <Button type="button" size="sm" onClick={assign} disabled={busy || pickedId === ''}>Assign</Button>
            <Button type="button" variant="ghost" size="sm" onClick={() => setMode('idle')} disabled={busy}>Cancel</Button>
          </div>
        ) : (
          <div className="flex w-full min-w-0 flex-wrap items-center gap-2 sm:justify-end">
            <Input
              className="h-8 min-w-0 flex-1 text-sm"
              aria-label={`Name for the new ${SOURCE_KIND_NOUNS[kind]} item`}
              placeholder={`Name, such as ${exampleName(kind)}`}
              value={newName}
              onChange={(e) => setNewName(e.target.value)}
            />
            <Button type="button" size="sm" onClick={create} disabled={busy || newName.trim() === ''}>Create</Button>
            <Button type="button" variant="ghost" size="sm" onClick={() => setMode('idle')} disabled={busy}>Cancel</Button>
          </div>
        )}
        {error && <p role="alert" className="text-xs text-destructive">{error}</p>}
      </div>
    </div>
  )
}

function sentenceCase(text: string): string {
  return text.charAt(0).toUpperCase() + text.slice(1)
}

function exampleName(kind: SourceKind): string {
  switch (kind) {
    case 'engine': return 'Port engine'
    case 'battery': return 'House bank'
    case 'alternator': return 'Port alternator'
    case 'solar': return 'Port array'
    case 'tank': return 'Port fuel'
    case 'generator': return 'Generator'
    case 'charger': return 'Shore charger'
    case 'inverter': return 'Inverter'
  }
}

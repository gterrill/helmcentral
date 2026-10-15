import { useMemo, useState } from 'react'

import { FormSection } from '@/components/patterns'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
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
import { useVesselDiscovery } from '@/hooks/use-vessel-discovery'
import {
  SOURCE_KINDS,
  SOURCE_KIND_LABELS,
  SOURCE_KIND_NOUNS,
  systemForKind,
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

  const unassigned = useMemo(
    () => items.filter((item) => item.signalk_source === '').sort((a, b) => a.name.localeCompare(b.name)),
    [items],
  )

  const groups = useMemo(
    () => SOURCE_KINDS
      .map((kind) => discovery.groups.find((g) => g.kind === kind))
      .filter((g): g is DiscoveryGroup => g !== undefined && g.rows.length > 0),
    [discovery.groups],
  )

  return (
    <FormSection
      title="Discovery"
      description="What the boat reports, by kind. Assign each source to an equipment item, or create one for it: the item gives the source its name on tiles, alarms and in Mate's answers. A source no item owns shows up under its number."
    >
      {loading && <p className="text-sm text-muted-foreground">Loading…</p>}
      {error && <p role="alert" className="text-sm text-destructive">Could not read what the boat reports: {error}</p>}
      {!loading && !error && !discovery.connected && (
        <p className="text-sm text-muted-foreground">
          Helmcentral has no data from SignalK yet. Discovery lists what the boat reports once the connection above is working.
        </p>
      )}
      {!loading && !error && discovery.connected && groups.length === 0 && (
        <p className="text-sm text-muted-foreground">The boat is not reporting any engines, batteries, tanks or other equipment yet.</p>
      )}
      {discovery.connected && groups.map((group) => (
        <div key={group.kind} className="flex flex-col gap-2">
          <h3 className="text-xs font-semibold uppercase tracking-[0.12em] text-muted-foreground">
            {SOURCE_KIND_LABELS[group.kind as SourceKind]}
          </h3>
          {group.rows.map((row) => (
            <DiscoveryRowView key={row.source} row={row} kind={group.kind as SourceKind} unassigned={unassigned} />
          ))}
        </div>
      ))}
    </FormSection>
  )
}

interface DiscoveryRowViewProps {
  row: DiscoveryRow
  kind: SourceKind
  unassigned: EquipmentItem[]
}

function DiscoveryRowView({ row, kind, unassigned }: DiscoveryRowViewProps) {
  const router = useOptionalAppRouter()
  const [mode, setMode] = useState<'idle' | 'assign' | 'create'>('idle')
  const [pickedId, setPickedId] = useState('')
  const [newName, setNewName] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

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
        <p className="truncate font-mono text-xs text-foreground">{row.source}</p>
        <p className="mt-1 truncate text-xs tabular-nums text-muted-foreground">
          {row.reporting ? (reading === '' ? 'Reporting, no figures yet' : reading) : 'Not reporting'}
        </p>
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
              {unassigned.map((item) => <option key={item.id} value={item.id}>{item.name}</option>)}
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

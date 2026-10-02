import { useMemo, useState } from 'react'
import { Layers, MapPin, Plus, Printer } from 'lucide-react'
import type { ColumnDef } from '@tanstack/react-table'

import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { DeckPlan } from '@/components/inventory/deck-plan'
import { PrintBinLabelsDialog } from '@/components/inventory/label-print'
import { EmptyState, IndexFilters, IndexTable, Page } from '@/components/patterns'
import { useInventoryDecks, useInventoryZones, type InventoryZone } from '@/hooks/use-inventory'

// ADR 0142: the Locations index - a name and a bin count per location, built
// on the pattern library like EquipmentIndex. A location is an area of the
// boat (a "zone" in the code). Rows open the location's own page.
//
// "New location" asks for a name in a small dialog rather than opening a
// blank draft page: a location is only a name, and the server assigns its
// id, so there is no draft worth a page of its own. Creating it opens its
// page straight away, where the bins are added.

const columns: ColumnDef<InventoryZone, unknown>[] = [
  {
    accessorKey: 'name',
    header: 'Name',
    cell: ({ row }) => <span className="font-medium text-foreground">{row.original.name}</span>,
  },
  {
    id: 'bins',
    accessorFn: (zone) => zone.bins.length,
    header: 'Bins',
    cell: ({ getValue }) => <span className="tabular-nums text-muted-foreground">{getValue<number>()}</span>,
  },
]

export type LocationsView = 'table' | 'plan'

interface LocationsIndexProps {
  onOpenLocation: (id: string) => void
  canWrite?: boolean
  /** ADR 0156: Table (the list) or Plan (the deck plans). The caller keeps it
   * in the URL. */
  view?: LocationsView
  onViewChange?: (view: LocationsView) => void
  /** The deck tab open in the Plan view; the first deck with a plan when null. */
  planDeckId?: string | null
  onPlanDeckChange?: (deckId: string) => void
  /** A pin on the plan opens its bin page. */
  onOpenBin?: (code: string) => void
  /** The Decks list, where plans are added and locations drawn on them. */
  onOpenDecks?: () => void
}

function ViewSwitch({ view, onChange }: { view: LocationsView; onChange: (view: LocationsView) => void }) {
  return (
    <div role="group" aria-label="Locations view" className="inline-flex gap-1 rounded-md bg-muted p-1">
      {(['table', 'plan'] as const).map((v) => (
        <Button
          key={v}
          type="button"
          size="sm"
          variant={view === v ? 'default' : 'ghost'}
          aria-pressed={view === v}
          onClick={() => onChange(v)}
        >
          {v === 'table' ? 'Table' : 'Plan'}
        </Button>
      ))}
    </div>
  )
}

function PlanView({
  zones, planDeckId, onPlanDeckChange, onOpenLocation, onOpenBin, onOpenDecks, canWrite,
}: {
  zones: InventoryZone[]
  planDeckId: string | null
  onPlanDeckChange: (deckId: string) => void
  onOpenLocation: (id: string) => void
  onOpenBin: (code: string) => void
  onOpenDecks: () => void
  canWrite: boolean
}) {
  const { decks, loading, error, refresh } = useInventoryDecks()
  const planned = decks.filter((d) => d.plan_document_id)
  const deck = planned.find((d) => d.id === planDeckId) ?? planned[0] ?? null

  if (error) {
    return (
      <div className="flex flex-col items-start gap-2">
        <p role="alert" className="text-sm text-destructive">{error}</p>
        <Button type="button" variant="outline" size="sm" onClick={() => { void refresh() }}>Retry</Button>
      </div>
    )
  }
  if (loading && decks.length === 0) return <p className="text-sm text-muted-foreground">Loading...</p>
  if (!deck) {
    return (
      <EmptyState
        icon={<Layers className="h-8 w-8 text-muted-foreground" aria-hidden="true" />}
        title="No deck plans yet"
        description="Add a picture of each deck, outline the locations on it and pin the bins. Then you can find them by looking at the boat."
        action={canWrite ? (
          <Button type="button" onClick={onOpenDecks} className="mt-2">Add a deck plan</Button>
        ) : undefined}
      />
    )
  }
  return (
    <div className="flex min-w-0 flex-col gap-3">
      {planned.length > 1 && (
        <div role="group" aria-label="Decks" className="flex flex-wrap gap-1">
          {planned.map((d) => (
            <Button
              key={d.id}
              type="button"
              size="sm"
              variant={d.id === deck.id ? 'default' : 'outline'}
              aria-pressed={d.id === deck.id}
              onClick={() => onPlanDeckChange(d.id)}
            >
              {d.name}
            </Button>
          ))}
        </div>
      )}
      <DeckPlan deck={deck} zones={zones} onOpenZone={onOpenLocation} onOpenBin={onOpenBin} />
    </div>
  )
}

export function LocationsIndex({
  onOpenLocation,
  canWrite = true,
  view = 'table',
  onViewChange = () => {},
  planDeckId = null,
  onPlanDeckChange = () => {},
  onOpenBin = () => {},
  onOpenDecks = () => {},
}: LocationsIndexProps) {
  const { zones, loading, error, refresh, createZone } = useInventoryZones()
  const [query, setQuery] = useState('')
  const [creating, setCreating] = useState(false)
  const [printingLabels, setPrintingLabels] = useState(false)
  const [newName, setNewName] = useState('')
  const [saving, setSaving] = useState(false)
  const [createError, setCreateError] = useState<string | null>(null)

  const rows = useMemo(() => {
    const q = query.trim().toLowerCase()
    return q === '' ? zones : zones.filter((z) => z.name.toLowerCase().includes(q))
  }, [zones, query])

  const openCreate = () => {
    setNewName('')
    setCreateError(null)
    setCreating(true)
  }

  const handleCreate = async () => {
    const name = newName.trim()
    if (name === '') return
    setSaving(true)
    setCreateError(null)
    try {
      const zone = await createZone(name)
      setCreating(false)
      onOpenLocation(zone.id)
    } catch (err) {
      // AGENTS.md fallback policy: the server's own message (a duplicate
      // name, say), never an invented one.
      setCreateError(err instanceof Error ? err.message : String(err))
    } finally {
      setSaving(false)
    }
  }

  return (
    <Page
      title="Locations"
      primaryAction={
        canWrite
          ? { label: 'New location', icon: <Plus className="h-4 w-4" aria-hidden="true" />, onClick: openCreate }
          : undefined
      }
      secondaryActions={[
        { label: 'Decks', icon: <Layers className="h-4 w-4" aria-hidden="true" />, onClick: onOpenDecks },
        { label: 'Print bin labels', icon: <Printer className="h-4 w-4" aria-hidden="true" />, onClick: () => setPrintingLabels(true) },
      ]}
    >
      <div className="flex h-full min-h-0 flex-col gap-4">
        <div className="flex flex-wrap items-center gap-2">
          <ViewSwitch view={view} onChange={onViewChange} />
          {view === 'table' && (
            <div className="min-w-0 flex-1">
              <IndexFilters
                searchValue={query}
                onSearchChange={setQuery}
                searchLabel="Search locations"
                searchPlaceholder="Name..."
              />
            </div>
          )}
        </div>

        {view === 'plan' ? (
          <PlanView
            zones={zones}
            planDeckId={planDeckId}
            onPlanDeckChange={onPlanDeckChange}
            onOpenLocation={onOpenLocation}
            onOpenBin={onOpenBin}
            onOpenDecks={onOpenDecks}
            canWrite={canWrite}
          />
        ) : (
        <IndexTable
          columns={columns}
          rows={rows}
          getRowId={(zone) => zone.id}
          onOpen={(zone) => onOpenLocation(zone.id)}
          loading={loading && zones.length === 0}
          error={error}
          onRetry={() => { void refresh() }}
          empty={
            <EmptyState
              icon={<MapPin className="h-8 w-8 text-muted-foreground" aria-hidden="true" />}
              title={zones.length === 0 ? 'No locations yet' : 'No locations match'}
              description={
                zones.length === 0
                  ? 'A location is an area of the boat: salon, engine room, lazarette. Bins and gear are filed in them.'
                  : undefined
              }
              action={
                canWrite && zones.length === 0 ? (
                  <Button type="button" onClick={openCreate} className="mt-2">
                    <Plus className="h-4 w-4" aria-hidden="true" />
                    New location
                  </Button>
                ) : undefined
              }
            />
          }
        />
        )}
      </div>

      <PrintBinLabelsDialog
        open={printingLabels}
        onOpenChange={setPrintingLabels}
        bins={zones.flatMap((z) => z.bins.map((b) => ({ id: b.id, code: b.code, zoneName: z.name })))}
      />

      <Dialog open={creating} onOpenChange={(open) => { if (!saving) setCreating(open) }}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>New location</DialogTitle>
            <DialogDescription>Name an area of the boat. You add its bins on the next page.</DialogDescription>
          </DialogHeader>
          <Input
            aria-label="Location name"
            placeholder="Lazarette"
            value={newName}
            autoFocus
            onChange={(e) => setNewName(e.target.value)}
            onKeyDown={(e) => { if (e.key === 'Enter') { e.preventDefault(); void handleCreate() } }}
          />
          {createError && <p role="alert" className="text-sm text-destructive">{createError}</p>}
          <DialogFooter>
            <Button type="button" variant="outline" disabled={saving} onClick={() => setCreating(false)}>Cancel</Button>
            <Button type="button" disabled={saving || newName.trim() === ''} onClick={() => { void handleCreate() }}>
              {saving ? 'Creating…' : 'Create'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </Page>
  )
}

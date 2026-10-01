import { useMemo, useState } from 'react'
import { MapPin, Plus, Printer } from 'lucide-react'
import type { ColumnDef } from '@tanstack/react-table'

import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { PrintBinLabelsDialog } from '@/components/inventory/label-print'
import { EmptyState, IndexFilters, IndexTable, Page } from '@/components/patterns'
import { useInventoryZones, type InventoryZone } from '@/hooks/use-inventory'

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

interface LocationsIndexProps {
  onOpenLocation: (id: string) => void
  canWrite?: boolean
}

export function LocationsIndex({ onOpenLocation, canWrite = true }: LocationsIndexProps) {
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
        { label: 'Print bin labels', icon: <Printer className="h-4 w-4" aria-hidden="true" />, onClick: () => setPrintingLabels(true) },
      ]}
    >
      <div className="flex h-full min-h-0 flex-col gap-4">
        <IndexFilters
          searchValue={query}
          onSearchChange={setQuery}
          searchLabel="Search locations"
          searchPlaceholder="Name..."
        />

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

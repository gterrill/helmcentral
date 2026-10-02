import { useEffect, useMemo, useState } from 'react'
import { Layers, Plus } from 'lucide-react'
import type { ColumnDef } from '@tanstack/react-table'

import { Breadcrumb, BreadcrumbItem, BreadcrumbLink, BreadcrumbList, BreadcrumbPage, BreadcrumbSeparator } from '@/components/ui/breadcrumb'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { prefetchDeckPlanEditor } from '@/components/inventory/deck-plan-editor'
import { EmptyState, IndexTable, Page } from '@/components/patterns'
import { useInventoryDecks, useInventoryZones, type InventoryDeck } from '@/hooks/use-inventory'

// ADR 0156: the Decks list under Locations - one row per deck plan, built on
// the pattern library like LocationsIndex. A deck is a plan image the
// locations are outlined on. "New deck" asks for a name in a small dialog and
// opens the deck's page, where the plan image is added.

interface DeckRow extends InventoryDeck {
  zoneCount: number
}

const columns: ColumnDef<DeckRow, unknown>[] = [
  {
    accessorKey: 'name',
    header: 'Name',
    cell: ({ row }) => <span className="font-medium text-foreground">{row.original.name}</span>,
  },
  {
    id: 'plan',
    accessorFn: (deck) => (deck.plan_document_id ? 'Yes' : 'No'),
    header: 'Plan',
    cell: ({ getValue }) => <span className="text-muted-foreground">{getValue<string>()}</span>,
  },
  {
    accessorKey: 'zoneCount',
    header: 'Locations',
    cell: ({ getValue }) => <span className="tabular-nums text-muted-foreground">{getValue<number>()}</span>,
  },
]

interface DecksIndexProps {
  onOpenDeck: (id: string) => void
  /** The breadcrumb's Locations: up to the Locations index. */
  onOpenLocations: () => void
  canWrite?: boolean
}

export function DecksIndex({ onOpenDeck, onOpenLocations, canWrite = true }: DecksIndexProps) {
  const { decks, loading, error, refresh, createDeck } = useInventoryDecks()
  const { zones } = useInventoryZones()
  const [creating, setCreating] = useState(false)
  const [newName, setNewName] = useState('')
  const [saving, setSaving] = useState(false)
  const [createError, setCreateError] = useState<string | null>(null)

  // Every route to a deck page starts here, so warm the drawing surface now.
  useEffect(() => { prefetchDeckPlanEditor() }, [])

  const rows = useMemo<DeckRow[]>(
    () => decks.map((d) => ({ ...d, zoneCount: zones.filter((z) => z.deck_id === d.id).length })),
    [decks, zones],
  )

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
      const deck = await createDeck(name)
      setCreating(false)
      onOpenDeck(deck.id)
    } catch (err) {
      // AGENTS.md fallback policy: the server's own message, never an invented one.
      setCreateError(err instanceof Error ? err.message : String(err))
    } finally {
      setSaving(false)
    }
  }

  return (
    <Page
      title="Decks"
      breadcrumb={
        <Breadcrumb>
          <BreadcrumbList>
            <BreadcrumbItem>
              <BreadcrumbLink href="#" onClick={(e) => { e.preventDefault(); onOpenLocations() }}>Locations</BreadcrumbLink>
            </BreadcrumbItem>
            <BreadcrumbSeparator />
            <BreadcrumbItem>
              <BreadcrumbPage>Decks</BreadcrumbPage>
            </BreadcrumbItem>
          </BreadcrumbList>
        </Breadcrumb>
      }
      primaryAction={
        canWrite
          ? { label: 'New deck', icon: <Plus className="h-4 w-4" aria-hidden="true" />, onClick: openCreate }
          : undefined
      }
    >
      <IndexTable
        columns={columns}
        rows={rows}
        getRowId={(deck) => deck.id}
        onOpen={(deck) => onOpenDeck(deck.id)}
        loading={loading && decks.length === 0}
        error={error}
        onRetry={() => { void refresh() }}
        empty={
          <EmptyState
            icon={<Layers className="h-8 w-8 text-muted-foreground" aria-hidden="true" />}
            title="No decks yet"
            description="Add a picture of a deck, such as the builder's general arrangement drawing, then outline the locations on it and pin the bins."
            action={
              canWrite ? (
                <Button type="button" onClick={openCreate} className="mt-2">
                  <Plus className="h-4 w-4" aria-hidden="true" />
                  New deck
                </Button>
              ) : undefined
            }
          />
        }
      />

      <Dialog open={creating} onOpenChange={(open) => { if (!saving) setCreating(open) }}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>New deck</DialogTitle>
            <DialogDescription>Name the deck. You add its plan image on the next page.</DialogDescription>
          </DialogHeader>
          <Input
            aria-label="Deck name"
            placeholder="Main deck"
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

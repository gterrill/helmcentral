import { useState } from 'react'
import { Plus, Wrench } from 'lucide-react'
import type { ColumnDef } from '@tanstack/react-table'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  ConfirmDelete,
  EmptyState,
  IndexFilters,
  IndexTable,
  Page,
  type IndexFilter,
  type RowAction,
} from '@/components/patterns'
import {
  EQUIPMENT_SYSTEMS,
  EQUIPMENT_SYSTEM_LABELS,
  deleteEquipment,
  useEquipment,
  useInventoryZones,
  type EquipmentFilter,
  type EquipmentItem,
  type EquipmentStatus,
  type EquipmentSystem,
} from '@/hooks/use-inventory'

// ADR 0142: the Equipment index, rebuilt on the CRUD pattern library
// (components/patterns) in place of the hand-rolled shell this used to copy
// from wall-displays-panel.tsx. The search/filter state below and its wiring
// into useEquipment(filter) - server-side filtering - is unchanged from
// before this rewrite; IndexTable is what adds client-side column sort, the
// per-row action menu, and the phone-width card list on top of whatever flat
// `items` list that hook returns. Grouping by system in enum order (not
// alphabetically - the plan's own rule, matching how the gear aboard reads
// as blocks) is also unchanged, now expressed as IndexTable's groupBy/
// groupOrder rather than a hand-rolled loop over EQUIPMENT_SYSTEMS.

function locationLabel(item: EquipmentItem): string {
  if (item.zone_name && item.bin_code) return `${item.zone_name} / ${item.bin_code}`
  if (item.zone_name) return item.zone_name
  return '--'
}

const columns: ColumnDef<EquipmentItem, unknown>[] = [
  {
    accessorKey: 'name',
    header: 'Name',
    cell: ({ row }) => <span className="font-medium text-foreground">{row.original.name}</span>,
  },
  {
    id: 'manufacturer',
    accessorFn: (item) => [item.manufacturer, item.model].filter(Boolean).join(' ') || '--',
    header: 'Manufacturer / Model',
    cell: ({ getValue }) => <span className="text-muted-foreground">{getValue<string>()}</span>,
  },
  {
    id: 'location',
    accessorFn: locationLabel,
    header: 'Location',
    cell: ({ getValue }) => <span className="text-muted-foreground">{getValue<string>()}</span>,
  },
  {
    accessorKey: 'status',
    header: 'Status',
    cell: ({ row }) => (
      <Badge variant={row.original.status === 'deployed' ? 'secondary' : 'outline'}>
        {row.original.status === 'deployed' ? 'Deployed' : 'Stored'}
      </Badge>
    ),
  },
  {
    accessorKey: 'link_count',
    header: 'Documents',
    cell: ({ row }) => (
      <span className="text-right tabular-nums text-muted-foreground">{row.original.link_count}</span>
    ),
  },
]

interface EquipmentIndexProps {
  onOpenItem: (id: string) => void
  onNewItem: () => void
  canWrite?: boolean
}

export function EquipmentIndex({ onOpenItem, onNewItem, canWrite = true }: EquipmentIndexProps) {
  const [system, setSystem] = useState<EquipmentSystem | ''>('')
  const [status, setStatus] = useState<EquipmentStatus | ''>('')
  const [zone, setZone] = useState('')
  const [query, setQuery] = useState('')
  const [pendingDeleteId, setPendingDeleteId] = useState<string | null>(null)
  const [deleting, setDeleting] = useState(false)
  const [deleteError, setDeleteError] = useState<string | null>(null)

  const filter: EquipmentFilter = { system, status, zone, q: query }
  const { items, loading, error, refresh } = useEquipment(filter)
  const { zones } = useInventoryZones()

  const filters: IndexFilter[] = [
    {
      id: 'system',
      label: 'Filter by system',
      value: system,
      options: EQUIPMENT_SYSTEMS.map((sys) => ({ value: sys, label: EQUIPMENT_SYSTEM_LABELS[sys] })),
      onChange: (value) => setSystem(value as EquipmentSystem | ''),
    },
    {
      id: 'status',
      label: 'Filter by status',
      value: status,
      options: [
        { value: 'deployed', label: 'Deployed' },
        { value: 'stored', label: 'Stored' },
      ],
      onChange: (value) => setStatus(value as EquipmentStatus | ''),
    },
    {
      id: 'zone',
      label: 'Filter by zone',
      value: zone,
      allLabel: 'All zones',
      options: zones.map((z) => ({ value: z.id, label: z.name })),
      onChange: setZone,
    },
  ]

  const pendingItem = pendingDeleteId ? (items.find((item) => item.id === pendingDeleteId) ?? null) : null

  const rowActions = (): RowAction<EquipmentItem>[] => [
    { label: 'Open', onSelect: (row) => onOpenItem(row.id) },
    ...(canWrite
      ? [
          {
            label: 'Delete',
            destructive: true,
            onSelect: (row: EquipmentItem) => {
              setDeleteError(null)
              setPendingDeleteId(row.id)
            },
          } satisfies RowAction<EquipmentItem>,
        ]
      : []),
  ]

  const handleConfirmDelete = async () => {
    if (!pendingDeleteId) return
    setDeleting(true)
    try {
      await deleteEquipment(pendingDeleteId)
      setPendingDeleteId(null)
      await refresh()
    } catch (err) {
      // AGENTS.md fallback policy: the server's own message, dialog stays
      // open so the operator sees it (and the item) rather than losing both.
      setDeleteError(err instanceof Error ? err.message : String(err))
    } finally {
      setDeleting(false)
    }
  }

  return (
    <Page
      title="Equipment"
      primaryAction={
        canWrite
          ? { label: 'New item', icon: <Plus className="h-4 w-4" aria-hidden="true" />, onClick: onNewItem }
          : undefined
      }
    >
      <div className="flex h-full min-h-0 flex-col gap-4">
        <IndexFilters
          searchValue={query}
          onSearchChange={setQuery}
          searchLabel="Search equipment"
          searchPlaceholder="Name, alias, manufacturer..."
          filters={filters}
          onClearAll={() => {
            setSystem('')
            setStatus('')
            setZone('')
          }}
        />

        <IndexTable
          columns={columns}
          rows={items}
          getRowId={(item) => item.id}
          onOpen={(item) => onOpenItem(item.id)}
          rowActions={rowActions}
          rowActionsLabel={(item) => `Actions for ${item.name}`}
          groupBy={(item) => item.system}
          groupOrder={[...EQUIPMENT_SYSTEMS]}
          groupLabel={(key) => EQUIPMENT_SYSTEM_LABELS[key as EquipmentSystem] ?? key}
          loading={loading}
          error={error}
          onRetry={() => { void refresh() }}
          empty={
            <EmptyState
              icon={<Wrench className="h-8 w-8 text-muted-foreground" aria-hidden="true" />}
              title="No equipment matches yet"
              description="Every system aboard gets its own record - name, location, and what it takes to service it."
              action={
                canWrite ? (
                  <Button type="button" onClick={onNewItem} className="mt-2">
                    <Plus className="h-4 w-4" aria-hidden="true" />
                    New item
                  </Button>
                ) : undefined
              }
            />
          }
        />
      </div>

      <ConfirmDelete
        open={pendingDeleteId !== null}
        onOpenChange={(open) => {
          if (!open) {
            setPendingDeleteId(null)
            setDeleteError(null)
          }
        }}
        title={`Delete "${pendingItem?.name ?? ''}"?`}
        description={deleteError ?? "This removes the equipment record. It can't be undone."}
        deleting={deleting}
        onConfirm={() => { void handleConfirmDelete() }}
      />
    </Page>
  )
}

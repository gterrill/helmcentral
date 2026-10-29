import { useMemo, useState } from 'react'
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
  type EquipmentItem,
  type EquipmentStatus,
  type EquipmentSystem,
} from '@/hooks/use-inventory'
import { useEquipmentDependants } from '@/hooks/use-equipment-dependants'
import type { InventoryList } from '@/lib/app-location'
import { equipmentFilterOf, equipmentLocationLabel, orderEquipment } from '@/lib/index-order'
import { patchList } from '@/lib/list-order'
import {
  DELETE_CONSEQUENCE,
  EquipmentDependantsNotice,
  dependantsBlock,
} from '@/components/inventory/equipment-dependants-notice'

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
    accessorFn: equipmentLocationLabel,
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
  /** The filter, search and sort in force. They live in the address, so the
   * page an item opens to can step through the same list. */
  list: InventoryList
  onListChange: (list: InventoryList) => void
  onOpenItem: (id: string) => void
  onNewItem: () => void
  canWrite?: boolean
}

export function EquipmentIndex({ list, onListChange, onOpenItem, onNewItem, canWrite = true }: EquipmentIndexProps) {
  const system = (list.system ?? '') as EquipmentSystem | ''
  const status = (list.status ?? '') as EquipmentStatus | ''
  const zone = list.zone ?? ''
  const query = list.q ?? ''
  const [pendingDeleteId, setPendingDeleteId] = useState<string | null>(null)
  const [deleting, setDeleting] = useState(false)
  const [deleteError, setDeleteError] = useState<string | null>(null)

  const { items: fetched, loading, error, refresh } = useEquipment(equipmentFilterOf(list))
  // The same function the item's page steps through the list with.
  const items = useMemo(() => orderEquipment(fetched, list), [fetched, list])
  const { zones } = useInventoryZones()

  const filters: IndexFilter[] = [
    {
      id: 'system',
      label: 'Filter by system',
      value: system,
      options: EQUIPMENT_SYSTEMS.map((sys) => ({ value: sys, label: EQUIPMENT_SYSTEM_LABELS[sys] })),
      onChange: (value) => onListChange(patchList(list, { system: value })),
    },
    {
      id: 'status',
      label: 'Filter by status',
      value: status,
      options: [
        { value: 'deployed', label: 'Deployed' },
        { value: 'stored', label: 'Stored' },
      ],
      onChange: (value) => onListChange(patchList(list, { status: value })),
    },
    {
      id: 'zone',
      label: 'Filter by zone',
      value: zone,
      allLabel: 'All zones',
      options: zones.map((z) => ({ value: z.id, label: z.name })),
      onChange: (value) => onListChange(patchList(list, { zone: value })),
    },
  ]

  const dependants = useEquipmentDependants(pendingDeleteId)
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
      // deleteEquipment resolves once the equipment lists have re-read, this
      // one included, so the dialog closes on fresh rows with a single GET.
      await deleteEquipment(pendingDeleteId)
      setPendingDeleteId(null)
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
          onSearchChange={(value) => onListChange(patchList(list, { q: value }))}
          searchLabel="Search equipment"
          searchPlaceholder="Name, alias, manufacturer..."
          filters={filters}
          onClearAll={() => onListChange(patchList(list, { system: '', status: '', zone: '' }))}
        />

        <IndexTable
          columns={columns}
          rows={items}
          manualSorting
          sorting={list.sort ? [list.sort] : []}
          onSortingChange={(sorting) => onListChange(patchList(list, { sort: sorting[0] }))}
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
        confirmDisabled={dependantsBlock(dependants)}
        onConfirm={() => { void handleConfirmDelete() }}
      >
        <EquipmentDependantsNotice state={dependants} consequence={DELETE_CONSEQUENCE} />
      </ConfirmDelete>
    </Page>
  )
}

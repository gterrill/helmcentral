import { useState } from 'react'
import { Plus, Wrench } from 'lucide-react'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Select, SelectItem, SelectPopup, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import {
  EQUIPMENT_SYSTEMS,
  EQUIPMENT_SYSTEM_LABELS,
  useEquipment,
  useInventoryZones,
  type EquipmentCategory,
  type EquipmentFilter,
  type EquipmentItem,
  type EquipmentStatus,
  type EquipmentSystem,
} from '@/hooks/use-inventory'

// ADR 0123: the Equipment index. Follows wall-displays-panel.tsx's shell
// (a bordered scroll region holding a ui/table, breadcrumb/actions-left
// toolbar above it) since that is the only other plain index surface in the
// app - this reuses its vocabulary rather than inventing a second one. Owns
// its own filter/search state (the toolbar) and feeds it straight into
// useEquipment(filter); the grouping below is purely a display concern over
// whatever flat `items` list that hook returns.

const ALL_VALUE = '__all__'

interface EquipmentIndexProps {
  onOpenItem: (id: string) => void
  onNewItem: () => void
  canWrite?: boolean
}

function locationLabel(item: EquipmentItem): string {
  if (item.zone_name && item.bin_code) return `${item.zone_name} / ${item.bin_code}`
  if (item.zone_name) return item.zone_name
  return '--'
}

function EmptyState({ onNewItem, canWrite }: { onNewItem: () => void; canWrite: boolean }) {
  return (
    <div className="flex h-full flex-col items-center justify-center gap-3 px-6 py-16 text-center">
      <Wrench className="h-8 w-8 text-muted-foreground" aria-hidden="true" />
      <p className="max-w-md text-sm text-muted-foreground">
        No equipment matches yet. Every system aboard gets its own record - name, location,
        and what it takes to service it.
      </p>
      {canWrite && (
        <Button type="button" onClick={onNewItem} className="mt-2">
          <Plus className="h-4 w-4" data-icon="inline-start" aria-hidden="true" />
          New item
        </Button>
      )}
    </div>
  )
}

export function EquipmentIndex({ onOpenItem, onNewItem, canWrite = true }: EquipmentIndexProps) {
  const [category, setCategory] = useState<EquipmentCategory | ''>('')
  const [system, setSystem] = useState<EquipmentSystem | ''>('')
  const [status, setStatus] = useState<EquipmentStatus | ''>('')
  const [zone, setZone] = useState('')
  const [query, setQuery] = useState('')

  const filter: EquipmentFilter = { category, system, status, zone, q: query }
  const { items, loading, error, refresh } = useEquipment(filter)
  const { zones } = useInventoryZones()

  // Enum order (EQUIPMENT_SYSTEMS), not alphabetical - the plan's own
  // grouping rule, matching how the equipment aboard actually reads as
  // blocks (propulsion, then electrical, then water...) rather than
  // whatever order their names happen to sort in.
  const groups = EQUIPMENT_SYSTEMS
    .map((sys) => ({ system: sys, items: items.filter((item) => item.system === sys) }))
    .filter((group) => group.items.length > 0)

  return (
    <div className="flex h-full min-h-0 flex-col gap-4">
      <div className="flex flex-wrap items-end gap-2">
        <div className="flex min-w-40 flex-col gap-1">
          <label htmlFor="equipment-search" className="text-[10px] font-medium uppercase tracking-[0.16em] text-muted-foreground">
            Search
          </label>
          <Input
            id="equipment-search"
            aria-label="Search equipment"
            placeholder="Name, alias, manufacturer..."
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            className="h-9"
          />
        </div>

        <Select value={category || ALL_VALUE} onValueChange={(value) => setCategory(value === ALL_VALUE ? '' : (value as EquipmentCategory))}>
          <SelectTrigger aria-label="Filter by category" className="h-9 w-auto min-w-32">
            <SelectValue>{(value: string) => (value === ALL_VALUE ? 'All categories' : value === 'mechanical' ? 'Mechanical' : 'General')}</SelectValue>
          </SelectTrigger>
          <SelectPopup>
            <SelectItem value={ALL_VALUE}>All categories</SelectItem>
            <SelectItem value="mechanical">Mechanical</SelectItem>
            <SelectItem value="general">General</SelectItem>
          </SelectPopup>
        </Select>

        <Select value={system || ALL_VALUE} onValueChange={(value) => setSystem(value === ALL_VALUE ? '' : (value as EquipmentSystem))}>
          <SelectTrigger aria-label="Filter by system" className="h-9 w-auto min-w-32">
            <SelectValue>{(value: string) => (value === ALL_VALUE ? 'All systems' : EQUIPMENT_SYSTEM_LABELS[value as EquipmentSystem])}</SelectValue>
          </SelectTrigger>
          <SelectPopup>
            <SelectItem value={ALL_VALUE}>All systems</SelectItem>
            {EQUIPMENT_SYSTEMS.map((sys) => (
              <SelectItem key={sys} value={sys}>{EQUIPMENT_SYSTEM_LABELS[sys]}</SelectItem>
            ))}
          </SelectPopup>
        </Select>

        <Select value={status || ALL_VALUE} onValueChange={(value) => setStatus(value === ALL_VALUE ? '' : (value as EquipmentStatus))}>
          <SelectTrigger aria-label="Filter by status" className="h-9 w-auto min-w-32">
            <SelectValue>{(value: string) => (value === ALL_VALUE ? 'All statuses' : value === 'deployed' ? 'Deployed' : 'Stored')}</SelectValue>
          </SelectTrigger>
          <SelectPopup>
            <SelectItem value={ALL_VALUE}>All statuses</SelectItem>
            <SelectItem value="deployed">Deployed</SelectItem>
            <SelectItem value="stored">Stored</SelectItem>
          </SelectPopup>
        </Select>

        <Select value={zone || ALL_VALUE} onValueChange={(value) => setZone(value === ALL_VALUE ? '' : (value ?? ''))}>
          <SelectTrigger aria-label="Filter by zone" className="h-9 w-auto min-w-32">
            <SelectValue>{(value: string) => (value === ALL_VALUE ? 'All zones' : zones.find((z) => z.id === value)?.name ?? value)}</SelectValue>
          </SelectTrigger>
          <SelectPopup>
            <SelectItem value={ALL_VALUE}>All zones</SelectItem>
            {zones.map((z) => (
              <SelectItem key={z.id} value={z.id}>{z.name}</SelectItem>
            ))}
          </SelectPopup>
        </Select>

        <div className="ml-auto">
          {canWrite && (
            <Button type="button" size="sm" onClick={onNewItem}>
              <Plus className="h-4 w-4" data-icon="inline-start" aria-hidden="true" />
              New item
            </Button>
          )}
        </div>
      </div>

      <div className="min-h-0 flex-1 overflow-y-auto rounded-md border border-border bg-card">
        {error ? (
          <div className="flex h-full flex-col items-center justify-center gap-3 p-8 text-center">
            <p role="alert" className="text-sm text-destructive">{error}</p>
            <Button type="button" variant="outline" size="sm" onClick={() => { void refresh() }}>Retry</Button>
          </div>
        ) : !loading && items.length === 0 ? (
          <EmptyState onNewItem={onNewItem} canWrite={canWrite} />
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Name</TableHead>
                <TableHead>Category</TableHead>
                <TableHead>Manufacturer / Model</TableHead>
                <TableHead>Location</TableHead>
                <TableHead>Status</TableHead>
                <TableHead className="text-right">Documents</TableHead>
              </TableRow>
            </TableHeader>
            {loading ? (
              <TableBody>
                <TableRow>
                  <TableCell colSpan={6} className="py-6 text-center text-sm text-muted-foreground">Loading...</TableCell>
                </TableRow>
              </TableBody>
            ) : (
              groups.map((group) => (
                <TableBody key={group.system}>
                  <TableRow className="hover:bg-transparent">
                    <TableCell colSpan={6} className="bg-muted/40 py-1.5 text-[10px] font-semibold uppercase tracking-[0.16em] text-muted-foreground">
                      {EQUIPMENT_SYSTEM_LABELS[group.system]}
                    </TableCell>
                  </TableRow>
                  {group.items.map((item) => (
                    <TableRow key={item.id}>
                      <TableCell>
                        <button
                          type="button"
                          onClick={() => onOpenItem(item.id)}
                          className="text-left font-medium hover:underline focus-visible:outline-hidden focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 rounded-sm"
                        >
                          {item.name}
                        </button>
                      </TableCell>
                      <TableCell className="text-muted-foreground">{item.category === 'mechanical' ? 'Mechanical' : 'General'}</TableCell>
                      <TableCell className="text-muted-foreground">
                        {[item.manufacturer, item.model].filter(Boolean).join(' ') || '--'}
                      </TableCell>
                      <TableCell className="text-muted-foreground">{locationLabel(item)}</TableCell>
                      <TableCell>
                        <Badge variant={item.status === 'deployed' ? 'secondary' : 'outline'}>
                          {item.status === 'deployed' ? 'Deployed' : 'Stored'}
                        </Badge>
                      </TableCell>
                      <TableCell className="text-right tabular-nums text-muted-foreground">{item.link_count}</TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              ))
            )}
          </Table>
        )}
      </div>
    </div>
  )
}

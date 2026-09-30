import { useMemo, useState, type ReactNode } from 'react'
import {
  flexRender,
  getCoreRowModel,
  getSortedRowModel,
  useReactTable,
  type CellContext,
  type ColumnDef,
  type Row as TanstackRow,
  type SortingState,
} from '@tanstack/react-table'
import { ArrowUpDown, ChevronDown, ChevronUp, MoreVertical } from 'lucide-react'

import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { Skeleton } from '@/components/ui/skeleton'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { useIsMobile } from '@/hooks/use-mobile'
import { cn } from '@/lib/utils'

// ADR 0142: the CRUD pattern library's index table - equipment-index.tsx's
// hand-rolled Table, generalised over TanStack's column-def shape (the same
// engine shadcn's own Data Table recipe uses) so sorting, row clicks and a
// per-row action menu are written once instead of once per index page.
// Below the mobile breakpoint (useIsMobile - the same matchMedia hook the
// rest of this app already uses for its own mobile/desktop split) rows
// render as a stacked card list instead of a table, reusing the exact same
// column cell renderers so a value never has to be written twice.

const ROW_ACTIONS_COLUMN_ID = '__row-actions__'

export interface RowAction<T> {
  label: string
  icon?: ReactNode
  onSelect: (row: T) => void
  destructive?: boolean
  disabled?: (row: T) => boolean
}

export interface RowActionsProps<T> {
  row: T
  actions: RowAction<T>[]
  label: string
}

/** A per-row overflow menu. Stops the click AND the keydown reaching a
 * parent row's own onClick/onKeyDown — the menu (trigger AND popup, even
 * though the popup itself renders through a portal elsewhere in the DOM) is
 * a React-tree descendant of the wrapping div below, and React's synthetic
 * events bubble along the React tree, not the DOM tree, so a menu item
 * click OR an Enter keydown (on the trigger itself, or on a menu item once
 * it's open) would otherwise still reach a clickable row wrapping it —
 * opening/navigating the row instead of, or on top of, whatever the menu
 * itself just did. */
export function RowActions<T>({ row, actions, label }: RowActionsProps<T>) {
  if (actions.length === 0) return null
  return (
    <div
      className="inline-flex"
      onClick={(e) => e.stopPropagation()}
      onKeyDown={(e) => e.stopPropagation()}
    >
      <DropdownMenu>
        <DropdownMenuTrigger
          render={
            <Button type="button" variant="ghost" size="icon" aria-label={label}>
              <MoreVertical className="h-4 w-4" aria-hidden="true" />
            </Button>
          }
        />
        <DropdownMenuContent align="end">
          {actions.map((action) => (
            <DropdownMenuItem
              key={action.label}
              variant={action.destructive ? 'destructive' : 'default'}
              disabled={action.disabled?.(row)}
              onClick={() => action.onSelect(row)}
            >
              {action.icon}
              {action.label}
            </DropdownMenuItem>
          ))}
        </DropdownMenuContent>
      </DropdownMenu>
    </div>
  )
}

interface RowActionsMeta<T> {
  rowActions?: (row: T) => RowAction<T>[]
  rowActionsLabel?: (row: T) => string
  getRowId: (row: T) => string
}

function RowActionsCell<T>({ row, table }: CellContext<T, unknown>) {
  const meta = table.options.meta as RowActionsMeta<T>
  return (
    <RowActions
      row={row.original}
      actions={meta.rowActions!(row.original)}
      label={meta.rowActionsLabel ? meta.rowActionsLabel(row.original) : `Actions for ${meta.getRowId(row.original)}`}
    />
  )
}

function SortIcon({ direction }: { direction: false | 'asc' | 'desc' }) {
  if (direction === 'asc') return <ChevronUp className="h-3.5 w-3.5" aria-hidden="true" />
  if (direction === 'desc') return <ChevronDown className="h-3.5 w-3.5" aria-hidden="true" />
  return <ArrowUpDown className="h-3.5 w-3.5 opacity-50" aria-hidden="true" />
}

function columnLabel<T>(column: { id: string; columnDef: ColumnDef<T, unknown> }): string {
  const header = column.columnDef.header
  return typeof header === 'string' ? header : column.id
}

export interface IndexTableProps<T> {
  columns: ColumnDef<T, unknown>[]
  rows: T[]
  getRowId: (row: T) => string
  onOpen?: (row: T) => void
  rowActions?: (row: T) => RowAction<T>[]
  /** aria-label for a row's action trigger. Defaults to `Actions for
   * ${getRowId(row)}`, which is stable but not necessarily readable - pass
   * this to say "Actions for Generator" instead of "Actions for eq-42". */
  rowActionsLabel?: (row: T) => string
  /** Buckets the (already sorted) rows under a header per distinct key -
   * equipment-index.tsx's own system grouping. Sorting is applied before
   * grouping and stays intact within each bucket; the buckets themselves
   * keep groupOrder's order, not the sort's. */
  groupBy?: (row: T) => string
  groupOrder?: string[]
  groupLabel?: (key: string) => string
  loading?: boolean
  error?: string | null
  onRetry?: () => void
  /** Rendered in place of the table/card list when there are no rows and
   * nothing is loading or errored - an EmptyState, typically. */
  empty?: ReactNode
  className?: string
}

export function IndexTable<T>({
  columns,
  rows,
  getRowId,
  onOpen,
  rowActions,
  rowActionsLabel,
  groupBy,
  groupOrder,
  groupLabel,
  loading = false,
  error = null,
  onRetry,
  empty,
  className,
}: IndexTableProps<T>) {
  const [sorting, setSorting] = useState<SortingState>([])
  const isMobile = useIsMobile()

  // The actions column's cell must have a STABLE identity: TanStack's
  // flexRender renders a function cell as a component, so a cell recreated
  // every render (as one closing over the inline rowActions prop would be)
  // remounts RowActions on any parent re-render and closes an open menu. The
  // cell is module-level and reads the current props from table meta instead.
  const mergedColumns = useMemo<ColumnDef<T, unknown>[]>(() => {
    if (!rowActions) return columns
    const actionsColumn: ColumnDef<T, unknown> = {
      id: ROW_ACTIONS_COLUMN_ID,
      header: '',
      enableSorting: false,
      cell: RowActionsCell as ColumnDef<T, unknown>['cell'],
    }
    return [...columns, actionsColumn]
  }, [columns, !!rowActions]) // eslint-disable-line react-hooks/exhaustive-deps

  const table = useReactTable({
    data: rows,
    columns: mergedColumns,
    state: { sorting },
    onSortingChange: setSorting,
    getRowId: (row) => getRowId(row),
    meta: { rowActions, rowActionsLabel, getRowId } satisfies RowActionsMeta<T>,
    getCoreRowModel: getCoreRowModel(),
    getSortedRowModel: getSortedRowModel(),
  })

  const wrapperClassName = cn('min-h-0 flex-1 overflow-y-auto rounded-md border border-border bg-card', className)

  if (error) {
    return (
      <div className={wrapperClassName}>
        <div className="flex h-full flex-col items-center justify-center gap-3 p-8 text-center">
          <p role="alert" className="text-sm text-destructive">{error}</p>
          {onRetry && (
            <Button type="button" variant="outline" size="sm" onClick={onRetry}>
              Retry
            </Button>
          )}
        </div>
      </div>
    )
  }

  if (loading) {
    return (
      <div className={wrapperClassName}>
        <div className="flex flex-col gap-2 p-4">
          {[0, 1, 2].map((i) => (
            <Skeleton key={i} data-testid="index-table-skeleton-row" className="h-9 w-full" />
          ))}
        </div>
      </div>
    )
  }

  const sortedRows = table.getRowModel().rows

  if (sortedRows.length === 0) {
    return <div className={wrapperClassName}>{empty ?? null}</div>
  }

  let groups: { key: string; label: string; rows: TanstackRow<T>[] }[] | null = null
  if (groupBy) {
    const buckets = new Map<string, TanstackRow<T>[]>()
    for (const row of sortedRows) {
      const key = groupBy(row.original)
      const bucket = buckets.get(key)
      if (bucket) bucket.push(row)
      else buckets.set(key, [row])
    }
    const order = groupOrder ?? []
    const orderedKeys = [
      ...order.filter((key) => buckets.has(key)),
      ...[...buckets.keys()].filter((key) => !order.includes(key)),
    ]
    groups = orderedKeys.map((key) => ({ key, label: groupLabel ? groupLabel(key) : key, rows: buckets.get(key)! }))
  }

  function renderCard(row: TanstackRow<T>) {
    const cells = row.getVisibleCells().filter((cell) => cell.column.id !== ROW_ACTIONS_COLUMN_ID)
    const [primary, ...rest] = cells
    return (
      <div
        key={row.id}
        role={onOpen ? 'button' : undefined}
        tabIndex={onOpen ? 0 : undefined}
        onClick={onOpen ? () => onOpen(row.original) : undefined}
        onKeyDown={onOpen ? (e) => {
          // Only Enter on the row/card itself opens it — RowActions already
          // stops its own keydown from bubbling here, but this guard is the
          // one that also holds for any other focusable thing a future
          // column's cell might put inside a row without going through
          // RowActions at all (e.target !== e.currentTarget whenever the
          // event started on a descendant, not the row/card element itself).
          if (e.key === 'Enter' && e.target === e.currentTarget) onOpen(row.original)
        } : undefined}
        className={cn(
          'flex flex-col gap-1.5 rounded-md border border-border bg-card p-3',
          onOpen && 'cursor-pointer',
        )}
      >
        <div className="flex items-start justify-between gap-2">
          <div className="min-w-0 flex-1 truncate text-sm font-medium text-foreground">
            {primary ? flexRender(primary.column.columnDef.cell, primary.getContext()) : null}
          </div>
          {rowActions && (
            <RowActions
              row={row.original}
              actions={rowActions(row.original)}
              label={rowActionsLabel ? rowActionsLabel(row.original) : `Actions for ${getRowId(row.original)}`}
            />
          )}
        </div>
        {rest.map((cell) => (
          <div key={cell.id} className="flex items-baseline justify-between gap-2 text-xs">
            <span className="text-muted-foreground">{columnLabel(cell.column)}</span>
            <span className="min-w-0 truncate text-right text-foreground">
              {flexRender(cell.column.columnDef.cell, cell.getContext())}
            </span>
          </div>
        ))}
      </div>
    )
  }

  if (isMobile) {
    return (
      <div className={cn('flex flex-col gap-3', className)}>
        {groups
          ? groups.map((group) => (
              <div key={group.key} className="flex flex-col gap-2">
                <p className="px-1 text-[10px] font-semibold uppercase tracking-[0.16em] text-muted-foreground">
                  {group.label}
                </p>
                {group.rows.map((row) => renderCard(row))}
              </div>
            ))
          : sortedRows.map((row) => renderCard(row))}
      </div>
    )
  }

  function renderRow(row: TanstackRow<T>) {
    return (
      <TableRow
        key={row.id}
        tabIndex={onOpen ? 0 : undefined}
        onClick={onOpen ? () => onOpen(row.original) : undefined}
        onKeyDown={onOpen ? (e) => {
          // Only Enter on the row/card itself opens it — RowActions already
          // stops its own keydown from bubbling here, but this guard is the
          // one that also holds for any other focusable thing a future
          // column's cell might put inside a row without going through
          // RowActions at all (e.target !== e.currentTarget whenever the
          // event started on a descendant, not the row/card element itself).
          if (e.key === 'Enter' && e.target === e.currentTarget) onOpen(row.original)
        } : undefined}
        className={cn(onOpen && 'cursor-pointer')}
      >
        {row.getVisibleCells().map((cell) => (
          <TableCell key={cell.id}>{flexRender(cell.column.columnDef.cell, cell.getContext())}</TableCell>
        ))}
      </TableRow>
    )
  }

  return (
    <div className={wrapperClassName}>
      <Table>
        <TableHeader>
          {table.getHeaderGroups().map((headerGroup) => (
            <TableRow key={headerGroup.id} className="hover:bg-transparent">
              {headerGroup.headers.map((header) => {
                const canSort = header.column.getCanSort()
                const sortDir = header.column.getIsSorted()
                return (
                  <TableHead
                    key={header.id}
                    aria-sort={canSort ? (sortDir === 'asc' ? 'ascending' : sortDir === 'desc' ? 'descending' : 'none') : undefined}
                  >
                    {header.isPlaceholder ? null : canSort ? (
                      <button
                        type="button"
                        className="-mx-2 flex items-center gap-1 rounded-sm px-2 py-1 hover:text-foreground focus-visible:outline-hidden focus-visible:ring-2 focus-visible:ring-ring"
                        onClick={header.column.getToggleSortingHandler()}
                      >
                        {flexRender(header.column.columnDef.header, header.getContext())}
                        <SortIcon direction={sortDir} />
                      </button>
                    ) : (
                      flexRender(header.column.columnDef.header, header.getContext())
                    )}
                  </TableHead>
                )
              })}
            </TableRow>
          ))}
        </TableHeader>
        <TableBody>
          {groups
            ? groups.flatMap((group) => [
                <TableRow key={`group-${group.key}`} className="hover:bg-transparent">
                  <TableCell
                    colSpan={mergedColumns.length}
                    className="bg-muted/40 py-1.5 text-[10px] font-semibold uppercase tracking-[0.16em] text-muted-foreground"
                  >
                    {group.label}
                  </TableCell>
                </TableRow>,
                ...group.rows.map((row) => renderRow(row)),
              ])
            : sortedRows.map((row) => renderRow(row))}
        </TableBody>
      </Table>
    </div>
  )
}

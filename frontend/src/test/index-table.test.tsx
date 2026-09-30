import { describe, expect, it, vi } from 'vitest'
import { render, screen, fireEvent, within } from '@testing-library/react'
import type { ColumnDef } from '@tanstack/react-table'
import { IndexTable, type RowAction } from '@/components/patterns/index-table'
import { setViewportWidth } from './viewport'

interface Row {
  id: string
  name: string
  system: string
  hours: number
}

const columns: ColumnDef<Row, unknown>[] = [
  { accessorKey: 'name', header: 'Name' },
  { accessorKey: 'hours', header: 'Hours' },
]

const rows: Row[] = [
  { id: 'a', name: 'Bilge pump', system: 'bilge', hours: 120 },
  { id: 'b', name: 'Generator', system: 'electrical', hours: 40 },
  { id: 'c', name: 'Windlass', system: 'anchoring', hours: 5 },
]

describe('IndexTable', () => {
  it('renders a row per item with each column\'s value', () => {
    render(<IndexTable columns={columns} rows={rows} getRowId={(r) => r.id} />)
    expect(screen.getByText('Bilge pump')).toBeInTheDocument()
    expect(screen.getByText('120')).toBeInTheDocument()
    expect(screen.getByText('Generator')).toBeInTheDocument()
  })

  it('calls onOpen with the row when a row is clicked', () => {
    const onOpen = vi.fn()
    render(<IndexTable columns={columns} rows={rows} getRowId={(r) => r.id} onOpen={onOpen} />)

    fireEvent.click(screen.getByText('Generator'))
    expect(onOpen).toHaveBeenCalledWith(rows[1])
  })

  it('opens a row on Enter when it is focused', () => {
    const onOpen = vi.fn()
    render(<IndexTable columns={columns} rows={rows} getRowId={(r) => r.id} onOpen={onOpen} />)

    const cell = screen.getByText('Generator')
    const row = cell.closest('tr')
    expect(row).not.toBeNull()
    fireEvent.keyDown(row as HTMLElement, { key: 'Enter' })
    expect(onOpen).toHaveBeenCalledWith(rows[1])
  })

  it('sorts ascending, then descending, then back to the original order on repeated header clicks, updating aria-sort', () => {
    render(<IndexTable columns={columns} rows={rows} getRowId={(r) => r.id} />)

    const header = screen.getByRole('columnheader', { name: 'Name' })
    expect(header).toHaveAttribute('aria-sort', 'none')

    const sortButton = within(header).getByRole('button')
    fireEvent.click(sortButton)
    expect(header).toHaveAttribute('aria-sort', 'ascending')
    let names = screen.getAllByRole('row').slice(1).map((r) => within(r).getAllByRole('cell')[0].textContent)
    expect(names).toEqual(['Bilge pump', 'Generator', 'Windlass'])

    fireEvent.click(sortButton)
    expect(header).toHaveAttribute('aria-sort', 'descending')
    names = screen.getAllByRole('row').slice(1).map((r) => within(r).getAllByRole('cell')[0].textContent)
    expect(names).toEqual(['Windlass', 'Generator', 'Bilge pump'])
  })

  it('a row action click does not also open the row, and stays scoped to its own row', async () => {
    const onOpen = vi.fn()
    const onDelete = vi.fn()
    const rowActions = (row: Row): RowAction<Row>[] => [
      { label: 'Delete', onSelect: () => onDelete(row.id), destructive: true },
    ]
    render(
      <IndexTable columns={columns} rows={rows} getRowId={(r) => r.id} onOpen={onOpen} rowActions={rowActions} />,
    )

    fireEvent.click(screen.getByRole('button', { name: 'Actions for b' }))
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Delete' }))

    expect(onDelete).toHaveBeenCalledWith('b')
    expect(onOpen).not.toHaveBeenCalled()
  })

  // Root cause of a flaky equipment-index test, and a real bug: the actions
  // column's cell was a fresh function on every render (it closed over the
  // rowActions prop, which callers pass inline), and TanStack's flexRender
  // renders a function cell as a COMPONENT - so any parent re-render remounted
  // RowActions and tore down an open menu (a zones/data refresh arriving while
  // the operator had the menu open closed it under their finger).
  it('an open row menu survives the parent re-rendering with new rowActions/label functions', async () => {
    const make = () => ({
      rowActions: (row: Row): RowAction<Row>[] => [{ label: 'Delete', onSelect: () => {}, destructive: row.id === 'b' }],
      rowActionsLabel: (row: Row) => `Actions for ${row.name}`,
    })
    const first = make()
    const { rerender } = render(
      <IndexTable columns={columns} rows={rows} getRowId={(r) => r.id} rowActions={first.rowActions} rowActionsLabel={first.rowActionsLabel} />,
    )

    fireEvent.click(screen.getByRole('button', { name: 'Actions for Generator' }))
    expect(await screen.findByRole('menuitem', { name: 'Delete' })).toBeInTheDocument()

    const second = make()
    rerender(
      <IndexTable columns={columns} rows={rows} getRowId={(r) => r.id} rowActions={second.rowActions} rowActionsLabel={second.rowActionsLabel} />,
    )
    // Same trigger element, menu still open.
    expect(screen.getByRole('menuitem', { name: 'Delete' })).toBeInTheDocument()
  })

  it('pressing Enter on the row action trigger does not also open the row', () => {
    const onOpen = vi.fn()
    const rowActions = (): RowAction<Row>[] => [
      { label: 'Delete', onSelect: vi.fn(), destructive: true },
    ]
    render(
      <IndexTable columns={columns} rows={rows} getRowId={(r) => r.id} onOpen={onOpen} rowActions={rowActions} />,
    )

    fireEvent.keyDown(screen.getByRole('button', { name: 'Actions for b' }), { key: 'Enter' })
    expect(onOpen).not.toHaveBeenCalled()
  })

  it('pressing Enter on a row action menu item does not also open the row', async () => {
    const onOpen = vi.fn()
    const onDelete = vi.fn()
    const rowActions = (row: Row): RowAction<Row>[] => [
      { label: 'Delete', onSelect: () => onDelete(row.id), destructive: true },
    ]
    render(
      <IndexTable columns={columns} rows={rows} getRowId={(r) => r.id} onOpen={onOpen} rowActions={rowActions} />,
    )

    fireEvent.click(screen.getByRole('button', { name: 'Actions for b' }))
    fireEvent.keyDown(await screen.findByRole('menuitem', { name: 'Delete' }), { key: 'Enter' })
    expect(onOpen).not.toHaveBeenCalled()
  })

  it('pressing Enter on a card\'s row action trigger does not also open the row, below the mobile breakpoint', () => {
    setViewportWidth(600)
    const onOpen = vi.fn()
    const rowActions = (): RowAction<Row>[] => [
      { label: 'Delete', onSelect: vi.fn(), destructive: true },
    ]
    render(
      <IndexTable columns={columns} rows={rows} getRowId={(r) => r.id} onOpen={onOpen} rowActions={rowActions} />,
    )

    fireEvent.keyDown(screen.getByRole('button', { name: 'Actions for b' }), { key: 'Enter' })
    expect(onOpen).not.toHaveBeenCalled()
    setViewportWidth(1280)
  })

  it('pressing Enter on a card\'s row action menu item does not also open the row, below the mobile breakpoint', async () => {
    setViewportWidth(600)
    const onOpen = vi.fn()
    const onDelete = vi.fn()
    const rowActions = (row: Row): RowAction<Row>[] => [
      { label: 'Delete', onSelect: () => onDelete(row.id), destructive: true },
    ]
    render(
      <IndexTable columns={columns} rows={rows} getRowId={(r) => r.id} onOpen={onOpen} rowActions={rowActions} />,
    )

    fireEvent.click(screen.getByRole('button', { name: 'Actions for b' }))
    fireEvent.keyDown(await screen.findByRole('menuitem', { name: 'Delete' }), { key: 'Enter' })
    expect(onOpen).not.toHaveBeenCalled()
    setViewportWidth(1280)
  })

  it('shows a loading skeleton instead of rows while loading', () => {
    render(<IndexTable columns={columns} rows={[]} getRowId={(r: Row) => r.id} loading />)
    expect(screen.queryByText('Bilge pump')).not.toBeInTheDocument()
    expect(screen.getAllByTestId('index-table-skeleton-row').length).toBeGreaterThan(0)
  })

  it('shows the error and a Retry action instead of rows when errored', () => {
    const onRetry = vi.fn()
    render(<IndexTable columns={columns} rows={[]} getRowId={(r: Row) => r.id} error="Could not load equipment" onRetry={onRetry} />)

    expect(screen.getByRole('alert')).toHaveTextContent('Could not load equipment')
    fireEvent.click(screen.getByRole('button', { name: 'Retry' }))
    expect(onRetry).toHaveBeenCalled()
  })

  it('shows the empty slot when there are no rows and nothing is loading or errored', () => {
    render(
      <IndexTable
        columns={columns}
        rows={[]}
        getRowId={(r: Row) => r.id}
        empty={<p>No equipment matches yet.</p>}
      />,
    )
    expect(screen.getByText('No equipment matches yet.')).toBeInTheDocument()
  })

  it('groups rows under a header per group, in the given group order', () => {
    render(
      <IndexTable
        columns={columns}
        rows={rows}
        getRowId={(r) => r.id}
        groupBy={(r) => r.system}
        groupOrder={['electrical', 'bilge', 'anchoring']}
        groupLabel={(key) => key.toUpperCase()}
      />,
    )
    const headings = screen.getAllByText(/^(ELECTRICAL|BILGE|ANCHORING)$/).map((el) => el.textContent)
    expect(headings).toEqual(['ELECTRICAL', 'BILGE', 'ANCHORING'])
  })

  it('renders a stacked card list instead of a table below the mobile breakpoint', () => {
    setViewportWidth(600)
    render(<IndexTable columns={columns} rows={rows} getRowId={(r) => r.id} />)

    expect(screen.queryByRole('table')).not.toBeInTheDocument()
    expect(screen.getByText('Bilge pump')).toBeInTheDocument()
    setViewportWidth(1280)
  })

  it('opens a row from a card on click below the mobile breakpoint', () => {
    setViewportWidth(600)
    const onOpen = vi.fn()
    render(<IndexTable columns={columns} rows={rows} getRowId={(r) => r.id} onOpen={onOpen} />)

    fireEvent.click(screen.getByText('Generator'))
    expect(onOpen).toHaveBeenCalledWith(rows[1])
    setViewportWidth(1280)
  })

  // A card lists "label: value" per column; a row whose accessor yields
  // nothing (Documents' folders have no size or status) omits that line
  // instead of printing an empty label.
  it('cards skip a column whose value is empty for that row', () => {
    setViewportWidth(390)
    const cols: ColumnDef<Row, unknown>[] = [
      { accessorKey: 'name', header: 'Name' },
      { id: 'hours', header: 'Hours', accessorFn: (r) => (r.hours > 0 ? String(r.hours) : '') },
    ]
    render(<IndexTable columns={cols} rows={[{ id: 'a', name: 'Folder', system: '', hours: 0 }, { id: 'b', name: 'Pump', system: '', hours: 9 }]} getRowId={(r) => r.id} />)
    const folder = screen.getByText('Folder').closest('[data-slot="index-card"]') as HTMLElement
    const pump = screen.getByText('Pump').closest('[data-slot="index-card"]') as HTMLElement
    expect(within(folder).queryByText('Hours')).not.toBeInTheDocument()
    expect(within(pump).getByText('Hours')).toBeInTheDocument()
    setViewportWidth(1280)
  })
})

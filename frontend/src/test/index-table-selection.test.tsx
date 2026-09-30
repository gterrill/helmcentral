import { useState } from 'react'
import { describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, within } from '@testing-library/react'
import type { ColumnDef } from '@tanstack/react-table'
import { IndexTable } from '@/components/patterns/index-table'
import { setViewportWidth } from './viewport'

// ADR 0142 (Documents migration): Polaris' bulk actions on IndexTable - a
// checkbox column, select-all for the visible rows, and a bar that replaces
// the idle toolbar with "N selected" plus the caller's actions.

interface Row { id: string; name: string; locked?: boolean }
const columns: ColumnDef<Row, unknown>[] = [{ accessorKey: 'name', header: 'Name' }]
const rows: Row[] = [
  { id: 'a', name: 'Alpha' },
  { id: 'b', name: 'Bravo' },
  { id: 'c', name: 'Charlie', locked: true },
]

function Harness({ initial = [], onOpen, bulk = true }: { initial?: string[]; onOpen?: (r: Row) => void; bulk?: boolean }) {
  const [selected, setSelected] = useState<Set<string>>(new Set(initial))
  return (
    <IndexTable
      columns={columns}
      rows={rows}
      getRowId={(r) => r.id}
      onOpen={onOpen}
      selectable
      isRowSelectable={(r) => !r.locked}
      selectedIds={selected}
      onSelectionChange={setSelected}
      rowSelectLabel={(r) => `Select ${r.name}`}
      bulkActions={bulk ? <button type="button">Delete selected</button> : undefined}
    />
  )
}

describe('IndexTable selection', () => {
  it('adds no checkbox column unless selectable', () => {
    render(<IndexTable columns={columns} rows={rows} getRowId={(r) => r.id} />)
    expect(screen.queryByRole('checkbox')).not.toBeInTheDocument()
  })

  it('selects and deselects a row with its checkbox, without opening it', () => {
    const onOpen = vi.fn()
    render(<Harness onOpen={onOpen} />)
    fireEvent.click(screen.getByRole('checkbox', { name: 'Select Alpha' }))
    expect(screen.getByRole('checkbox', { name: 'Select Alpha' })).toBeChecked()
    expect(onOpen).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole('checkbox', { name: 'Select Alpha' }))
    expect(screen.getByRole('checkbox', { name: 'Select Alpha' })).not.toBeChecked()
  })

  it('a row that is not selectable gets no checkbox', () => {
    render(<Harness />)
    expect(screen.queryByRole('checkbox', { name: 'Select Charlie' })).not.toBeInTheDocument()
  })

  it('shows "N selected" and the bulk actions only while something is selected', () => {
    render(<Harness />)
    expect(screen.queryByText(/selected$/)).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Delete selected' })).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('checkbox', { name: 'Select Alpha' }))
    expect(screen.getByText('1 selected')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Delete selected' })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('checkbox', { name: 'Select Bravo' }))
    expect(screen.getByText('2 selected')).toBeInTheDocument()
  })

  it('Clear empties the selection', () => {
    render(<Harness initial={['a', 'b']} />)
    fireEvent.click(screen.getByRole('button', { name: 'Clear selection' }))
    expect(screen.queryByText(/selected$/)).not.toBeInTheDocument()
  })

  it('header checkbox selects every selectable visible row, is indeterminate for some, and clears when all are selected', () => {
    render(<Harness />)
    const all = screen.getByRole('checkbox', { name: 'Select all' })
    expect(all).not.toBeChecked()

    fireEvent.click(screen.getByRole('checkbox', { name: 'Select Alpha' }))
    expect(all).toHaveAttribute('aria-checked', 'mixed')

    fireEvent.click(all)
    expect(screen.getByRole('checkbox', { name: 'Select Alpha' })).toBeChecked()
    expect(screen.getByRole('checkbox', { name: 'Select Bravo' })).toBeChecked()
    expect(screen.getByText('2 selected')).toBeInTheDocument()
    expect(all).toBeChecked()

    fireEvent.click(all)
    expect(screen.queryByText(/selected$/)).not.toBeInTheDocument()
  })

  it('without bulkActions the count and Clear still show', () => {
    render(<Harness bulk={false} initial={['a']} />)
    expect(screen.getByText('1 selected')).toBeInTheDocument()
  })

  it('is controlled: it reports changes and shows whatever selectedIds says', () => {
    const onSelectionChange = vi.fn()
    const { rerender } = render(
      <IndexTable columns={columns} rows={rows} getRowId={(r) => r.id} selectable selectedIds={new Set(['b'])} onSelectionChange={onSelectionChange} rowSelectLabel={(r) => `Select ${r.name}`} />,
    )
    expect(screen.getByRole('checkbox', { name: 'Select Bravo' })).toBeChecked()
    fireEvent.click(screen.getByRole('checkbox', { name: 'Select Alpha' }))
    expect(onSelectionChange).toHaveBeenCalledWith(new Set(['b', 'a']))
    rerender(<IndexTable columns={columns} rows={rows} getRowId={(r) => r.id} selectable selectedIds={new Set()} onSelectionChange={onSelectionChange} rowSelectLabel={(r) => `Select ${r.name}`} />)
    expect(screen.getByRole('checkbox', { name: 'Select Bravo' })).not.toBeChecked()
  })

  it('cards on a phone carry a checkbox each and the same bar', () => {
    setViewportWidth(390)
    render(<Harness />)
    const card = screen.getByText('Alpha').closest('[data-slot="index-card"]') as HTMLElement
    expect(card).not.toBeNull()
    fireEvent.click(within(card).getByRole('checkbox', { name: 'Select Alpha' }))
    expect(screen.getByText('1 selected')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Delete selected' })).toBeInTheDocument()
    setViewportWidth(1280)
  })

  // A selection must never hold rows that are not in `rows`: a caller that
  // filters its list would otherwise act on ids the operator can no longer
  // see ("1 selected" deleting three).
  it('prunes the selection to the rows present whenever rows change', () => {
    const onSelectionChange = vi.fn()
    const props = { columns, getRowId: (r: Row) => r.id, selectable: true, onSelectionChange }
    const { rerender } = render(<IndexTable {...props} rows={rows} selectedIds={new Set(['a', 'b'])} />)
    expect(onSelectionChange).not.toHaveBeenCalled()

    rerender(<IndexTable {...props} rows={rows.filter((r) => r.id === 'a')} selectedIds={new Set(['a', 'b'])} />)
    expect(onSelectionChange).toHaveBeenCalledTimes(1)
    expect(onSelectionChange).toHaveBeenCalledWith(new Set(['a']))
  })

  it('does not report a prune when every selected id is still present', () => {
    const onSelectionChange = vi.fn()
    const props = { columns, getRowId: (r: Row) => r.id, selectable: true, onSelectionChange }
    const { rerender } = render(<IndexTable {...props} rows={rows} selectedIds={new Set(['a'])} />)
    rerender(<IndexTable {...props} rows={[...rows].reverse()} selectedIds={new Set(['a'])} />)
    expect(onSelectionChange).not.toHaveBeenCalled()
  })

  it('Clear selection empties the whole selection, not just the visible rows', () => {
    const onSelectionChange = vi.fn()
    render(<IndexTable columns={columns} rows={rows} getRowId={(r) => r.id} selectable selectedIds={new Set(['a', 'gone'])} onSelectionChange={onSelectionChange} />)
    fireEvent.click(screen.getByRole('button', { name: 'Clear selection' }))
    expect(onSelectionChange).toHaveBeenLastCalledWith(new Set())
  })
})

import { useState } from 'react'
import { describe, expect, it } from 'vitest'
import { fireEvent, render, screen } from '@testing-library/react'

import { EquipmentSparesFields } from '@/components/inventory/equipment-spares-fields'
import { stockState } from '@/lib/equipment-stock'

// The stock fields of an equipment record. "Out of stock" and "Below
// required" are read off on hand and required, never stored.

function Harness({ initial }: { initial: { part_number: string; quantity: number; required_quantity: number | null } }) {
  const [draft, setDraft] = useState(initial)
  return <EquipmentSparesFields draft={draft} onChange={(patch) => setDraft((p) => ({ ...p, ...patch }))} />
}

describe('stockState', () => {
  it('is null when no required number is set, however few are on board', () => {
    expect(stockState(0, null)).toBeNull()
  })

  it('is null when on hand meets or beats required', () => {
    expect(stockState(2, 2)).toBeNull()
    expect(stockState(5, 2)).toBeNull()
  })

  it('is out when none are left and some are required', () => {
    expect(stockState(0, 1)).toBe('out')
  })

  it('is below when some are left but fewer than required', () => {
    expect(stockState(1, 3)).toBe('below')
  })
})

describe('EquipmentSparesFields', () => {
  it('shows no flag for a spare with no target', () => {
    render(<Harness initial={{ part_number: '21669', quantity: 0, required_quantity: null }} />)
    expect(screen.queryByRole('status')).not.toBeInTheDocument()
  })

  it('flags Out of stock when on hand is 0 and one is required', () => {
    render(<Harness initial={{ part_number: '21669', quantity: 0, required_quantity: 1 }} />)
    expect(screen.getByRole('status')).toHaveTextContent('Out of stock')
  })

  it('flags Below required, and clears it once on hand catches up', () => {
    render(<Harness initial={{ part_number: '', quantity: 1, required_quantity: 3 }} />)
    expect(screen.getByRole('status')).toHaveTextContent('Below required')

    fireEvent.change(screen.getByLabelText('On hand'), { target: { value: '3' } })
    expect(screen.queryByRole('status')).not.toBeInTheDocument()
  })

  it('keeps 0 as a valid on hand number', () => {
    render(<Harness initial={{ part_number: '', quantity: 4, required_quantity: 2 }} />)
    fireEvent.change(screen.getByLabelText('On hand'), { target: { value: '0' } })
    expect(screen.getByLabelText('On hand')).toHaveValue(0)
    expect(screen.getByRole('status')).toHaveTextContent('Out of stock')
  })

  it('turns a blank Required quantity into no target', () => {
    render(<Harness initial={{ part_number: '', quantity: 0, required_quantity: 2 }} />)
    fireEvent.change(screen.getByLabelText('Required quantity'), { target: { value: '' } })
    expect(screen.getByLabelText('Required quantity')).toHaveValue(null)
    expect(screen.queryByRole('status')).not.toBeInTheDocument()
  })

  it('edits the part number', () => {
    render(<Harness initial={{ part_number: '', quantity: 1, required_quantity: null }} />)
    fireEvent.change(screen.getByLabelText('Part number'), { target: { value: '0130-4434' } })
    expect(screen.getByLabelText('Part number')).toHaveValue('0130-4434')
  })
})

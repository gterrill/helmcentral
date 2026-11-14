import { useState } from 'react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { PartsEditor } from '@/components/inventory/maintenance-log-dialogs'
import type { EquipmentItem } from '@/hooks/use-inventory'
import type { MaintenanceLogPartInput } from '@/hooks/use-maintenance'

const createEquipment = vi.fn()
let failCreate = false

vi.mock('@/hooks/use-inventory', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/hooks/use-inventory')>()
  return { ...actual, createEquipment: async (...args: unknown[]) => { if (failCreate) throw new Error('disk full'); return createEquipment(...args) } }
})

function item(overrides: Partial<EquipmentItem>): EquipmentItem {
  return { id: 'x', name: 'x', manufacturer: '', model: '', part_number: '', ...overrides } as EquipmentItem
}

const ITEMS = [
  item({ id: 'eq-impeller', name: 'Raw water impeller', part_number: 'JP-9959', manufacturer: 'Jabsco' }),
  item({ id: 'eq-filter', name: 'Fuel filter', part_number: 'R90P', manufacturer: 'Racor' }),
]

function Harness({ initial = [], items = ITEMS, itemsLoading = false, itemsError = null }: { initial?: MaintenanceLogPartInput[]; items?: EquipmentItem[]; itemsLoading?: boolean; itemsError?: string | null }) {
  const [parts, setParts] = useState(initial)
  return (
    <>
      <PartsEditor parts={parts} onChange={setParts} items={items} itemsLoading={itemsLoading} itemsError={itemsError} />
      <output data-testid="parts">{JSON.stringify(parts)}</output>
    </>
  )
}

function type(text: string) {
  const input = screen.getByRole('combobox', { name: 'Add a part' })
  fireEvent.click(input)
  fireEvent.change(input, { target: { value: text } })
}

const partsOut = () => JSON.parse(screen.getByTestId('parts').textContent ?? '[]')

beforeEach(() => { createEquipment.mockReset(); failCreate = false })

describe('PartsEditor: searchable picker', () => {
  it('filters by name, case-insensitively', async () => {
    render(<Harness />)
    type('IMPELL')
    expect(await screen.findByRole('option', { name: 'Raw water impeller' })).toBeInTheDocument()
    expect(screen.queryByRole('option', { name: 'Fuel filter' })).not.toBeInTheDocument()
  })

  it('matches every typed word across the fields', async () => {
    render(<Harness />)
    type('jabsco impeller')
    expect(await screen.findByRole('option', { name: 'Raw water impeller' })).toBeInTheDocument()
    expect(screen.queryByRole('option', { name: 'Fuel filter' })).not.toBeInTheDocument()
  })

  it('filters by part number', async () => {
    render(<Harness />)
    type('r90p')
    expect(await screen.findByRole('option', { name: 'Fuel filter' })).toBeInTheDocument()
    expect(screen.queryByRole('option', { name: 'Raw water impeller' })).not.toBeInTheDocument()
  })

  it('adds a picked match with the quantity', async () => {
    render(<Harness />)
    type('fuel')
    fireEvent.click(await screen.findByRole('option', { name: 'Fuel filter' }))
    fireEvent.change(screen.getByLabelText('Quantity'), { target: { value: '2' } })
    fireEvent.click(screen.getByRole('button', { name: 'Add part' }))
    expect(partsOut()).toEqual([{ equipment_id: 'eq-filter', quantity: 2 }])
    expect(screen.getByText('Fuel filter')).toBeInTheDocument()
  })
})

describe('PartsEditor: add a spare from the field', () => {
  it('creates a stored item from the typed text, then adds it with the quantity', async () => {
    createEquipment.mockResolvedValue(item({ id: 'eq-new', name: 'Hydraulic oil' }))
    render(<Harness />)
    type('  Hydraulic oil ')
    fireEvent.click(await screen.findByRole('option', { name: 'Add "Hydraulic oil" as a spare' }))
    await waitFor(() => expect(createEquipment).toHaveBeenCalledTimes(1))
    expect(createEquipment.mock.calls[0][0]).toMatchObject({ name: 'Hydraulic oil', status: 'stored', quantity: 0 })

    await waitFor(() => expect(screen.getByRole('button', { name: 'Add part' })).toBeEnabled())
    fireEvent.change(screen.getByLabelText('Quantity'), { target: { value: '3' } })
    fireEvent.click(screen.getByRole('button', { name: 'Add part' }))
    expect(partsOut()).toEqual([{ equipment_id: 'eq-new', quantity: 3 }])
    // The items prop has not refetched yet; the row still shows the name.
    expect(screen.getByText('Hydraulic oil')).toBeInTheDocument()
  })

  it('offers no create option when the text matches an existing name exactly', async () => {
    render(<Harness />)
    type('fuel FILTER')
    expect(await screen.findByRole('option', { name: 'Fuel filter' })).toBeInTheDocument()
    expect(screen.queryByRole('option', { name: /as a spare/ })).not.toBeInTheDocument()
  })

  it('shows the error and adds nothing when the create fails', async () => {
    failCreate = true
    render(<Harness />)
    type('Hydraulic oil')
    fireEvent.click(await screen.findByRole('option', { name: 'Add "Hydraulic oil" as a spare' }))
    expect(await screen.findByText(/disk full/)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Add part' })).toBeDisabled()
    expect(partsOut()).toEqual([])
    // The box keeps what was typed, not the create row's wording, so a
    // retry offers the same name again.
    expect(screen.getByRole('combobox', { name: 'Add a part' })).toHaveValue('Hydraulic oil')
    expect(screen.queryByRole('option', { name: /Add "Add/ })).not.toBeInTheDocument()
  })

  it('offers no second create for a spare just made, before the list refetches', async () => {
    createEquipment.mockResolvedValue(item({ id: 'eq-new', name: 'Hydraulic oil' }))
    render(<Harness />)
    type('Hydraulic oil')
    fireEvent.click(await screen.findByRole('option', { name: 'Add "Hydraulic oil" as a spare' }))
    await waitFor(() => expect(screen.getByRole('button', { name: 'Add part' })).toBeEnabled())
    fireEvent.click(screen.getByRole('button', { name: 'Add part' }))
    type('hydraulic oil')
    expect(screen.queryByRole('option', { name: /as a spare/ })).not.toBeInTheDocument()
  })
})

describe('PartsEditor: review fixes', () => {
  it('re-selects the pick when the text is changed back to its name', async () => {
    render(<Harness />)
    type('fuel')
    fireEvent.click(await screen.findByRole('option', { name: 'Fuel filter' }))
    const input = screen.getByRole('combobox', { name: 'Add a part' })
    // The open list marks the rest of the page inert, so look it up by label.
    const addButton = () => screen.getByLabelText('Add part')
    fireEvent.change(input, { target: { value: 'Fuel filter ' } })
    expect(addButton()).toBeDisabled()
    fireEvent.change(input, { target: { value: 'Fuel filter' } })
    expect(addButton()).toBeEnabled()
    fireEvent.click(addButton())
    expect(partsOut()).toEqual([{ equipment_id: 'eq-filter', quantity: 1 }])
  })

  it('locks the box and says so while a spare saves', async () => {
    let resolve: (v: EquipmentItem) => void = () => {}
    createEquipment.mockReturnValue(new Promise<EquipmentItem>((r) => { resolve = r }))
    render(<Harness />)
    type('Hydraulic oil')
    fireEvent.click(await screen.findByRole('option', { name: 'Add "Hydraulic oil" as a spare' }))
    expect(await screen.findByText('Adding the spare...')).toBeInTheDocument()
    const input = screen.getByRole('combobox', { name: 'Add a part' })
    expect(input).toHaveAttribute('placeholder', 'Add a part...')
    fireEvent.change(input, { target: { value: 'Something else' } })
    expect(input).toHaveValue('Hydraulic oil')
    resolve(item({ id: 'eq-new', name: 'Hydraulic oil' }))
    await waitFor(() => expect(screen.getByRole('button', { name: 'Add part' })).toBeEnabled())
    expect(screen.queryByText('Adding the spare...')).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Add part' }))
    expect(partsOut()).toEqual([{ equipment_id: 'eq-new', quantity: 1 }])
  })

  it('treats runs of spaces as one when checking for an existing name', async () => {
    render(<Harness />)
    type('fuel  filter')
    expect(await screen.findByRole('option', { name: 'Fuel filter' })).toBeInTheDocument()
    expect(screen.queryByRole('option', { name: /as a spare/ })).not.toBeInTheDocument()
  })

  it('shows the part number on each row so twins can be told apart', async () => {
    const twins = [
      item({ id: 'a', name: 'Fuel filter', part_number: 'R90P' }),
      item({ id: 'b', name: 'Fuel filter', part_number: 'R120A' }),
    ]
    render(<PartsEditor parts={[]} onChange={() => {}} items={twins} itemsLoading={false} itemsError={null} />)
    type('fuel')
    expect(await screen.findByText('R90P')).toBeInTheDocument()
    expect(screen.getByText('R120A')).toBeInTheDocument()
    expect(screen.getAllByRole('option', { name: 'Fuel filter' })).toHaveLength(2)
  })
})

describe('PartsEditor: changing the pick', () => {
  it('keeps the second pick when switching from one part to another', async () => {
    render(<Harness />)
    type('fuel')
    fireEvent.click(await screen.findByRole('option', { name: 'Fuel filter' }))
    fireEvent.click(screen.getByRole('button', { name: 'Show matching items' }))
    // The chevron offers the whole list again, not only the picked name.
    fireEvent.click(await screen.findByRole('option', { name: 'Raw water impeller' }))
    expect(screen.getByRole('button', { name: 'Add part' })).toBeEnabled()
    fireEvent.click(screen.getByRole('button', { name: 'Add part' }))
    expect(partsOut()).toEqual([{ equipment_id: 'eq-impeller', quantity: 1 }])
  })
})

describe('PartsEditor: equipment list not here', () => {
  it('offers no create row while the equipment list is still loading', async () => {
    render(<Harness items={[]} itemsLoading />)
    type('Raw water impeller')
    await screen.findByRole('listbox')
    expect(screen.queryByRole('option', { name: /as a spare/ })).not.toBeInTheDocument()
  })

  it('says the list could not load and offers no create row', async () => {
    render(<Harness items={[]} itemsError="box unreachable" />)
    expect(screen.getByText(/box unreachable/)).toBeInTheDocument()
    type('Raw water impeller')
    expect(screen.queryByRole('option', { name: /as a spare/ })).not.toBeInTheDocument()
  })
})

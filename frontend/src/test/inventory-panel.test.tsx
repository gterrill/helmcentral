import { forwardRef, useImperativeHandle } from 'react'
import { describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen } from '@testing-library/react'
import { InventoryPanel, type InventoryPanelHandle } from '@/components/inventory/inventory-panel'
import { INVENTORY_HELP_TARGETS } from '@/lib/help-links'

// InventoryPanel is a thin composition shell over four already-tested
// children (equipment-index.test.tsx, equipment-editor.test.tsx,
// profiles-section.test.tsx, locations-section.test.tsx each cover their own
// fetches/behaviour) - mocked here so this file only exercises InventoryPanel's
// own job: which one renders for a given (section, equipmentEditId,
// creatingEquipment) combination, and that its callback props are wired to
// the right child prop.

const saveMock = vi.fn().mockResolvedValue(undefined)

vi.mock('@/components/inventory/equipment-index', () => ({
  EquipmentIndex: (props: { onOpenItem: (id: string) => void; onNewItem: () => void }) => (
    <div data-testid="equipment-index">
      <button type="button" onClick={() => props.onOpenItem('eq-1')}>open-eq-1</button>
      <button type="button" onClick={props.onNewItem}>new-item</button>
    </div>
  ),
}))

vi.mock('@/components/inventory/equipment-editor', () => ({
  EquipmentEditor: forwardRef(function MockEquipmentEditor(
    props: { id: string | null; onDirtyChange?: (dirty: boolean) => void },
    ref,
  ) {
    useImperativeHandle(ref, () => ({ save: saveMock }), [])
    return (
      <div data-testid="equipment-editor">
        {props.id ?? 'new'}
        <button type="button" onClick={() => props.onDirtyChange?.(true)}>make-dirty</button>
      </div>
    )
  }),
}))

vi.mock('@/components/inventory/profiles-section', () => ({
  ProfilesSection: () => <div data-testid="profiles-section" />,
}))

vi.mock('@/components/inventory/locations-section', () => ({
  LocationsSection: () => <div data-testid="locations-section" />,
}))

function baseProps() {
  return {
    activeSectionId: 'equipment' as const,
    onSectionChange: vi.fn(),
    equipmentEditId: null,
    onEquipmentEditIdChange: vi.fn(),
    creatingEquipment: false,
    onCreatingEquipmentChange: vi.fn(),
  }
}

describe('InventoryPanel', () => {
  it('renders the Equipment index by default', () => {
    render(<InventoryPanel {...baseProps()} />)
    expect(screen.getByTestId('equipment-index')).toBeInTheDocument()
  })

  it('opening an item from the index reports it through onEquipmentEditIdChange', () => {
    const props = baseProps()
    render(<InventoryPanel {...props} />)
    fireEvent.click(screen.getByText('open-eq-1'))
    expect(props.onEquipmentEditIdChange).toHaveBeenCalledWith('eq-1')
  })

  it('New item reports through onCreatingEquipmentChange', () => {
    const props = baseProps()
    render(<InventoryPanel {...props} />)
    fireEvent.click(screen.getByText('new-item'))
    expect(props.onCreatingEquipmentChange).toHaveBeenCalledWith(true)
  })

  it('renders the editor, not the index, when an equipmentEditId is set', () => {
    render(<InventoryPanel {...baseProps()} equipmentEditId="eq-1" />)
    expect(screen.getByTestId('equipment-editor')).toHaveTextContent('eq-1')
    expect(screen.queryByTestId('equipment-index')).not.toBeInTheDocument()
  })

  it('renders the editor in create mode when creatingEquipment is true', () => {
    render(<InventoryPanel {...baseProps()} creatingEquipment />)
    expect(screen.getByTestId('equipment-editor')).toHaveTextContent('new')
  })

  it('renders ProfilesSection for the profiles section', () => {
    render(<InventoryPanel {...baseProps()} activeSectionId="profiles" />)
    expect(screen.getByTestId('profiles-section')).toBeInTheDocument()
  })

  it('renders LocationsSection for the locations section', () => {
    render(<InventoryPanel {...baseProps()} activeSectionId="locations" />)
    expect(screen.getByTestId('locations-section')).toBeInTheDocument()
  })

  it('clicking a nav section reports it through onSectionChange', () => {
    const props = baseProps()
    render(<InventoryPanel {...props} />)
    fireEvent.click(screen.getByRole('button', { name: 'Locations' }))
    expect(props.onSectionChange).toHaveBeenCalledWith('locations')
  })

  it('forwards the dirty flag from the mounted editor', () => {
    const onDirtyChange = vi.fn()
    render(<InventoryPanel {...baseProps()} equipmentEditId="eq-1" onDirtyChange={onDirtyChange} />)
    fireEvent.click(screen.getByText('make-dirty'))
    expect(onDirtyChange).toHaveBeenCalledWith(true)
  })

  it('opens help at the target for the active section', () => {
    const onOpenHelp = vi.fn()
    render(<InventoryPanel {...baseProps()} activeSectionId="locations" onOpenHelp={onOpenHelp} />)
    fireEvent.click(screen.getByRole('button', { name: 'Open help for this section' }))
    expect(onOpenHelp).toHaveBeenCalledWith(INVENTORY_HELP_TARGETS.locations)
  })

  it('exposes save() through the imperative handle, delegating to the mounted editor', async () => {
    const ref = { current: null as InventoryPanelHandle | null }
    render(<InventoryPanel {...baseProps()} equipmentEditId="eq-1" ref={ref} />)
    await ref.current?.save()
    expect(saveMock).toHaveBeenCalled()
  })
})

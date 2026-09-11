import { fireEvent, render, screen } from '@testing-library/react'
import { beforeEach, describe, expect, test, vi } from 'vitest'

import { PoiMapConfigDialog } from '@/components/poi-map-config-dialog'
import type { DashboardLayoutItem } from '@/lib/dashboard-widgets'

const widget: DashboardLayoutItem = {
  id: 'poi-map:m1x8abcd',
  x: 0,
  y: 0,
  w: 12,
  h: 7,
  poiMap: { title: 'Nearby', rangeNm: 5, categories: ['anchorage', 'marina'], layout: 'split', showAis: true, showTrail: false },
}

function renderDialog(overrides: Partial<React.ComponentProps<typeof PoiMapConfigDialog>> = {}) {
  const onSave = vi.fn()
  const onOpenChange = vi.fn()
  render(
    <PoiMapConfigDialog
      widget={widget}
      open
      onOpenChange={onOpenChange}
      onSave={onSave}
      {...overrides}
    />,
  )
  return { onSave, onOpenChange }
}

const titleField = () => screen.getByLabelText(/title/i)
const rangeField = () => screen.getByLabelText(/range/i)
const mapOnlyRadio = () => screen.getByRole('radio', { name: /map only/i })
const mapAndListRadio = () => screen.getByRole('radio', { name: /map and list/i })
const categoryCheckbox = (label: string) => screen.getByRole('checkbox', { name: label })
const aisSwitch = () => screen.getByRole('switch', { name: /ais/i })
const trailSwitch = () => screen.getByRole('switch', { name: /trail/i })
const saveButton = () => screen.getByRole('button', { name: /save/i })

describe('PoiMapConfigDialog', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  test('seeds the fields from the widget being edited', () => {
    renderDialog()

    expect(titleField()).toHaveValue('Nearby')
    expect(rangeField()).toHaveValue(5)
    expect(mapAndListRadio()).toBeChecked()
    expect(mapOnlyRadio()).not.toBeChecked()
    expect(categoryCheckbox('Anchorage')).toBeChecked()
    expect(categoryCheckbox('Marina')).toBeChecked()
    expect(categoryCheckbox('Fuel')).not.toBeChecked()
    expect(aisSwitch()).toBeChecked()
    expect(trailSwitch()).not.toBeChecked()
  })

  test('saves the trimmed title and current field values', () => {
    const { onSave, onOpenChange } = renderDialog()

    fireEvent.change(titleField(), { target: { value: '  Anchorages  ' } })
    fireEvent.click(saveButton())

    expect(onSave).toHaveBeenCalledWith(widget.id, {
      title: 'Anchorages',
      rangeNm: 5,
      categories: ['anchorage', 'marina'],
      layout: 'split',
      showAis: true,
      showTrail: false,
    })
    expect(onOpenChange).toHaveBeenCalledWith(false)
  })

  test('blocks saving with no categories selected', () => {
    const { onSave } = renderDialog()

    fireEvent.click(categoryCheckbox('Anchorage'))
    fireEvent.click(categoryCheckbox('Marina'))

    expect(saveButton()).toBeDisabled()
    fireEvent.click(saveButton())
    expect(onSave).not.toHaveBeenCalled()
  })

  test('blocks saving a range outside 0.5 to 25', () => {
    renderDialog()

    fireEvent.change(rangeField(), { target: { value: '30' } })
    expect(saveButton()).toBeDisabled()

    fireEvent.change(rangeField(), { target: { value: '0.1' } })
    expect(saveButton()).toBeDisabled()

    fireEvent.change(rangeField(), { target: { value: '10' } })
    expect(saveButton()).toBeEnabled()
  })

  test('adds a category by checking it', () => {
    const { onSave } = renderDialog()

    fireEvent.click(categoryCheckbox('Fuel'))
    fireEvent.click(saveButton())

    expect(onSave).toHaveBeenCalledWith(widget.id, expect.objectContaining({
      categories: ['anchorage', 'marina', 'fuel'],
    }))
  })

  test('switches to "map only" layout', () => {
    const { onSave } = renderDialog()

    fireEvent.click(mapOnlyRadio())
    fireEvent.click(saveButton())

    expect(onSave).toHaveBeenCalledWith(widget.id, expect.objectContaining({ layout: 'map' }))
  })

  test('toggles AIS and trail switches', () => {
    const { onSave } = renderDialog()

    fireEvent.click(aisSwitch())
    fireEvent.click(trailSwitch())
    fireEvent.click(saveButton())

    expect(onSave).toHaveBeenCalledWith(widget.id, expect.objectContaining({
      showAis: false,
      showTrail: true,
    }))
  })

  test('re-seeds the fields when a different widget is opened', () => {
    const { rerender } = render(
      <PoiMapConfigDialog widget={widget} open onOpenChange={vi.fn()} onSave={vi.fn()} />,
    )
    fireEvent.change(titleField(), { target: { value: 'scratch' } })

    const other: DashboardLayoutItem = {
      ...widget,
      id: 'poi-map:m1x8efgh',
      poiMap: { title: 'Fuel stops', rangeNm: 12, categories: ['fuel'], layout: 'map', showAis: false, showTrail: true },
    }
    rerender(<PoiMapConfigDialog widget={other} open onOpenChange={vi.fn()} onSave={vi.fn()} />)

    expect(titleField()).toHaveValue('Fuel stops')
    expect(rangeField()).toHaveValue(12)
    expect(mapOnlyRadio()).toBeChecked()
    expect(categoryCheckbox('Fuel')).toBeChecked()
    expect(categoryCheckbox('Anchorage')).not.toBeChecked()
    expect(aisSwitch()).not.toBeChecked()
    expect(trailSwitch()).toBeChecked()
  })

  test('a brand new widget with no poiMap config seeds sensible defaults and is valid to save', () => {
    const draft: DashboardLayoutItem = { id: 'poi-map:m1x8zzzz', x: 0, y: 0, w: 12, h: 7 }
    renderDialog({ widget: draft })

    expect(saveButton()).toBeEnabled()
  })
})

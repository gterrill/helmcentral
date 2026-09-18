/**
 * The grouped Add Widget picker (ADR 0107): every built-in widget listed
 * under its category, in category order, placed widgets greyed out rather
 * than removed from the list (so it's obvious why they're missing from a
 * click, not silently absent), and multi-instance entries (Gauge, Engine
 * Cluster, …) always enabled since more than one of each can exist.
 */
import { fireEvent, render, screen, within } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { AddWidgetPicker } from '@/components/add-widget-picker'
import { DASHBOARD_WIDGET_IDS, DASHBOARD_WIDGET_LABELS, WIDGET_CATEGORIES } from '@/lib/dashboard-widgets'

function openMenu() {
  fireEvent.click(screen.getByRole('button', { name: /add widget/i }))
}

describe('AddWidgetPicker', () => {
  it('lists every built-in widget, grouped under a category heading in category order', () => {
    render(<AddWidgetPicker placedWidgetIds={[]} onAddWidget={vi.fn()} multiInstanceEntries={[]} />)
    openMenu()

    const menu = screen.getByRole('menu')
    const headings = within(menu).getAllByText(new RegExp(WIDGET_CATEGORIES.map((c) => c.label).join('|')))
    const headingOrder = headings.map((el) => el.textContent)
    const categoryLabelsPresent = WIDGET_CATEGORIES.map((c) => c.label).filter((label) => headingOrder.includes(label))
    // The categories actually rendered (some, like Engine and Custom, carry
    // no built-in widget at all here since multiInstanceEntries is empty)
    // still come out in WIDGET_CATEGORIES' own order.
    expect(headingOrder.filter((h) => categoryLabelsPresent.includes(h!))).toEqual(categoryLabelsPresent)

    for (const id of DASHBOARD_WIDGET_IDS) {
      expect(within(menu).getByText(DASHBOARD_WIDGET_LABELS[id])).toBeInTheDocument()
    }
  })

  it('disables a widget already on the page and does nothing when it is chosen', () => {
    const onAddWidget = vi.fn()
    render(<AddWidgetPicker placedWidgetIds={['wind']} onAddWidget={onAddWidget} multiInstanceEntries={[]} />)
    openMenu()

    const item = screen.getByRole('menuitem', { name: new RegExp(DASHBOARD_WIDGET_LABELS.wind) })
    expect(item).toHaveAttribute('aria-disabled', 'true')
    expect(within(item).getByText(/on page/i)).toBeInTheDocument()

    fireEvent.click(item)
    expect(onAddWidget).not.toHaveBeenCalled()
  })

  it('calls onAddWidget when an unplaced built-in widget is chosen', () => {
    const onAddWidget = vi.fn()
    render(<AddWidgetPicker placedWidgetIds={[]} onAddWidget={onAddWidget} multiInstanceEntries={[]} />)
    openMenu()

    fireEvent.click(screen.getByRole('menuitem', { name: new RegExp(DASHBOARD_WIDGET_LABELS.wind) }))
    expect(onAddWidget).toHaveBeenCalledWith('wind')
  })

  it('always enables a multi-instance entry and calls its own handler when chosen', () => {
    const onSelect = vi.fn()
    render(
      <AddWidgetPicker
        placedWidgetIds={[]}
        onAddWidget={vi.fn()}
        multiInstanceEntries={[{ label: 'Gauge…', category: 'custom', onSelect }]}
      />,
    )
    openMenu()

    const item = screen.getByRole('menuitem', { name: 'Gauge…' })
    expect(item).not.toHaveAttribute('aria-disabled', 'true')
    fireEvent.click(item)
    expect(onSelect).toHaveBeenCalled()
  })
})

/**
 * Popover z-order stacking test: a popover's trigger commonly lives inside a
 * sheet or dialog (the note editor's Link/Image buttons in the documents
 * viewer sheet are the case that exposed this), so the popover must draw
 * above both, the same way tooltip-stacking.test.tsx pins tooltips above
 * dialogs and sheets.
 */
import { describe, it, expect } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { Popover, PopoverTrigger, PopoverContent } from '@/components/ui/popover'

// The app's z-order ladder (see tooltip.tsx and dropdown-menu.tsx): sheet
// z-70, dialog z-80, tooltip z-90 (topmost). A popover must sit strictly
// between dialog and tooltip.
const SHEET_Z = 70
const DIALOG_Z = 80
const TOOLTIP_Z = 90

function zIndexOf(className: string | null | undefined): number {
  const match = className?.match(/\bz-(\d+)\b/)
  if (!match) throw new Error(`no z-<n> utility class found in "${className}"`)
  return Number(match[1])
}

describe('Popover stacking', () => {
  it('renders popover content above the sheet and dialog layers, below the tooltip layer', async () => {
    render(
      <Popover open>
        <PopoverTrigger>Open</PopoverTrigger>
        <PopoverContent>Popover text</PopoverContent>
      </Popover>,
    )

    // Wait for the popover to render (Portal renders asynchronously)
    await waitFor(() => {
      const popupElement = screen.getByText('Popover text')
      expect(popupElement).toBeTruthy()
    })

    // Assert the popup (PopoverPrimitive.Popup) sits above sheet and dialog,
    // below tooltip
    const popupElement = screen.getByText('Popover text')
    const popupZ = zIndexOf(popupElement.className)
    expect(popupZ).toBeGreaterThan(SHEET_Z)
    expect(popupZ).toBeGreaterThan(DIALOG_Z)
    expect(popupZ).toBeLessThan(TOOLTIP_Z)

    // Assert the positioner (PopoverPrimitive.Positioner) parent matches
    const positioner = popupElement.parentElement
    const positionerZ = zIndexOf(positioner?.className)
    expect(positionerZ).toBeGreaterThan(SHEET_Z)
    expect(positionerZ).toBeGreaterThan(DIALOG_Z)
    expect(positionerZ).toBeLessThan(TOOLTIP_Z)
  })
})

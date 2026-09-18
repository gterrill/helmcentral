/**
 * Tooltip z-order stacking test: tooltips must render at z-90, the topmost
 * layer, above dialogs (z-80) and sheets (z-70) because a tooltip can be
 * anchored to a trigger element inside any of those and must remain readable
 * over them.
 */
import { describe, it, expect } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { Tooltip, TooltipTrigger, TooltipContent } from '@/components/ui/tooltip'

describe('Tooltip stacking', () => {
  it('renders tooltip content at z-90, above dialogs and sheets', async () => {
    render(
      <Tooltip open>
        <TooltipTrigger>Hover me</TooltipTrigger>
        <TooltipContent side="right">Tooltip text</TooltipContent>
      </Tooltip>,
    )

    // Wait for the tooltip to render (Portal renders asynchronously)
    await waitFor(() => {
      const popupElement = screen.getByText('Tooltip text')
      expect(popupElement).toBeTruthy()
    })

    // Assert the popup (TooltipPrimitive.Popup) has z-90
    const popupElement = screen.getByText('Tooltip text')
    expect(popupElement.className).toEqual(expect.stringContaining('z-90'))
    expect(popupElement.className).not.toEqual(expect.stringContaining('z-50'))

    // Assert the positioner (TooltipPrimitive.Positioner) parent has z-90
    const positioner = popupElement.parentElement
    expect(positioner?.className).toEqual(expect.stringContaining('z-90'))
    expect(positioner?.className).not.toEqual(expect.stringContaining('z-50'))
  })
})

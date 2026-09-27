/**
 * Select z-order stacking test: a select's trigger commonly lives inside a
 * dialog (the maintenance rule dialog's Item picker is the case that exposed
 * this), so the select popup must draw above the live alarm banner, the
 * sheet and the dialog layers, the same way popover-stacking.test.tsx and
 * tooltip-stacking.test.tsx pin their own layers.
 */
import { describe, it, expect, beforeAll } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { Select, SelectTrigger, SelectValue, SelectPopup, SelectItem } from '@/components/ui/select'
import { measureZLadder, zIndexOf } from './helpers/z-ladder'

// The app's z-order ladder (see popover.tsx, tooltip.tsx and
// dropdown-menu.tsx): alarm banner, sheet, dialog, tooltip (topmost). A
// select popup must sit strictly between dialog and tooltip. Measured from
// the real components (helpers/z-ladder.ts) rather than copied as literals,
// so this fails if a surface's z-index changes instead of staying green
// against a stale ladder.
let ALARM_Z: number
let SHEET_Z: number
let DIALOG_Z: number
let TOOLTIP_Z: number

beforeAll(async () => {
  ;({ alarmZ: ALARM_Z, sheetZ: SHEET_Z, dialogZ: DIALOG_Z, tooltipZ: TOOLTIP_Z } = await measureZLadder())
})

// The item's text sits several DOM levels below the Popup (List and Item
// wrap it with no z-index class of their own), so walk up from it to the
// first ancestor that actually carries a z-<n> utility - that is the Popup.
function findAncestorWithZ(el: HTMLElement): HTMLElement {
  let node: HTMLElement | null = el
  while (node) {
    if (/\bz-\d+\b/.test(node.className)) return node
    node = node.parentElement
  }
  throw new Error('no ancestor with a z-<n> utility class found')
}

describe('Select stacking', () => {
  it('renders the select popup above the alarm banner, sheet and dialog layers, below the tooltip layer', async () => {
    render(
      <Select open>
        <SelectTrigger aria-label="Item">
          <SelectValue />
        </SelectTrigger>
        <SelectPopup>
          <SelectItem value="a">Option A</SelectItem>
        </SelectPopup>
      </Select>,
    )

    // Wait for the popup to render (Portal renders asynchronously)
    await waitFor(() => {
      const itemText = screen.getByText('Option A')
      expect(itemText).toBeTruthy()
    })

    // Assert the popup (SelectPrimitive.Popup) sits above the alarm banner,
    // sheet and dialog, below tooltip
    const itemText = screen.getByText('Option A')
    const popupElement = findAncestorWithZ(itemText)
    const popupZ = zIndexOf(popupElement.className)
    expect(popupZ).toBeGreaterThan(ALARM_Z)
    expect(popupZ).toBeGreaterThan(SHEET_Z)
    expect(popupZ).toBeGreaterThan(DIALOG_Z)
    expect(popupZ).toBeLessThan(TOOLTIP_Z)

    // Assert the positioner (SelectPrimitive.Positioner) parent matches
    const positioner = popupElement.parentElement
    const positionerZ = zIndexOf(positioner?.className)
    expect(positionerZ).toBeGreaterThan(ALARM_Z)
    expect(positionerZ).toBeGreaterThan(SHEET_Z)
    expect(positionerZ).toBeGreaterThan(DIALOG_Z)
    expect(positionerZ).toBeLessThan(TOOLTIP_Z)
  })
})

import { formatAppLocation } from '@/lib/app-location'

// ADR 0127 / ADR 0152: the one place the address on a bin or equipment tag is
// built. The NFC tag row and the printed QR label both call it, so a scan of
// either opens exactly the same page.

/** The app-relative path a bin's tag opens. formatAppLocation percent-encodes
 * the code the same way the app parses `/inventory/bins/<code>` back. */
export function binTagPath(code: string): string {
  return formatAppLocation({ panel: 'inventory', inventorySection: 'locations', binCode: code }, { firstPageId: null })
}

/** The app-relative path an equipment item's tag opens. */
export function equipmentTagPath(id: string): string {
  return formatAppLocation({ panel: 'inventory', inventorySection: 'equipment', equipmentEditId: id }, { firstPageId: null })
}

/** The full address written to a tag or encoded in a QR code: the page's own
 * origin (the boat's tailnet https address when opened from there). */
export function tagUrl(path: string): string {
  return new URL(path, window.location.origin).toString()
}

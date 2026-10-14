// Whether the passage replay shows the small overview map. One choice for the
// replay and for what the export carries, remembered in this browser.
const INSET_KEY = 'passages.insetMap'

export function loadInsetPreference(): boolean {
  try {
    return localStorage.getItem(INSET_KEY) === '1'
  } catch {
    return false
  }
}

export function saveInsetPreference(on: boolean): void {
  try {
    localStorage.setItem(INSET_KEY, on ? '1' : '0')
  } catch {
    // Remembering the toggle is a convenience only.
  }
}
